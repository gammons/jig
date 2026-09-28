package coderender

import (
	"fmt"
	"strings"
	"testing"

	udiff "github.com/aymanbagabas/go-udiff"

	"github.com/gammons/jig/internal/golden"
)

// twentyLineChange builds a 20-line before/after pair with changes to
// lines 3-4 (a two-line replace) and line 15 (a one-line replace), so
// context 3 keeps the two changes in separate hunks (line 15's context
// starts more than 2*context lines after line 3-4's hunk ends) while
// still pulling in enough leading context on the first hunk (only 2 lines
// are available before line 3) to reach a 7-line hunk.
func twentyLineChange() (before, after string) {
	var b, a []string
	for i := 1; i <= 20; i++ {
		b = append(b, fmt.Sprintf("line%d", i))
		a = append(a, fmt.Sprintf("line%d", i))
	}
	a[2] = "line3 CHANGED"
	a[3] = "line4 CHANGED"
	a[14] = "line15 CHANGED"
	return strings.Join(b, "\n") + "\n", strings.Join(a, "\n") + "\n"
}

func TestDiff_HunksAndContext(t *testing.T) {
	t.Parallel()
	before, after := twentyLineChange()
	st := testStyles()

	lines, hunks := Diff("file.txt", before, after, 3, st)

	if hunks != 2 {
		t.Fatalf("hunks = %d, want 2", hunks)
	}
	wantHeader := st.Hunk.Render("@@ -1,7 +1,7 @@")
	if len(lines) == 0 || lines[0] != wantHeader {
		t.Fatalf("lines[0] = %q, want %q", firstOrEmpty(lines), wantHeader)
	}
}

func firstOrEmpty(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func TestDiffText_MatchesUdiff(t *testing.T) {
	t.Parallel()
	before, after := twentyLineChange()

	got := DiffText(before, after, 3)

	edits := udiff.Lines(before, after)
	want, err := udiff.ToUnified("before", "after", before, edits, 3)
	if err != nil {
		t.Fatalf("udiff.ToUnified: %v", err)
	}
	if got != want {
		t.Errorf("DiffText(...) = %q, want %q", got, want)
	}
}

func TestDiff_Golden(t *testing.T) {
	t.Parallel()
	before := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	after := "package main\n\nfunc main() {\n\tprintln(\"hello, world\")\n}\n"

	lines, _ := Diff("main.go", before, after, 3, testStyles())
	golden.Assert(t, "diff_go", strings.Join(lines, "\n"))
}
