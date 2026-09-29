//go:build !unix

package main

// reexecBare is a no-op off unix: there is no exec(2) to rename argv[0].
func reexecBare() {}
