package search

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// Grep runs `rg --line-number --no-heading --color never [--glob include]
// -e <pattern>` in dir.
func (s *rgSearcher) Grep(ctx context.Context, dir, pattern, include string, limit int) ([]Match, error) {
	args := []string{"--line-number", "--no-heading", "--color", "never"}
	if include != "" {
		args = append(args, "--glob", include)
	}
	args = append(args, "-e", pattern)

	lines, err := s.run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}

	matches := make([]Match, 0, len(lines))
	for _, line := range lines {
		if m, ok := parseRgMatch(line); ok {
			matches = append(matches, m)
		}
	}
	return applyLimit(sortMatches(matches), limit), nil
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
