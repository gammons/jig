package trustfs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// hashOf is HashOptional that fails the test on error.
func hashOf(t *testing.T, files, optional []string) string {
	t.Helper()
	h, err := HashOptional(files, optional)
	if err != nil {
		t.Fatalf("HashOptional(%v, %v): %v", files, optional, err)
	}
	return h
}

// TestHash_NonRegularFileNotRead pins that a device (here via a symlink
// to /dev/zero, which would otherwise be read forever) hashes as a
// distinct "unreadable" record instead of being read.
func TestHash_NonRegularFileNotRead(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero unavailable")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "include")
	if err := os.Symlink("/dev/zero", link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	for _, c := range []struct{ files, optional []string }{{[]string{link}, nil}, {nil, []string{link}}} {
		dev := hashOf(t, c.files, c.optional)
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(link, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if empty := hashOf(t, c.files, c.optional); empty == dev {
			t.Error("a device hashes like an empty file")
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/zero", link); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHash_UnreadableIncludeIsARecord pins that an in-tree include that
// is a directory, or sits under a file (ENOTDIR), hashes as a distinct
// record rather than aborting startup.
func TestHash_UnreadableIncludeIsARecord(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	inc := filepath.Join(dir, "inc")
	absent := hashOf(t, []string{cfg}, []string{inc})

	if err := os.Mkdir(inc, 0o755); err != nil {
		t.Fatal(err)
	}
	asDir := hashOf(t, []string{cfg}, []string{inc})
	if asDir == absent {
		t.Error("a directory include hashes like an absent one")
	}
	under := hashOf(t, []string{cfg}, []string{filepath.Join(cfg, "sub")})
	if under == absent || under == asDir {
		t.Error("an ENOTDIR include does not hash as its own record")
	}
}

// TestHash_OverCapFileNotRead pins the 1 MiB per-file cap: a bigger file
// hashes as a distinct record (keyed by its size), not by its content.
func TestHash_OverCapFileNotRead(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	write := func(b []byte) {
		if err := os.WriteFile(big, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data := bytes.Repeat([]byte("a"), maxHashBytes+1)
	write(data)
	h1 := hashOf(t, []string{big}, nil)
	data[0] = 'b' // same size, different content
	write(data)
	if h2 := hashOf(t, []string{big}, nil); h2 != h1 {
		t.Error("an over-cap file's content was hashed")
	}
	write(append(data, 'c'))
	if h3 := hashOf(t, []string{big}, nil); h3 == h1 {
		t.Error("an over-cap file's size change does not change the hash")
	}
	write(data[:maxHashBytes])
	if h4 := hashOf(t, []string{big}, nil); h4 == h1 {
		t.Error("an at-cap file hashes like an over-cap one")
	}
}
