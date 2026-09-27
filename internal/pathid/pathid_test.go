package pathid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKey_SymlinkedDirMatchesTarget(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if Key(link) != Key(real) {
		t.Errorf("Key(%q) = %q, Key(%q) = %q; want equal", link, Key(link), real, Key(real))
	}
}

func TestKey_MissingPathFallsBackToAbs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope", "..", "gone")
	want, err := filepath.Abs(missing)
	if err != nil {
		t.Fatal(err)
	}
	if got := Key(missing); got != want {
		t.Errorf("Key(%q) = %q, want %q", missing, got, want)
	}
}

func TestKey_RelativePathIsAbsolute(t *testing.T) {
	if got := Key("some/missing/rel"); !filepath.IsAbs(got) {
		t.Errorf("Key = %q, want an absolute path", got)
	}
}
