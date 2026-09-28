package tools

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gammons/jig/internal/service/media"
)

// bigStatFS is the real OS file system, except Stat reports every file
// as size bytes long.
type bigStatFS struct {
	FS
	size int64
}

func (b bigStatFS) Stat(path string) (fs.FileInfo, error) {
	if _, err := b.FS.Stat(path); err != nil {
		return nil, err
	}
	return fakeFileInfo{size: b.size}, nil
}

// shotDirs are the three unrelated directories a screenshot test uses:
// the run's workdir, the (fake) OS temp dir, and somewhere outside both.
type shotDirs struct{ work, tmp, outside string }

func newShotDirs(t *testing.T) shotDirs {
	t.Helper()
	return shotDirs{work: t.TempDir(), tmp: t.TempDir(), outside: t.TempDir()}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestScreenshots_AttachesWorkdirAndTempShots(t *testing.T) {
	d := newShotDirs(t)
	s := NewScreenshots(media.New(&fakeBlobs{}), OSFS(), d.tmp)

	tmpShot := filepath.Join(d.tmp, "screenshot-1.png")
	writePNG(t, tmpShot, 4, 4)
	got := s.Attach(rcFor(d.work), "agent-browser screenshot", "Screenshot saved to "+tmpShot+"\n")
	if len(got) != 1 || got[0].MIME != "image/png" {
		t.Errorf("temp shot: got %+v, want 1 png media", got)
	}

	mkdirAll(t, filepath.Join(d.work, "shots"))
	writePNG(t, filepath.Join(d.work, "shots", "page.png"), 4, 4)
	got = s.Attach(rcFor(d.work), "  agent-browser screenshot shots/page.png", `Screenshot saved to "shots/page.png".`)
	if len(got) != 1 {
		t.Errorf("workdir shot: got %d media, want 1", len(got))
	}
}

func TestScreenshots_UsesLastPathInOutput(t *testing.T) {
	d := newShotDirs(t)
	s := NewScreenshots(media.New(&fakeBlobs{}), OSFS(), d.tmp)
	writePNG(t, filepath.Join(d.work, "b.png"), 4, 4)

	got := s.Attach(rcFor(d.work), "agent-browser screenshot b.png", "missing.png\nsaved b.png")
	if len(got) != 1 {
		t.Errorf("got %d media, want 1 (last path wins)", len(got))
	}
}

func TestScreenshots_RefusesEscapes(t *testing.T) {
	d := newShotDirs(t)
	outsidePNG := filepath.Join(d.outside, "x.png")
	writePNG(t, outsidePNG, 4, 4)

	// ../outside.png, next to the workdir.
	writePNG(t, filepath.Join(filepath.Dir(d.work), "outside.png"), 4, 4)
	// A workdir symlink to a file outside both roots.
	symlink(t, outsidePNG, filepath.Join(d.work, "link.png"))
	// A temp file not named screenshot*.
	writePNG(t, filepath.Join(d.tmp, "other.png"), 4, 4)
	// A screenshot* name reached through .. out of tmp.
	mkdirAll(t, filepath.Join(d.tmp, "sub"))
	escaped := filepath.Join(filepath.Dir(d.tmp), "x", "screenshot.png")
	mkdirAll(t, filepath.Dir(escaped))
	writePNG(t, escaped, 4, 4)
	// A temp symlink named screenshot* pointing outside both roots.
	symlink(t, outsidePNG, filepath.Join(d.tmp, "screenshot-evil.png"))
	// A temp symlink named screenshot* pointing at a non-screenshot temp file.
	symlink(t, filepath.Join(d.tmp, "other.png"), filepath.Join(d.tmp, "screenshot-alias.png"))
	// A directory named like an image.
	mkdirAll(t, filepath.Join(d.work, "x.png"))
	// A workdir file that does not exist.

	cases := []struct {
		name   string
		output string
	}{
		{"absolute outside both roots", "Screenshot saved to " + outsidePNG},
		{"dot-dot out of workdir", "Screenshot saved to ../outside.png"},
		{"workdir symlink to outside", "Screenshot saved to link.png"},
		{"temp file not named screenshot", "Screenshot saved to " + filepath.Join(d.tmp, "other.png")},
		{"dot-dot out of tmp", "Screenshot saved to " + d.tmp + "/sub/../../x/screenshot.png"},
		{"temp screenshot symlink to outside", "Screenshot saved to " + filepath.Join(d.tmp, "screenshot-evil.png")},
		{"temp screenshot symlink to other temp file", "Screenshot saved to " + filepath.Join(d.tmp, "screenshot-alias.png")},
		{"directory named x.png", "Screenshot saved to x.png"},
		{"missing file", "Screenshot saved to nope.png"},
		{"no path", "Screenshot saved"},
	}
	s := NewScreenshots(media.New(&fakeBlobs{}), OSFS(), d.tmp)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.Attach(rcFor(d.work), "agent-browser screenshot", tc.output); got != nil {
				t.Errorf("got %+v, want nil", got)
			}
		})
	}
}

func TestScreenshots_RefusesOversizedFile(t *testing.T) {
	d := newShotDirs(t)
	writePNG(t, filepath.Join(d.work, "big.png"), 4, 4)
	img := media.New(&fakeBlobs{})

	if got := NewScreenshots(img, OSFS(), d.tmp).Attach(rcFor(d.work), "agent-browser screenshot", "big.png"); len(got) != 1 {
		t.Fatalf("control: got %d media, want 1", len(got))
	}
	big := bigStatFS{FS: OSFS(), size: 20<<20 + 1}
	if got := NewScreenshots(img, big, d.tmp).Attach(rcFor(d.work), "agent-browser screenshot", "big.png"); got != nil {
		t.Errorf("oversized: got %+v, want nil", got)
	}
}

// TestScreenshots_RefusesFIFO checks the regular-file check: reading a
// named pipe would block forever.
func TestScreenshots_RefusesFIFO(t *testing.T) {
	d := newShotDirs(t)
	if err := syscall.Mkfifo(filepath.Join(d.work, "pipe.png"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	s := NewScreenshots(media.New(&fakeBlobs{}), OSFS(), d.tmp)
	if got := s.Attach(rcFor(d.work), "agent-browser screenshot", "pipe.png"); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestScreenshots_RefusesCorruptImage(t *testing.T) {
	d := newShotDirs(t)
	if err := os.WriteFile(filepath.Join(d.work, "bad.png"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewScreenshots(media.New(&fakeBlobs{}), OSFS(), d.tmp)
	if got := s.Attach(rcFor(d.work), "agent-browser screenshot", "bad.png"); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestScreenshots_OnlyForScreenshotCommand(t *testing.T) {
	d := newShotDirs(t)
	writePNG(t, filepath.Join(d.work, "page.png"), 4, 4)
	s := NewScreenshots(media.New(&fakeBlobs{}), OSFS(), d.tmp)

	for _, cmd := range []string{
		"agent-browser snapshot",
		"echo agent-browser screenshot",
		"cat page.png; agent-browser screenshot",
		"agent-browser screenshotter",
	} {
		if got := s.Attach(rcFor(d.work), cmd, "page.png"); got != nil {
			t.Errorf("%q: got %+v, want nil", cmd, got)
		}
	}
}
