//go:build unix

package app

import "syscall"

// openNoFollow makes opening the debug log fail on a symlink.
const openNoFollow = syscall.O_NOFOLLOW
