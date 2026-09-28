package blobfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobfs_PutOpenDedup(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	data := []byte("hello blob")
	sum := sha256.Sum256(data)
	wantRef := hex.EncodeToString(sum[:])

	ref1, err := s.Put(data)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	ref2, err := s.Put(data)
	if err != nil {
		t.Fatalf("Put (again): %v", err)
	}
	if ref1 != wantRef || ref2 != wantRef {
		t.Fatalf("ref = %q, %q, want %q", ref1, ref2, wantRef)
	}

	wantPath := filepath.Join(dir, wantRef[:2], wantRef)
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("blob not at expected path %s: %v", wantPath, err)
	}

	got, err := s.Open(ref1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Open = %q, want %q", got, data)
	}

	if s.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", s.Dir(), dir)
	}
}

func TestBlobfs_RejectsBadRefs(t *testing.T) {
	// A nonexistent dir proves Open never touches the filesystem for a bad
	// ref: any attempt to os.Open something under this dir would fail with
	// a filesystem error, not ErrBadRef.
	s := New(filepath.Join(t.TempDir(), "does-not-exist"))

	bad := []string{
		"../../etc/passwd",
		strings.Repeat("a", 63), // one char short of 64
		strings.Repeat("A", 64), // uppercase hex, not lowercase
		"a/b",
	}
	for _, ref := range bad {
		if _, err := s.Open(ref); !errors.Is(err, ErrBadRef) {
			t.Errorf("Open(%q) err = %v, want ErrBadRef", ref, err)
		}
	}
}
