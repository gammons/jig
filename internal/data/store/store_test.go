package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpen_MigratesIdempotently(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jig.db")

	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOpen_CreatesParentDir(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "dir", "jig.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
}
