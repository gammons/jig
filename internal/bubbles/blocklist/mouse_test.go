package blocklist

import (
	"strconv"
	"strings"
	"testing"
)

// tallText returns n lines of numbered text, "line 0".."line n-1", joined
// as one item's Data.
func tallText(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	return strings.Join(lines, "\n")
}

func TestBlocklist_ScrollByFollowsMiddleRow(t *testing.T) {
	t.Parallel()
	const w, h = 30, 10
	// Heights 3, 40, 1; gap 1 (pinnedStyles' default). Offsets: 0, 4, 45;
	// total 46.
	m := newList(w, h, testItems(tallText(3), tallText(40), tallText(1)))
	if !m.flags.follow || m.sel != 2 {
		t.Fatalf("setup: follow=%v sel=%d, want follow=true sel=2 (last)", m.flags.follow, m.sel)
	}
	if m.yOffset != m.total-h {
		t.Fatalf("setup: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}

	m.ScrollBy(-3)
	if want := m.total - h - 3; m.yOffset != want {
		t.Fatalf("ScrollBy(-3): yOffset = %d, want %d", m.yOffset, want)
	}
	if got := selectedID(t, m); got != "i1" {
		t.Fatalf("ScrollBy(-3): selection = %s, want i1 (the 40-line item)", got)
	}
	if m.flags.follow {
		t.Errorf("ScrollBy(-3): follow still true after leaving the bottom")
	}

	// Continuing: repeated ScrollBy(-3) keeps i1 selected, yOffset moving
	// by exactly 3 each step, until it clamps at 0.
	prev := m.yOffset
	for range 20 {
		m.ScrollBy(-3)
		if delta := prev - m.yOffset; delta != 3 && m.yOffset != 0 {
			t.Fatalf("yOffset step = %d (from %d to %d), want 3 (or clamp to 0)", delta, prev, m.yOffset)
		}
		if m.yOffset == 0 {
			break
		}
		if got := selectedID(t, m); got != "i1" {
			t.Fatalf("yOffset=%d: selection = %s, want i1", m.yOffset, got)
		}
		prev = m.yOffset
	}
	if m.yOffset != 0 {
		t.Fatalf("did not reach yOffset 0")
	}
	if got := selectedID(t, m); got != "i0" {
		t.Errorf("at the top: selection = %s, want i0", got)
	}

	m.ScrollBy(1000)
	if got := selectedID(t, m); got != "i2" {
		t.Errorf("ScrollBy(+1000): selection = %s, want i2 (last)", got)
	}
	if m.yOffset != m.total-h {
		t.Errorf("ScrollBy(+1000): yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}
	if !m.flags.follow {
		t.Errorf("ScrollBy(+1000): follow not resumed")
	}
}

func TestBlocklist_ScrollByNeverJumpsToBlockTop(t *testing.T) {
	t.Parallel()
	const w, h = 30, 10
	// Heights 1, 100, 1; gap 1. Offsets: 0, 2, 103; total 104.
	m := newList(w, h, testItems(tallText(1), tallText(100), tallText(1)))
	if m.yOffset != m.total-h {
		t.Fatalf("setup: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}
	for range 25 {
		before := m.yOffset
		m.ScrollBy(-3)
		if got := selectedID(t, m); got == "i1" {
			if m.yOffset == m.offsets[1] {
				t.Fatalf("yOffset snapped to the tall item's first line (%d) instead of scrolling by 3 from %d", m.offsets[1], before)
			}
		}
		if m.yOffset == 0 {
			break
		}
	}
}

func TestBlocklist_ScrolledUpInsideLastBlockStaysPut(t *testing.T) {
	t.Parallel()
	const w, h = 30, 10
	// item i0: 5 lines; last item i1: 40 lines. gap 1. offsets: 0, 6, 47;
	// total 46.
	m := newList(w, h, testItems(tallText(5), tallText(40)))
	if m.yOffset != m.total-h {
		t.Fatalf("setup: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}

	m.ScrollBy(-6)
	if m.yOffset != m.total-h-6 {
		t.Fatalf("ScrollBy(-6): yOffset = %d, want %d", m.yOffset, m.total-h-6)
	}
	if m.flags.follow {
		t.Fatalf("ScrollBy(-6): follow still true")
	}
	y0 := m.yOffset

	// Streaming: the last item grows; yOffset stays put.
	m.Upsert(Item{ID: "i1", Version: 2, Data: tallText(45)})
	if m.yOffset != y0 {
		t.Errorf("Upsert while scrolled up: yOffset moved %d -> %d, want unchanged", y0, m.yOffset)
	}

	// Resuming: scrolling to the bottom, then growing the last item keeps
	// the view pinned to the (new) bottom.
	m.ScrollBy(1000)
	if !m.flags.follow {
		t.Fatalf("ScrollBy(+1000) did not resume following")
	}
	m.Upsert(Item{ID: "i1", Version: 3, Data: tallText(50)})
	if m.yOffset != m.total-h {
		t.Errorf("Upsert after resuming: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}
}

func TestBlocklist_KeysKeepFollowing(t *testing.T) {
	t.Parallel()
	const h = 5
	m := newList(30, h, oneLiners(20))

	// j onto the last item (already there; move away and back).
	m = press(m, "k", "k")
	if m.flags.follow {
		t.Fatalf("k moved off the last item but follow is still true")
	}
	m = press(m, "j", "j")
	if !m.flags.follow {
		t.Errorf("j back onto the last item did not set follow")
	}
	m.Upsert(Item{ID: "i19", Version: 2, Data: "item 19\ngrew"})
	if m.yOffset != m.total-h {
		t.Errorf("after j onto last + Upsert: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}

	m = press(m, "k", "k", "k")
	m = press(m, "G")
	if !m.flags.follow {
		t.Errorf("G did not set follow")
	}
	m.Upsert(Item{ID: "i19", Version: 3, Data: "item 19\ngrew\nmore"})
	if m.yOffset != m.total-h {
		t.Errorf("after G + Upsert: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}

	m.Top()
	for range 20 {
		m = press(m, "ctrl+d")
	}
	if got := selectedID(t, m); got != "i19" {
		t.Fatalf("ctrl+d did not reach the last item: selection = %s", got)
	}
	if !m.flags.follow {
		t.Errorf("ctrl+d to the end did not set follow")
	}
	m.Upsert(Item{ID: "i19", Version: 4, Data: "item 19\ngrew\nmore\nstill"})
	if m.yOffset != m.total-h {
		t.Errorf("after ctrl+d to end + Upsert: yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}
}

func TestBlocklist_HitTest(t *testing.T) {
	t.Parallel()
	const w, h = 12, 6
	// i0: 2 lines, i1: 3 lines; gap 1. offsets: 0, 3, 7; total 6.
	m := newList(w, h, testItems("a0\na1", "b0\nb1\nb2"))
	m.Top()

	if _, _, _, ok := HitTest(m, 0, 0); ok {
		t.Errorf("x=0 (prefix column): ok = true, want false")
	}
	if _, _, _, ok := HitTest(m, w-1, 0); ok {
		t.Errorf("x=w-1 (scrollbar column): ok = true, want false")
	}
	if _, _, _, ok := HitTest(m, 3, h); ok {
		t.Errorf("y=h (out of range): ok = true, want false")
	}
	if _, _, _, ok := HitTest(m, 3, -1); ok {
		t.Errorf("y=-1 (out of range): ok = true, want false")
	}

	// Row 0 is i0's first line ("a0"), row 1 is i0's second line ("a1").
	id, line, col, ok := HitTest(m, 3, 0)
	if !ok || id != "i0" || line != 0 || col != 2 {
		t.Errorf("HitTest(3,0) = %q,%d,%d,%v; want i0,0,2,true", id, line, col, ok)
	}
	id, line, col, ok = HitTest(m, 3, 1)
	if !ok || id != "i0" || line != 1 || col != 2 {
		t.Errorf("HitTest(3,1) = %q,%d,%d,%v; want i0,1,2,true", id, line, col, ok)
	}

	// Row 2 is the gap between i0 and i1: nearest is i0's last line.
	id, line, _, ok = HitTest(m, 3, 2)
	if !ok || id != "i0" || line != 1 {
		t.Errorf("HitTest gap row = %q,%d,%v; want i0,1,true", id, line, ok)
	}

	// Row 3 is i1's first line ("b0").
	id, line, _, ok = HitTest(m, 3, 3)
	if !ok || id != "i1" || line != 0 {
		t.Errorf("HitTest(3,3) = %q,%d,%v; want i1,0,true", id, line, ok)
	}

	// A leading gap row: scroll so the gap line (line 2) is the view's
	// first visible row. It snaps to the item below (i1's first line),
	// not the item above, since there's no item content shown above it.
	m2 := m
	m2.yOffset = 2
	id, line, _, ok = HitTest(m2, 3, 0)
	if !ok || id != "i1" || line != 0 {
		t.Errorf("leading gap row = %q,%d,%v; want i1,0,true", id, line, ok)
	}

	got := Lines(m, "i1")
	want := []string{"b0" + strings.Repeat(" ", w-2-2), "b1" + strings.Repeat(" ", w-2-2), "b2" + strings.Repeat(" ", w-2-2)}
	if len(got) != len(want) {
		t.Fatalf("Lines(i1) = %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lines(i1)[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := Lines(m, "nope"); got != nil {
		t.Errorf("Lines of an unknown ID = %v, want nil", got)
	}
}
