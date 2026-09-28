package blocklist

import (
	"fmt"
	"strings"
	"testing"
)

func TestSearch_HighlightsAndNavigates(t *testing.T) {
	t.Parallel()
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = fmt.Sprintf("item %d", i)
	}
	texts[2] = "item 2 has a needle"
	texts[5] = "item 5\nsecond line NEEDLE"
	texts[8] = "Needle in item 8"
	m := newList(40, 30, testItems(texts...))

	if n := m.SetSearch("needle"); n != 3 {
		t.Fatalf("SetSearch = %d, want 3", n)
	}
	m.Top()
	for i, want := range []string{"i2", "i5", "i8", "i2"} {
		m = press(m, "n")
		if got := selectedID(t, m); got != want {
			t.Fatalf("n #%d: selection = %s, want %s", i+1, got, want)
		}
	}
	for i, want := range []string{"i8", "i5", "i2", "i8"} {
		m = press(m, "N")
		if got := selectedID(t, m); got != want {
			t.Fatalf("N #%d: selection = %s, want %s", i+1, got, want)
		}
	}

	view := m.View()
	for _, want := range []string{"\x1b[7mneedle\x1b[27m", "\x1b[7mNEEDLE\x1b[27m", "\x1b[7mNeedle\x1b[27m"} {
		if !strings.Contains(view, want) {
			t.Errorf("View lacks the highlight %q", want)
		}
	}

	if n := m.SetSearch(""); n != 0 {
		t.Errorf("SetSearch(\"\") = %d, want 0", n)
	}
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Errorf("View still highlights after clearing the search")
	}
	before := selectedID(t, m)
	m = press(m, "n", "N")
	if got := selectedID(t, m); got != before {
		t.Errorf("n/N with no search moved the selection %s -> %s", before, got)
	}
}

func TestSearch_NoMatchesKeepsSelection(t *testing.T) {
	t.Parallel()
	m := newList(40, 10, oneLiners(5))
	if n := m.SetSearch("zzz"); n != 0 {
		t.Fatalf("SetSearch = %d, want 0", n)
	}
	m = press(m, "n")
	if got := selectedID(t, m); got != "i4" {
		t.Errorf("n with no matches moved the selection to %s", got)
	}
}

func TestSearch_DoesNotMatchInsideEscapes(t *testing.T) {
	t.Parallel()
	m := newList(40, 10, testItems("\x1b[31mred\x1b[0m", "plain"))
	if n := m.SetSearch("31m"); n != 0 {
		t.Errorf("SetSearch(\"31m\") = %d, want 0 (the query is only inside an escape)", n)
	}
	if n := m.SetSearch("red"); n != 1 {
		t.Errorf("SetSearch(\"red\") = %d, want 1", n)
	}
}

func TestSearch_TracksUpsertsAndEvictedItems(t *testing.T) {
	t.Parallel()
	m := newList(40, 6, oneLiners(300))
	_ = m.View() // evicts the far items' lines
	if n := m.SetSearch("item 7"); n != 11 {
		// item 7, item 70..79
		t.Fatalf("SetSearch over evicted items = %d, want 11", n)
	}
	m.Upsert(Item{ID: "i1", Version: 2, Data: "now item 7 too"})
	m.Top()
	m = press(m, "n")
	if got := selectedID(t, m); got != "i1" {
		t.Errorf("n after an Upsert made i1 match: selection = %s, want i1", got)
	}
}
