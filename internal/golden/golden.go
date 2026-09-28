// Package golden compares rendered output against files checked into
// testdata/golden, so tests can catch unintended changes to terminal frames.
package golden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Assert compares got with testdata/golden/<name>.ansi (relative to the
// test's package dir). With JIG_UPDATE_GOLDEN=1 it (re)writes the file and
// passes. On mismatch it writes <name>.ansi.actual and fails, reporting the
// first differing line of each side with %q.
func Assert(t testing.TB, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name+".ansi")

	if os.Getenv("JIG_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden: mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("golden: write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: read %s: %v (run with JIG_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) == got {
		return
	}

	actualPath := path + ".actual"
	if err := os.WriteFile(actualPath, []byte(got), 0o644); err != nil {
		t.Fatalf("golden: write %s: %v", actualPath, err)
	}

	wantLine, gotLine := firstDiffLine(string(want), got)
	t.Errorf("golden: %s does not match (wrote %s)\nwant: %q\ngot:  %q", path, actualPath, wantLine, gotLine)
}

// firstDiffLine returns the first line that differs between want and got
// (comparing line by line), or the first extra line on whichever side is
// longer once the shorter side is exhausted.
func firstDiffLine(want, got string) (string, string) {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	n := min(len(gotLines), len(wantLines))
	for i := range n {
		if wantLines[i] != gotLines[i] {
			return wantLines[i], gotLines[i]
		}
	}
	if len(wantLines) > n {
		return wantLines[n], ""
	}
	if len(gotLines) > n {
		return "", gotLines[n]
	}
	return "", ""
}
