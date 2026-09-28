package search

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

// GitModified returns the workdir-relative, slash-separated paths that
// `git status --porcelain -z` reports as changed in dir. A rename or copy
// entry contributes its new path only. If dir is not inside a git
// repository (or git otherwise exits non-zero), it returns nil, nil.
func GitModified(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "-z")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil
		}
		return nil, err
	}
	return parsePorcelainZ(out), nil
}

// parsePorcelainZ parses the NUL-separated output of `git status
// --porcelain -z`. Each entry is "XY PATH"; a rename/copy status (R/C)
// is followed by a second entry holding the original path, which is
// skipped.
func parsePorcelainZ(out []byte) []string {
	entries := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	var paths []string
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		status, path := entry[:2], string(entry[3:])
		paths = append(paths, path)
		if status[0] == 'R' || status[0] == 'C' {
			i++ // skip the original-path entry
		}
	}
	return paths
}
