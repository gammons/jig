// Package contextfs discovers AGENTS.md/CLAUDE.md project context files and
// resolves config-declared instruction file patterns, per spec §6.4.
package contextfs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/paths"
)

// File is a context file's path and content.
type File struct {
	Path    string
	Content string
}

// AgentsFiles returns p.ConfigDir/AGENTS.md (if present), followed by, for
// each directory in the chain from gitRoot down to workDir (or just workDir
// if gitRoot is ""), that directory's AGENTS.md if present, else its
// CLAUDE.md if present. A directory with neither contributes nothing.
func AgentsFiles(p paths.Paths, gitRoot, workDir string) []File {
	var files []File
	if f, ok := readFile(filepath.Join(p.ConfigDir, "AGENTS.md")); ok {
		files = append(files, f)
	}

	for _, d := range projectChain(gitRoot, workDir) {
		if f, ok := readFile(filepath.Join(d, "AGENTS.md")); ok {
			files = append(files, f)
			continue
		}
		if f, ok := readFile(filepath.Join(d, "CLAUDE.md")); ok {
			files = append(files, f)
		}
	}
	return files
}

func readFile(path string) (File, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, false
	}
	return File{Path: path, Content: string(data)}, true
}

// projectChain returns the project directories from gitRoot to workDir,
// inclusive, or just []string{workDir} if gitRoot is "".
func projectChain(gitRoot, workDir string) []string {
	if gitRoot == "" {
		return []string{workDir}
	}
	return fsroot.Chain(gitRoot, workDir)
}

// Instructions resolves patterns (config's "instructions" list) into files,
// in pattern order, de-duplicated by resolved absolute path. Each pattern
// has "~" expanded against home; if the result is still relative, it is
// resolved against baseDir. A pattern containing any of "*?[" is a glob:
// filepath.Glob's matches are sorted and included, and an unmatched glob is
// not an error. Any other pattern is a literal path; a missing literal path
// is an error.
func Instructions(patterns []string, baseDir, home string) ([]File, error) {
	seen := make(map[string]bool)
	var files []File

	for _, pattern := range patterns {
		resolved := resolvePattern(pattern, baseDir, home)

		if isGlob(pattern) {
			matches, err := filepath.Glob(resolved)
			if err != nil {
				return nil, fmt.Errorf("contextfs: invalid glob %q: %w", pattern, err)
			}
			sort.Strings(matches)
			for _, m := range matches {
				f, err := loadFile(m, seen)
				if err != nil {
					return nil, err
				}
				if f != nil {
					files = append(files, *f)
				}
			}
			continue
		}

		f, err := loadFile(resolved, seen)
		if err != nil {
			return nil, fmt.Errorf("contextfs: reading %s: %w", resolved, err)
		}
		if f != nil {
			files = append(files, *f)
		}
	}
	return files, nil
}

// loadFile reads path and returns a *File, or nil if path's absolute form
// was already in seen. It marks path as seen either way.
func loadFile(path string, seen map[string]bool) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if seen[abs] {
		return nil, nil
	}
	seen[abs] = true
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &File{Path: abs, Content: string(data)}, nil
}

// resolvePattern expands a leading "~" in pattern against home, then joins
// the result against baseDir if it is still relative.
func resolvePattern(pattern, baseDir, home string) string {
	expanded := paths.ExpandHome(pattern, home)
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(baseDir, expanded)
	}
	return expanded
}

// isGlob reports whether pattern contains any glob metacharacter recognized
// by filepath.Glob.
func isGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
