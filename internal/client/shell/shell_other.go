//go:build !unix

package shell

import "os/exec"

// configureProcessGroup is a no-op on non-unix platforms: ctx cancellation
// falls back to exec.Cmd's default behavior of killing just the direct
// child process.
func configureProcessGroup(cmd *exec.Cmd) {}
