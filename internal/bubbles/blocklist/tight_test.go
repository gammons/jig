package blocklist

import (
	"reflect"
	"strings"
	"testing"
)

// tightItems is a, then b and c each Tight (no gap before them), then d.
func tightItems() []Item {
	items := testItems("a", "b\nb1", "c", "d")
	items[1].Tight, items[2].Tight = true, true
	return items
}

// trimmed is visibleText with each row's selection prefix and scrollbar
// column (its first and last cells) and padding removed.
func trimmed(m Model) []string {
	rows := visibleText(m)
	for i, r := range rows {
		rs := []rune(r)
		rows[i] = strings.TrimSpace(string(rs[1 : len(rs)-1]))
	}
	return rows
}

func TestTight_NoGapBeforeATightItem(t *testing.T) {
	t.Parallel()
	m := newList(20, 8, tightItems())
	m.Top()
	want := []string{"a", "b", "b1", "c", "", "d", "", ""}
	if got := trimmed(m); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q\nwant   %q", got, want)
	}
	if wantOff := []int{0, 1, 3, 5, 7}; !reflect.DeepEqual(m.offsets, wantOff) {
		t.Errorf("offsets = %v, want %v", m.offsets, wantOff)
	}
	if m.total != 6 {
		t.Errorf("total = %d, want 6", m.total)
	}
}

func TestTight_TightFirstItemHasNothingToJoin(t *testing.T) {
	t.Parallel()
	items := testItems("a", "b")
	items[0].Tight = true
	m := newList(20, 4, items)
	m.Top()
	if got, want := trimmed(m), []string{"a", "", "b", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestTight_EnsureVisibleUsesTheItemsOwnHeight(t *testing.T) {
	t.Parallel()
	// h=3: selecting b (2 lines, tight under a) must show both its lines
	// and must not scroll further for a gap that is not there.
	m := newList(20, 3, tightItems())
	m.Top()
	m = press(m, "j")
	if got := selectedID(t, m); got != "i1" {
		t.Fatalf("selection = %s, want i1", got)
	}
	if got, want := trimmed(m), []string{"a", "b", "b1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q, want %q (no scroll needed)", got, want)
	}
	m = press(m, "j")
	if got, want := trimmed(m), []string{"b", "b1", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after j to c: rows = %q, want %q (scrolled by one)", got, want)
	}
}

func TestTight_HitTest(t *testing.T) {
	t.Parallel()
	m := newList(20, 8, tightItems())
	m.Top()
	tests := []struct {
		y    int
		id   string
		line int
	}{
		{0, "i0", 0}, {1, "i1", 0}, {2, "i1", 1}, {3, "i2", 0},
		{4, "i2", 0}, // the gap row under c snaps to c's last line
		{5, "i3", 0},
	}
	for _, tt := range tests {
		id, line, _, ok := HitTest(m, 2, tt.y)
		if !ok || id != tt.id || line != tt.line {
			t.Errorf("HitTest(2, %d) = %s:%d ok=%v, want %s:%d", tt.y, id, line, ok, tt.id, tt.line)
		}
	}
}

func TestTight_UpsertAppendsTight(t *testing.T) {
	t.Parallel()
	m := newList(20, 6, testItems("a"))
	m.Top()
	m.Upsert(Item{ID: "i1", Version: 1, Data: "b", Tight: true})
	if got, want := trimmed(m), []string{"a", "b", "", "", "", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}
