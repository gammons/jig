package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo makes a git repo in a temp dir with one commit on branch.
func initRepo(t *testing.T, branch string) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", branch)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestGitBranch_NotARepo(t *testing.T) {
	got, err := GitBranch(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("GitBranch: unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("GitBranch(not a repo) = %q, want \"\"", got)
	}
}

func TestGitBranch_OnBranch(t *testing.T) {
	dir := initRepo(t, "feat/x")
	got, err := GitBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("GitBranch: %v", err)
	}
	if got != "feat/x" {
		t.Errorf("GitBranch = %q, want %q", got, "feat/x")
	}
}

func TestGitBranch_FromSubdir(t *testing.T) {
	dir := initRepo(t, "main")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sub, "b.go"), "package sub\n")
	got, err := GitBranch(context.Background(), sub)
	if err != nil {
		t.Fatalf("GitBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("GitBranch(subdir) = %q, want %q", got, "main")
	}
}

func TestGitBranch_UnbornBranch(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "trunk")
	got, err := GitBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("GitBranch: %v", err)
	}
	if got != "trunk" {
		t.Errorf("GitBranch(unborn) = %q, want %q", got, "trunk")
	}
}

func TestGitBranch_DetachedShowsShortSHA(t *testing.T) {
	dir := initRepo(t, "main")
	runGit(t, dir, "checkout", "-q", "--detach")
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	want := strings.TrimSpace(string(out))

	got, err := GitBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("GitBranch: %v", err)
	}
	if got != want {
		t.Errorf("GitBranch(detached) = %q, want %q", got, want)
	}
}
