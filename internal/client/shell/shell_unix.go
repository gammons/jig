//go:build unix

package shell

import (
	"os/exec"
	"syscall"
	"time"
)

// waitDelay bounds how long Wait() will wait for the command's stdout and
// stderr pipes to close after Cancel kills the process group, in case a
// grandchild process detached from the group and kept a pipe end open.
const waitDelay = 2 * time.Second

// configureProcessGroup puts cmd in its own process group and arranges for
// ctx cancellation (parent cancel or our own timeout) to SIGKILL the whole
// group, so background jobs the command spawned (e.g. "sleep 5 &") die
// with it instead of leaking.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
