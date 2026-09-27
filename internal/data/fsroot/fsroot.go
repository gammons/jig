// Package fsroot locates a project's git root and walks the chain of
// directories from that root down to a target directory, for use by
// context and skill discovery.
package fsroot

import (
	"os"
	"path/filepath"
	"strings"
)

// GitRoot returns the nearest ancestor of dir (dir included) that contains a
// ".git" entry, whether a directory (a normal clone) or a regular file (a
// worktree or submodule). It operates on filepath.Clean(dir) and does not
// resolve symlinks. It returns ok=false if no ancestor has a ".git" entry.
func GitRoot(dir string) (string, bool) {
	cur := filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

// Chain returns the directories from root to dir, inclusive, in root-to-leaf
// order. Both arguments are filepath.Clean'ed first. If dir is not under
// root (including partial-prefix false positives like "/a/bc" under
// "/a/b"), Chain returns []string{dir}. Chain(root, root) returns [root].
func Chain(root, dir string) []string {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	if dir == root {
		return []string{root}
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return []string{dir}
	}

	parts := strings.Split(rel, string(filepath.Separator))
	chain := make([]string, 0, len(parts)+1)
	chain = append(chain, root)
	cur := root
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		chain = append(chain, cur)
	}
	return chain
}
