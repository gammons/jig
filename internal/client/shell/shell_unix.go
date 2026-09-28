//go:build unix

package shell

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// waitDelay bounds how long Wait() will wait for the command's stdout and
// stderr pipes to close after Cancel kills the process group, in case a
// grandchild process detached from the group and kept a pipe end open.
const waitDelay = 2 * time.Second

// reKillInterval and reKills bound KillGroup's follow-up kills: every
// 10 ms for up to 1 s.
const (
	reKillInterval = 10 * time.Millisecond
	reKills        = 100
)

// configureProcessGroup starts cmd in a new session (setsid), so it has no
// controlling terminal — a command that opens /dev/tty, or reads the
// terminal, can't fight the TUI for it — and leads its own process group
// (pgid == pid). ctx cancellation (parent cancel or our own timeout)
// SIGKILLs that whole group (KillGroup), so background jobs the command
// spawned (e.g. "sleep 5 &") die with it instead of leaking.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		return KillGroup(cmd.Process.Pid)
	}
}

// KillGroup SIGKILLs process group pgid, then keeps re-sending SIGKILL to
// it in the background until the group is empty (kill fails, normally
// with ESRCH), for at most reKills × reKillInterval.
//
// One kill is not enough on darwin: killpg there walks the group's
// members without excluding a concurrent fork, so a child the shell was
// forking at that moment can join the group after the walk and survive,
// holding the output pipe open until WaitDelay. Linux makes fork and a
// group signal atomic, so there the first follow-up kill already sees
// ESRCH (or only unreaped zombies).
//
// Re-killing by pgid is safe from pid reuse: exec.Cmd.Wait reaps the
// leader before it waits for the pipes, so the leader can be gone, but a
// process group ID is not reused while any member of the group exists,
// and the loop stops at the first failed kill, when the group is empty.
// What remains is a new group taking the same ID within one interval of
// the group emptying, which needs the pid space to wrap in 10 ms.
func KillGroup(pgid int) error {
	if err := firstKill(pgid, syscall.Kill); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(reKillInterval)
		defer t.Stop()
		killUntilEmpty(pgid, syscall.Kill, t.C, reKills)
	}()
	return nil
}

// firstKill SIGKILLs group pgid via kill. A failure that is ESRCH
// (typically: the group is already gone) is reported as
// os.ErrProcessDone, which exec.Cmd's Cancel machinery treats specially
// — it does not turn a clean exit into a spurious cancellation error.
func firstKill(pgid int, kill func(int, syscall.Signal) error) error {
	err := kill(-pgid, syscall.SIGKILL)
	if err != nil && errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// killUntilEmpty sends SIGKILL to group pgid on each of up to n ticks,
// stopping at the first kill that fails (the group is empty).
func killUntilEmpty(pgid int, kill func(int, syscall.Signal) error, ticks <-chan time.Time, n int) {
	for range n {
		<-ticks
		if kill(-pgid, syscall.SIGKILL) != nil {
			return
		}
	}
}
