//go:build unix

package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// waitForPID busy-waits (no time.Sleep) for path to hold a pid, failing
// if done yields first.
func waitForPID[T any](t *testing.T, path string, done <-chan T) int {
	t.Helper()
	for {
		if b, err := os.ReadFile(path); err == nil && strings.HasSuffix(string(b), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatalf("pid file %q: %v", b, err)
			}
			return pid
		}
		select {
		case <-done:
			t.Fatal("Run finished before the pid file appeared")
		default:
		}
	}
}

// waitGone busy-waits until pid no longer exists, failing after limit. A
// killed orphan is reaped by init (or a subreaper) shortly after it dies.
func waitGone(t *testing.T, pid int, limit time.Duration) {
	t.Helper()
	deadline := time.After(limit)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("process %d still exists %v after Run returned", pid, limit)
		default:
		}
	}
}

func TestRun_NewSessionWithoutControllingTerminal(t *testing.T) {
	var r Runner
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, Spec{Command: fmt.Sprintf("echo $$ > %s; sleep 5", pidFile)})
		done <- err
	}()
	pid := waitForPID(t, pidFile, done)

	sid, err := unix.Getsid(pid)
	if err != nil {
		t.Fatalf("Getsid(%d): %v", pid, err)
	}
	if sid != pid {
		t.Errorf("the command's session is %d, want its own (%d): it must not share jig's session and terminal", sid, pid)
	}
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Errorf("Getpgid(%d) = %d, %v; want its own group (the group kill relies on it)", pid, pgid, err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}

// A group member that escaped the first SIGKILL (on darwin, a child bash
// was forking while killpg ran) is caught by a later one: the group is
// re-killed on every tick until kill reports it empty.
func TestKillUntilEmpty_RepeatsUntilESRCH(t *testing.T) {
	t.Parallel()
	var calls []int
	kill := func(pid int, sig syscall.Signal) error {
		if sig != syscall.SIGKILL {
			t.Errorf("signal = %v, want SIGKILL", sig)
		}
		calls = append(calls, pid)
		if len(calls) == 3 {
			return syscall.ESRCH
		}
		return nil
	}
	ticks := make(chan time.Time, 10)
	for range 10 {
		ticks <- time.Time{}
	}
	killUntilEmpty(42, kill, ticks, 10)
	if want := []int{-42, -42, -42}; !slices.Equal(calls, want) {
		t.Errorf("kills = %v, want %v (stop at the first ESRCH)", calls, want)
	}
	if len(ticks) != 7 {
		t.Errorf("%d ticks left, want 7: it must not wait for more after ESRCH", len(ticks))
	}
}

func TestKillUntilEmpty_Bounded(t *testing.T) {
	t.Parallel()
	n := 0
	kill := func(int, syscall.Signal) error { n++; return nil }
	ticks := make(chan time.Time, 10)
	for range 10 {
		ticks <- time.Time{}
	}
	killUntilEmpty(42, kill, ticks, 4)
	if n != 4 {
		t.Errorf("kills = %d, want 4 (the bound)", n)
	}
}

// A command that forks continuously is cancelled mid-fork most of the
// time: on darwin that is the race the re-kill closes. Run must return
// promptly and leave the group empty.
func TestRun_CancelWhileForking(t *testing.T) {
	var r Runner
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, Spec{Command: fmt.Sprintf("echo $$ > %s; while :; do sleep 30 & done", pidFile)})
		done <- err
	}()
	pid := waitForPID(t, pidFile, done)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
	waitGone(t, -pid, 2*time.Second)
}

func TestRun_TimeoutKillsGrandchildren(t *testing.T) {
	var r Runner
	pidFile := filepath.Join(t.TempDir(), "pid")
	done := make(chan Result, 1)
	go func() {
		res, _ := r.Run(context.Background(), Spec{
			Command: fmt.Sprintf("sleep 30 & echo $! > %s; sleep 30", pidFile),
			Timeout: 300 * time.Millisecond,
		})
		done <- res
	}()
	pid := waitForPID(t, pidFile, done)

	select {
	case res := <-done:
		if !res.TimedOut {
			t.Errorf("TimedOut = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of a 300ms timeout")
	}
	waitGone(t, pid, 2*time.Second)
}

func TestRun_CancelKillsGrandchildren(t *testing.T) {
	var r Runner
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, Spec{Command: fmt.Sprintf("sleep 30 & echo $! > %s; sleep 30", pidFile)})
		done <- err
	}()
	pid := waitForPID(t, pidFile, done)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
	waitGone(t, pid, 2*time.Second)
}
