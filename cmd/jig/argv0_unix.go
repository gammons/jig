//go:build unix

package main

import (
	"os"
	"syscall"
)

// reexecBare replaces the process with itself under a bare argv[0], so
// tmux shows "jig" instead of the invocation path. It returns only when
// argv[0] is already bare or the exec fails, in which case jig carries on
// under the original name.
func reexecBare() {
	argv := bareArgv(os.Args)
	if argv == nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = syscall.Exec(exe, argv, os.Environ())
}
