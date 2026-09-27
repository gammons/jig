package search

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// rgSearcher implements Searcher by shelling out to ripgrep.
type rgSearcher struct {
	rgPath string
}

// Glob runs `rg --files --glob <pattern>` in dir and stats each result to
// sort by mtime.
func (s *rgSearcher) Glob(ctx context.Context, dir, pattern string, limit int) ([]string, error) {
	lines, err := s.run(ctx, dir, "--files", "--glob", pattern)
	if err != nil {
		return nil, err
	}

	entries := make([]fileEntry, 0, len(lines))
	for _, rel := range lines {
		info, statErr := os.Stat(filepath.Join(dir, rel))
		if statErr != nil {
			continue
		}
		entries = append(entries, fileEntry{path: rel, mtime: info.ModTime()})
	}
	return sortAndLimitPaths(entries, limit), nil
}

// Grep runs `rg --line-number --no-heading --color never --sort path
// --max-columns 500 --max-columns-preview [--glob include] -e <pattern>`
// in dir, reading matches as rg prints them and killing rg once limit
// matches have arrived. --sort path makes the first limit matches the same
// ones a full, sorted search would keep.
func (s *rgSearcher) Grep(ctx context.Context, dir, pattern, include string, limit int) ([]Match, error) {
	args := []string{
		"--line-number", "--no-heading", "--color", "never", "--sort", "path",
		"--max-columns", strconv.Itoa(maxLineChars), "--max-columns-preview",
	}
	if include != "" {
		args = append(args, "--glob", include)
	}
	args = append(args, "-e", pattern)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.rgPath, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("search: rg: %w", err)
	}

	matches, full := scanMatches(stdout, limit)
	if full {
		cancel()
		_ = cmd.Wait()
		return sortMatches(matches), nil
	}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("search: rg %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return sortMatches(matches), nil
}

// scanMatches parses rg output lines from r until EOF or, when limit > 0,
// until limit matches are read (full reports the latter).
func scanMatches(r io.Reader, limit int) (matches []Match, full bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if m, ok := parseRgMatch(sc.Text()); ok {
			matches = append(matches, m)
			if limit > 0 && len(matches) >= limit {
				return matches, true
			}
		}
	}
	return matches, false
}

// run executes rg with args in dir and returns its stdout split into
// non-empty lines. rg's exit code 1 (no files/no matches) is reported as
// success with no output, matching the walker's behavior for the same
// case.
func (s *rgSearcher) run(ctx context.Context, dir string, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, s.rgPath, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("search: rg %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	text := strings.TrimRight(stdout.String(), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// parseRgMatch parses one line of `rg --line-number --no-heading` output
// ("path:line:text") into a Match.
func parseRgMatch(line string) (Match, bool) {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return Match{}, false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return Match{}, false
	}
	return Match{Path: parts[0], Line: n, Text: parts[2]}, true
}
