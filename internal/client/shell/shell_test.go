package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun_CapturesOutputAndExit(t *testing.T) {
	var r Runner
	res, err := r.Run(context.Background(), Spec{Command: `echo hi; echo err 1>&2; exit 3`})
	if err != nil {
		t.Fatalf("Run err = %v, want nil", err)
	}
	if got, want := string(res.Output), "hi\nerr\n"; got != want {
		t.Errorf("Output = %q, want %q", got, want)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
	if res.TimedOut {
		t.Errorf("TimedOut = true, want false")
	}
}

func TestRun_TimeoutKillsProcessGroup(t *testing.T) {
	var r Runner
	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := r.Run(context.Background(), Spec{
			Command: "sleep 5 & sleep 5",
			Timeout: 200 * time.Millisecond,
		})
		done <- outcome{res, err}
	}()

	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("Run err = %v, want nil", o.err)
		}
		if !o.res.TimedOut {
			t.Errorf("TimedOut = false, want true")
		}
		if o.res.ExitCode != -1 {
			t.Errorf("ExitCode = %d, want -1", o.res.ExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of a 200ms timeout")
	}
}

func TestRun_ContextCancel(t *testing.T) {
	var r Runner
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := r.Run(ctx, Spec{Command: fmt.Sprintf("touch %s && sleep 5", marker)})
		done <- outcome{res, err}
	}()

	// Busy-wait (no time.Sleep) for the marker file, proving the process
	// actually started before we cancel it mid-flight.
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		select {
		case <-done:
			t.Fatal("Run finished before the marker file appeared")
		default:
		}
	}
	cancel()

	select {
	case o := <-done:
		if !errors.Is(o.err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", o.err)
		}
		if o.res.TimedOut {
			t.Errorf("TimedOut = true, want false")
		}
		if o.res.ExitCode != -1 {
			t.Errorf("ExitCode = %d, want -1", o.res.ExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}

func TestRun_Dir(t *testing.T) {
	var r Runner
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	res, err := r.Run(context.Background(), Spec{Command: "pwd", Dir: dir})
	if err != nil {
		t.Fatalf("Run err = %v, want nil", err)
	}
	got := strings.TrimSpace(string(res.Output))
	if got != resolved {
		t.Errorf("pwd output = %q, want %q", got, resolved)
	}
}
