package search

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// walker implements Searcher with filepath.WalkDir, for use when rg is not
// on PATH.
type walker struct{}

// Glob walks dir collecting files matching pattern.
func (walker) Glob(ctx context.Context, dir, pattern string, limit int) ([]string, error) {
	var entries []fileEntry
	err := walkEligibleFiles(ctx, dir, func(rel string, d fs.DirEntry) error {
		if !matchGlob(pattern, rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, fileEntry{path: rel, mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortAndLimitPaths(entries, limit), nil
}

// Grep walks dir, scanning files matching include (if set) for lines
// matching the RE2 pattern.
func (walker) Grep(ctx context.Context, dir, pattern, include string, limit int) ([]Match, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("search: invalid pattern %q: %w", pattern, err)
	}

	var matches []Match
	walkErr := walkEligibleFiles(ctx, dir, func(rel string, d fs.DirEntry) error {
		if include != "" && !matchGlob(include, rel) {
			return nil
		}
		matches = append(matches, grepFile(filepath.Join(dir, rel), rel, re)...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return applyLimit(sortMatches(matches), limit), nil
}

// walkEligibleFiles calls fn for every regular file under dir that is not
// hidden (dot-prefixed), not inside .git, and not matched by dir's root
// .gitignore. fn's rel path is slash-separated and relative to dir. It
// stops and returns ctx.Err() as soon as ctx is done.
func walkEligibleFiles(ctx context.Context, dir string, fn func(rel string, d fs.DirEntry) error) error {
	ignore := loadGitignore(dir)

	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()

		if d.IsDir() {
			if name == ".git" || strings.HasPrefix(name, ".") || ignore.matches(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || ignore.matches(rel, false) {
			return nil
		}
		return fn(rel, d)
	})
}

// grepFile scans fullPath (rel is its path relative to the search root)
// for lines matching re, skipping the file if it cannot be opened or
// looks binary (a NUL byte in the first 8KB).
func grepFile(fullPath, rel string, re *regexp.Regexp) []Match {
	f, err := os.Open(fullPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	head := make([]byte, 8192)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil
	}

	var matches []Match
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, Match{Path: rel, Line: line, Text: previewLine(text)})
		}
	}
	return matches
}

// previewLine cuts text to maxLineChars runes, marking the cut the way
// `rg --max-columns-preview` does.
func previewLine(text string) string {
	if utf8.RuneCountInString(text) <= maxLineChars {
		return text
	}
	return string([]rune(text)[:maxLineChars]) + longLineNote
}

// matchGlob reports whether relPath (slash-separated) matches pattern,
// supporting "**" as any number of path segments (including zero); other
// segments are matched with path.Match. A pattern with no "/" matches the
// basename at any depth, per rg's glob semantics.
func matchGlob(pattern, relPath string) bool {
	segs := strings.Split(pattern, "/")
	if !strings.Contains(pattern, "/") {
		segs = append([]string{"**"}, segs...)
	}
	return matchSegments(segs, strings.Split(relPath, "/"))
}

func matchSegments(pat, p []string) bool {
	if len(pat) == 0 {
		return len(p) == 0
	}
	if pat[0] == "**" {
		if matchSegments(pat[1:], p) {
			return true
		}
		if len(p) == 0 {
			return false
		}
		return matchSegments(pat, p[1:])
	}
	if len(p) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], p[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], p[1:])
}

// gitignore is a small, documented subset of .gitignore matching: only the
// root file is read (nested .gitignore files are not honored), negation
// ("!pattern") is not supported, and each pattern is matched line-by-line
// with filepath.Match rather than full gitignore glob semantics.
type gitignore struct {
	patterns []string
}

func loadGitignore(dir string) gitignore {
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return gitignore{}
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return gitignore{patterns: patterns}
}

// matches reports whether relPath (slash-separated, relative to dir) is
// ignored. isDir indicates whether relPath is a directory: a pattern
// ending in "/" only matches directories.
func (g gitignore) matches(relPath string, isDir bool) bool {
	base := path.Base(relPath)
	for _, pat := range g.patterns {
		p := pat
		dirOnly := strings.HasSuffix(p, "/")
		if dirOnly {
			p = strings.TrimSuffix(p, "/")
			if !isDir {
				continue
			}
		}
		anchored := strings.HasPrefix(p, "/")
		p = strings.TrimPrefix(p, "/")

		candidate := base
		if anchored || strings.Contains(p, "/") {
			candidate = relPath
		}
		if ok, err := filepath.Match(p, candidate); err == nil && ok {
			return true
		}
	}
	return false
}
