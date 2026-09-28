package search

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// requireGit skips the test if git is not on PATH.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitModified_NotARepo(t *testing.T) {
	dir := t.TempDir()
	got, err := GitModified(context.Background(), dir)
	if err != nil {
		t.Fatalf("GitModified: unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("GitModified(not a repo) = %v, want nil", got)
	}
}

func TestGitModified_ReportsChangedFile(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")

	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "package main\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")

	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n\n// changed\n")

	got, err := GitModified(context.Background(), dir)
	if err != nil {
		t.Fatalf("GitModified: %v", err)
	}
	sort.Strings(got)
	want := []string{"a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GitModified = %v, want %v", got, want)
	}
}
