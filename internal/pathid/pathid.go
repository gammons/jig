// Package pathid gives file system paths an identity key, so two paths
// naming the same file or directory (e.g. through a symlink, or /var vs
// /private/var on macOS) compare equal.
package pathid

import "path/filepath"

// Key returns p with symlinks resolved, or, when that fails (e.g. p does
// not exist), p's absolute form. If both fail it returns filepath.Clean(p).
func Key(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		if abs, err := filepath.Abs(r); err == nil {
			return abs
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
