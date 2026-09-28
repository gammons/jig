package trustfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHash_OrderIndependentAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("world"), 0o600); err != nil {
		t.Fatalf("WriteFile b: %v", err)
	}

	h1, err := Hash([]string{pathA, pathB})
	if err != nil {
		t.Fatalf("Hash(a, b): %v", err)
	}
	h2, err := Hash([]string{pathB, pathA})
	if err != nil {
		t.Fatalf("Hash(b, a): %v", err)
	}
	if h1 != h2 {
		t.Errorf("Hash is order-dependent: Hash(a,b)=%q, Hash(b,a)=%q", h1, h2)
	}

	// Changing one byte of a file's content changes the hash.
	if err := os.WriteFile(pathA, []byte("hellp"), 0o600); err != nil {
		t.Fatalf("WriteFile a (changed): %v", err)
	}
	h3, err := Hash([]string{pathA, pathB})
	if err != nil {
		t.Fatalf("Hash after content change: %v", err)
	}
	if h3 == h1 {
		t.Errorf("Hash did not change after a one-byte content change: %q", h3)
	}

	// Renaming a file (same content, different path) changes the hash.
	if err := os.WriteFile(pathA, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile a (restored): %v", err)
	}
	pathC := filepath.Join(dir, "c.txt")
	if err := os.Rename(pathA, pathC); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	h4, err := Hash([]string{pathC, pathB})
	if err != nil {
		t.Fatalf("Hash after rename: %v", err)
	}
	if h4 == h1 {
		t.Errorf("Hash did not change after renaming a file: %q", h4)
	}

	empty, err := Hash(nil)
	if err != nil {
		t.Fatalf("Hash(nil): %v", err)
	}
	if empty != "" {
		t.Errorf("Hash(nil) = %q, want \"\"", empty)
	}
}

// TestHash_PathNewlineCannotForgeBoundary constructs two different file
// sets whose old-format streams (path + "\n" + len(content) + "\n" +
// content, with no length prefix on the path) would coincide byte-for-byte:
//
//	Set A: file p1 (path ".../z", content "")     old-encode: p1 + "\n0\n"
//	       file p2 (path ".../zz", content "x")   old-encode: p2 + "\n1\nx"
//	       concatenated: p1 + "\n0\n" + p2 + "\n1\nx"
//
//	Set B: one file, path = p1 + "\n0\n" + p2, content "x"
//	       old-encode: (p1+"\n0\n"+p2) + "\n1\nx"
//
// Both streams are the literal byte string p1+"\n0\n"+p2+"\n1\nx": set B's
// crafted path swallows set A's first file's own "\n0\n" boundary and its
// second file's path, forging the exact bytes set A would have produced.
// With the path length-prefixed, the two sets must hash differently.
func TestHash_PathNewlineCannotForgeBoundary(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "z")
	p2 := filepath.Join(dir, "zz")

	if err := os.WriteFile(p1, nil, 0o600); err != nil {
		t.Fatalf("WriteFile p1: %v", err)
	}
	if err := os.WriteFile(p2, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile p2: %v", err)
	}

	setHash, err := Hash([]string{p2, p1})
	if err != nil {
		t.Fatalf("Hash(set A): %v", err)
	}

	pathB := p1 + "\n0\n" + p2
	if err := os.MkdirAll(filepath.Dir(pathB), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile pathB: %v", err)
	}

	singleHash, err := Hash([]string{pathB})
	if err != nil {
		t.Fatalf("Hash(set B): %v", err)
	}

	if setHash == singleHash {
		t.Errorf("Hash collided across different file sets via a forged path boundary: both = %q", setHash)
	}
}

// TestHash_DuplicatePath checks that a repeated path is deterministic (not,
// say, deduplicated non-deterministically by map iteration) and that
// listing a file twice contributes it twice, producing a different hash
// than listing it once.
func TestHash_DuplicatePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	single, err := Hash([]string{p})
	if err != nil {
		t.Fatalf("Hash([p]): %v", err)
	}
	dup1, err := Hash([]string{p, p})
	if err != nil {
		t.Fatalf("Hash([p,p]) (1st): %v", err)
	}
	dup2, err := Hash([]string{p, p})
	if err != nil {
		t.Fatalf("Hash([p,p]) (2nd): %v", err)
	}

	if dup1 != dup2 {
		t.Errorf("Hash([p,p]) is not deterministic: %q vs %q", dup1, dup2)
	}
	if dup1 == single {
		t.Errorf("Hash([p,p]) = %q, want it to differ from Hash([p]) = %q", dup1, single)
	}
}

// granted is Get that fails the test on error.
func granted(t *testing.T, s *Store, project, hash string) bool {
	t.Helper()
	ok, err := s.Get(project, hash)
	if err != nil {
		t.Fatalf("Get(%s, %s): %v", project, hash, err)
	}
	return ok
}

func TestStore_PutGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	s := New(path)

	g1 := Grant{Hash: "hash-1", GrantedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	g2 := Grant{Hash: "hash-2", GrantedAt: time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)}

	if err := s.Put("/proj/one", g1); err != nil {
		t.Fatalf("Put proj one: %v", err)
	}
	if err := s.Put("/proj/two", g2); err != nil {
		t.Fatalf("Put proj two: %v", err)
	}

	if !granted(t, s, "/proj/one", "hash-1") || !granted(t, s, "/proj/two", "hash-2") {
		t.Error("a granted hash is not reported as granted")
	}
	if granted(t, s, "/proj/one", "hash-2") || granted(t, s, "/proj/missing", "hash-1") {
		t.Error("an ungranted project/hash is reported as granted")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want %o", mode, 0o600)
	}
}

// TestStore_KeepsSeveralHashesPerProject pins that monorepo subdirs with
// different project configs (different hashes under one git root) don't
// revoke each other's grants.
func TestStore_KeepsSeveralHashesPerProject(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "trust.json"))
	for _, h := range []string{"pkg-a", "pkg-b"} {
		if err := s.Put("/repo", Grant{Hash: h}); err != nil {
			t.Fatal(err)
		}
	}
	if !granted(t, s, "/repo", "pkg-a") || !granted(t, s, "/repo", "pkg-b") {
		t.Error("granting pkg-b revoked pkg-a, want both trusted")
	}
}

func TestStore_EvictsOldestPastLimit(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "trust.json"))
	for i := 0; i <= maxHashes; i++ {
		if err := s.Put("/repo", Grant{Hash: fmt.Sprint("h", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// Re-granting h1 moves it to the front, so h2 is now the oldest.
	if err := s.Put("/repo", Grant{Hash: "h1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("/repo", Grant{Hash: "new"}); err != nil {
		t.Fatal(err)
	}
	for i, want := range map[int]bool{0: false, 1: true, 2: false, 3: true, maxHashes: true} {
		if got := granted(t, s, "/repo", fmt.Sprint("h", i)); got != want {
			t.Errorf("h%d granted = %v, want %v", i, got, want)
		}
	}
	if !granted(t, s, "/repo", "new") {
		t.Error("newest hash not granted")
	}
}

func TestStore_ReadsOldSingleGrantFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	old := `{"/repo":{"hash":"old-hash","grantedAt":"2026-01-02T03:04:05Z"}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if !granted(t, s, "/repo", "old-hash") {
		t.Fatal("old-format grant not recognized")
	}
	if err := s.Put("/repo", Grant{Hash: "new-hash"}); err != nil {
		t.Fatal(err)
	}
	if !granted(t, s, "/repo", "old-hash") || !granted(t, s, "/repo", "new-hash") {
		t.Error("after Put, want both the migrated and the new hash granted")
	}
}

func TestStore_MissingFileIsNotFound(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "trust.json"))
	if granted(t, s, "/proj/one", "h") {
		t.Error("Get: ok = true, want false")
	}
}

func TestStore_CorruptFileIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := New(path)

	_, err := s.Get("/proj/one", "h")
	if err == nil {
		t.Fatal("Get: want error for corrupt JSON, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Get err = %v, want it to name %q", err, path)
	}

	err = s.Put("/proj/one", Grant{Hash: "h"})
	if err == nil {
		t.Fatal("Put: want error for corrupt JSON, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Put err = %v, want it to name %q", err, path)
	}
}

func TestHashOptional_MissingIsStableAndDistinct(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	ref := filepath.Join(dir, "mode")
	if err := os.WriteFile(cfg, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	absent1, err := HashOptional([]string{cfg}, []string{ref})
	if err != nil {
		t.Fatalf("HashOptional with missing optional: %v", err)
	}
	absent2, _ := HashOptional([]string{cfg}, []string{ref})
	if absent1 != absent2 {
		t.Error("absent hash is not stable")
	}
	if plain, _ := Hash([]string{cfg}); plain == absent1 {
		t.Error("an absent optional file does not change the hash")
	}
	if err := os.WriteFile(ref, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if empty, _ := HashOptional([]string{cfg}, []string{ref}); empty == absent1 {
		t.Error("an empty optional file hashes like an absent one")
	}
	if _, err := HashOptional([]string{filepath.Join(dir, "nope")}, nil); err == nil {
		t.Error("a missing required file is not an error")
	}
}
