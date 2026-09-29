package search

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// GitBranch returns the name of the branch checked out in dir's
// repository (an unborn branch included), or HEAD's short SHA when it is
// detached. If dir is not inside a git repository (or git otherwise
// exits non-zero), it returns "", nil.
func GitBranch(ctx context.Context, dir string) (string, error) {
	name, ok, err := gitLine(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || ok {
		return name, err
	}
	sha, _, err := gitLine(ctx, dir, "rev-parse", "--short", "HEAD")
	return sha, err
}

// gitLine runs git with args in dir and returns its trimmed stdout. A
// non-zero exit is ("", false, nil); only a failure to run git at all is
// an error.
func gitLine(ctx context.Context, dir string, args ...string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}
