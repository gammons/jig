package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

func TestTracker_PerSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	sidA := core.SessionID("a")
	sidB := core.SessionID("b")

	tr.MarkRead(sidA, path, info)

	if err := tr.CheckWritable(sidA, path, info); err != nil {
		t.Errorf("session that read the file: got error %v, want nil", err)
	}
	if err := tr.CheckWritable(sidB, path, info); err == nil {
		t.Error("session that never read the file: got nil error, want an error")
	}
}

func TestTracker_NonExistentFileIsAlwaysWritable(t *testing.T) {
	tr := NewTracker()
	if err := tr.CheckWritable(core.SessionID("s"), "/does/not/exist", nil); err != nil {
		t.Errorf("got error %v, want nil", err)
	}
}

func TestTracker_DetectsChangeSinceRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	sid := core.SessionID("s")
	tr.MarkRead(sid, path, info)

	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime().Add(time.Second), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	changed, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := tr.CheckWritable(sid, path, changed); err == nil {
		t.Error("got nil error after external modification, want an error")
	}
}
