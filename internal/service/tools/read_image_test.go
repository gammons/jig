package tools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/service/media"
)

// fakeBlobs is a media.BlobStore that only counts its puts.
type fakeBlobs struct{ puts int }

func (f *fakeBlobs) Put(data []byte) (string, error) {
	f.puts++
	return "ref", nil
}

// writePNG writes a w×h opaque PNG to path.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRead_ImageReturnsMedia(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.png")
	writePNG(t, path, 2000, 1000)

	fb := &fakeBlobs{}
	tool := NewRead(OSFS(), NewTracker(), media.New(fb))
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	want := "image 1568x784 ("
	if !strings.HasPrefix(res.Output, want) {
		t.Errorf("got %q, want prefix %q", res.Output, want)
	}
	if len(res.Media) != 1 {
		t.Fatalf("len(Media) = %d, want 1", len(res.Media))
	}
	if res.Media[0].MIME != "image/png" {
		t.Errorf("MIME = %q, want image/png", res.Media[0].MIME)
	}
	if fb.puts != 1 {
		t.Errorf("puts = %d, want 1", fb.puts)
	}
}

// fakeStatFS is an FS whose Stat always reports statSize, without
// touching the disk; every other method is unused by the too-large path.
type fakeStatFS struct{ statSize int64 }

func (fakeStatFS) ReadFile(string) ([]byte, error) {
	return nil, errors.New("fakeStatFS: not implemented")
}

func (fakeStatFS) WriteFile(string, []byte, fs.FileMode) error {
	return errors.New("fakeStatFS: not implemented")
}

func (f fakeStatFS) Stat(string) (fs.FileInfo, error) { return fakeFileInfo{size: f.statSize}, nil }

func (fakeStatFS) ReadDir(string) ([]fs.DirEntry, error) {
	return nil, errors.New("fakeStatFS: not implemented")
}

func (fakeStatFS) MkdirAll(string, fs.FileMode) error {
	return errors.New("fakeStatFS: not implemented")
}

type fakeFileInfo struct{ size int64 }

func (fakeFileInfo) Name() string       { return "big.png" }
func (f fakeFileInfo) Size() int64      { return f.size }
func (fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

func TestRead_ImageTooLarge(t *testing.T) {
	dir := t.TempDir()
	tool := NewRead(fakeStatFS{statSize: 21 << 20}, NewTracker(), media.New(&fakeBlobs{}))
	call := mustCall(t, "read", map[string]any{"path": filepath.Join(dir, "big.png")})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Output, "image too large") {
		t.Errorf("got %+v, want IsError containing %q", res, "image too large")
	}
}

func TestRead_CorruptImageIsErrorResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(path, []byte("not a real image"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), media.New(&fakeBlobs{}))
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for a corrupt image")
	}
}

func TestRead_TruncationNoteSurvivesExecutorCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	line := strings.Repeat("a", 40)
	var b strings.Builder
	for range 3000 {
		b.WriteString(line + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	if !regexp.MustCompile(`continue with offset \d+\)$`).MatchString(res.Output) {
		tail := res.Output
		if len(tail) > 60 {
			tail = tail[len(tail)-60:]
		}
		t.Errorf("output does not end with 'continue with offset N)': ...%q", tail)
	}

	maxLen := 50*1024 - len("\n[output truncated at 50 KB]")
	if len(res.Output) > maxLen {
		t.Errorf("len(output) = %d, want <= %d", len(res.Output), maxLen)
	}
}
