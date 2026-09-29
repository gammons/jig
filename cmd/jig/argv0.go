package main

import "path/filepath"

// bareArgv returns argv with argv[0] reduced to its base name, or nil when
// argv[0] is already bare (or argv is empty). tmux names a window after the
// pane's argv[0] (Linux reads /proc/<pid>/cmdline), so `./bin/jig` would
// otherwise show up as "./bin/jig" rather than "jig".
func bareArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	base := filepath.Base(argv[0])
	if base == argv[0] {
		return nil
	}
	out := append([]string{base}, argv[1:]...)
	return out
}
