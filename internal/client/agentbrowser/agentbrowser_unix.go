//go:build unix

package agentbrowser

import (
	"os/exec"
	"syscall"
	"time"
)

// waitDelay bounds how long Wait() will wait for cmd's stdout pipe to
// close after cancellation kills its process group, in case a grandchild
// process detached from the group and kept the pipe open.
const waitDelay = 2 * time.Second

// configureProcessGroup puts cmd in its own process group and arranges
// for ctx cancellation to SIGKILL the whole group, so a script that has
// already forked (e.g. "sleep 10") does not outlive its shell and leave
// cmd.Run waiting on a pipe no one will ever close.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
