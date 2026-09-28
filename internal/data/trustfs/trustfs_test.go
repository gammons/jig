package trustfs

import (
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

	got1, ok, err := s.Get("/proj/one")
	if err != nil {
		t.Fatalf("Get proj one: %v", err)
	}
	if !ok {
		t.Fatal("Get proj one: ok = false, want true")
	}
	if got1 != g1 {
		t.Errorf("Get proj one = %+v, want %+v", got1, g1)
	}

	got2, ok, err := s.Get("/proj/two")
	if err != nil {
		t.Fatalf("Get proj two: %v", err)
	}
	if !ok {
		t.Fatal("Get proj two: ok = false, want true")
	}
	if got2 != g2 {
		t.Errorf("Get proj two = %+v, want %+v", got2, g2)
	}

	_, ok, err = s.Get("/proj/missing")
	if err != nil {
		t.Fatalf("Get proj missing: %v", err)
	}
	if ok {
		t.Error("Get proj missing: ok = true, want false")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want %o", mode, 0o600)
	}
}

func TestStore_MissingFileIsNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	s := New(path)

	g, ok, err := s.Get("/proj/one")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("Get: ok = true, want false")
	}
	if g != (Grant{}) {
		t.Errorf("Get: g = %+v, want zero value", g)
	}
}

func TestStore_CorruptFileIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := New(path)

	_, _, err := s.Get("/proj/one")
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
