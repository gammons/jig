// Package blobfs stores content-addressed binary blobs on disk, addressed
// by the hex-encoded SHA-256 of their bytes.
package blobfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/data/atomicfile"
)

// ErrBadRef is returned by Open when ref does not match ^[0-9a-f]{64}$, the
// hex-encoded SHA-256 of some bytes. Open checks this before any
// filesystem access, since a ref can originate from untrusted input and an
// unvalidated one could otherwise be used to read outside the store's
// directory.
var ErrBadRef = errors.New("blobfs: invalid ref")

// Store is a content-addressed blob store rooted at a directory.
type Store struct {
	dir string
}

// New returns a Store rooted at dir (typically DataDir/blobs). It performs
// no filesystem I/O.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the store's root directory.
func (s *Store) Dir() string {
	return s.dir
}

// Put stores data and returns its ref, the hex-encoded SHA-256 of its
// bytes, at <dir>/<ref[:2]>/<ref> with 0600 permissions. It is idempotent:
// storing the same bytes twice returns the same ref without rewriting the
// file. It is also safe if two processes Put the same blob concurrently,
// since the write goes through atomicfile.Write to the final path; finding
// the file already there means some writer (this one or another process)
// already finished, and the content is guaranteed identical because ref is
// derived from it.
func (s *Store) Put(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	ref := hex.EncodeToString(sum[:])

	path := s.path(ref)
	if _, err := os.Stat(path); err == nil {
		return ref, nil
	}
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return "", fmt.Errorf("blobfs: storing %s: %w", ref, err)
	}
	return ref, nil
}

// Open reads and returns the bytes stored under ref. It returns ErrBadRef,
// without touching the filesystem, if ref is not a lowercase hex SHA-256
// string.
func (s *Store) Open(ref string) ([]byte, error) {
	if !validRef(ref) {
		return nil, ErrBadRef
	}
	data, err := os.ReadFile(s.path(ref))
	if err != nil {
		return nil, fmt.Errorf("blobfs: opening %s: %w", ref, err)
	}
	return data, nil
}

// path returns the on-disk path for a ref already known to be valid.
func (s *Store) path(ref string) string {
	return filepath.Join(s.dir, ref[:2], ref)
}

// validRef reports whether ref is exactly 64 lowercase hex digits.
func validRef(ref string) bool {
	if len(ref) != 64 {
		return false
	}
	for _, c := range ref {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
