package fsroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitRoot_FindsAncestor(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll .git: %v", err)
	}
	leaf := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("MkdirAll leaf: %v", err)
	}

	got, ok := GitRoot(leaf)
	if !ok {
		t.Fatalf("GitRoot(%q): ok = false, want true", leaf)
	}
	if got != repo {
		t.Errorf("GitRoot(%q) = %q, want %q", leaf, got, repo)
	}
}

func TestGitRoot_WorktreeGitFile(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatalf("WriteFile .git: %v", err)
	}
	leaf := filepath.Join(repo, "sub")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("MkdirAll leaf: %v", err)
	}

	got, ok := GitRoot(leaf)
	if !ok {
		t.Fatalf("GitRoot(%q): ok = false, want true", leaf)
	}
	if got != repo {
		t.Errorf("GitRoot(%q) = %q, want %q", leaf, got, repo)
	}
}

func TestGitRoot_NoGitReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	leaf := filepath.Join(tmp, "a", "b")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("MkdirAll leaf: %v", err)
	}

	if _, ok := GitRoot(leaf); ok {
		t.Errorf("GitRoot(%q): ok = true, want false", leaf)
	}
}

func TestChain_OrderRootToLeaf(t *testing.T) {
	root := filepath.FromSlash("/repo")
	dir := filepath.FromSlash("/repo/a/b")

	got := Chain(root, dir)
	want := []string{
		filepath.FromSlash("/repo"),
		filepath.FromSlash("/repo/a"),
		filepath.FromSlash("/repo/a/b"),
	}
	if len(got) != len(want) {
		t.Fatalf("Chain(%q, %q) = %v, want %v", root, dir, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Chain(%q, %q)[%d] = %q, want %q", root, dir, i, got[i], want[i])
		}
	}
}

func TestChain_RootEqualsDir(t *testing.T) {
	root := filepath.FromSlash("/repo")
	got := Chain(root, root)
	want := []string{root}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("Chain(%q, %q) = %v, want %v", root, root, got, want)
	}
}

func TestChain_OutsideRoot(t *testing.T) {
	root := filepath.FromSlash("/a/b")
	dir := filepath.FromSlash("/a/bc")

	got := Chain(root, dir)
	want := []string{dir}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("Chain(%q, %q) = %v, want %v", root, dir, got, want)
	}
}

func TestChain_Unrelated(t *testing.T) {
	root := filepath.FromSlash("/a/b")
	dir := filepath.FromSlash("/x/y")

	got := Chain(root, dir)
	want := []string{dir}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("Chain(%q, %q) = %v, want %v", root, dir, got, want)
	}
}
