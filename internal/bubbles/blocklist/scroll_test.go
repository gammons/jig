package blocklist

import (
	"fmt"
	"strings"
	"testing"
)

func noGap() Option {
	st := pinnedStyles()
	st.Gap = 0
	return WithStyles(st)
}

func TestScroll_KeepsSelectionVisible(t *testing.T) {
	t.Parallel()
	tall := make([]string, 12)
	for i := range tall {
		tall[i] = fmt.Sprintf("tall %d", i)
	}
	m := newList(30, 5, testItems("above", strings.Join(tall, "\n"), "below"))
	m.Top()
	if !containsRow(visibleText(m), "above") {
		t.Fatalf("Top: first item not visible")
	}

	m = press(m, "j")
	if got := selectedID(t, m); got != "i1" {
		t.Fatalf("selection = %s, want i1", got)
	}
	if !containsRow(visibleText(m), "tall 0") {
		t.Errorf("a selected item taller than h does not show its first line:\n%s", strings.Join(visibleText(m), "\n"))
	}

	m = press(m, "j")
	if !containsRow(visibleText(m), "below") {
		t.Errorf("the item below the tall one is not visible after j")
	}

	m = press(m, "k")
	if !containsRow(visibleText(m), "tall 0") {
		t.Errorf("moving back up onto the tall item does not show its first line")
	}
}

func TestScroll_MinimalMovement(t *testing.T) {
	t.Parallel()
	m := newList(20, 5, oneLiners(20), noGap())
	m.Top()
	m = press(m, "j", "j", "j", "j")
	if m.yOffset != 0 {
		t.Errorf("moving within the view scrolled to %d, want 0", m.yOffset)
	}
	m = press(m, "j")
	if m.yOffset != 1 {
		t.Errorf("moving one past the view: yOffset = %d, want 1 (minimal)", m.yOffset)
	}
	for range 10 {
		m = press(m, "j")
		if !containsRow(visibleText(m), fmt.Sprintf("item %d", m.sel)) {
			t.Fatalf("selected item %d not visible", m.sel)
		}
	}
	m.Top()
	if m.yOffset != 0 {
		t.Errorf("Top: yOffset = %d, want 0", m.yOffset)
	}
}

func TestScroll_WholeItemWhenItFits(t *testing.T) {
	t.Parallel()
	m := newList(20, 5, testItems("a", "b", "c\nc1\nc2"), noGap())
	m.Top()
	m = press(m, "j", "j")
	rows := visibleText(m)
	for _, want := range []string{"c", "c1", "c2"} {
		if !containsRow(rows, want) {
			t.Errorf("row %q of the selected 3-line item not visible:\n%s", want, strings.Join(rows, "\n"))
		}
	}
}

func TestPinned_StaysAtBottomOnUpsert(t *testing.T) {
	t.Parallel()
	const h = 5
	m := newList(30, h, oneLiners(20))
	if got := selectedID(t, m); got != "i19" {
		t.Fatalf("selection = %s, want i19", got)
	}
	if m.yOffset != m.total-h {
		t.Fatalf("initial yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}

	text := "item 19"
	for v := 2; v < 8; v++ {
		text += fmt.Sprintf("\ngrowing %d", v)
		m.Upsert(Item{ID: "i19", Version: v, Data: text})
		if m.yOffset != m.total-h {
			t.Fatalf("v%d: yOffset = %d, want bottom %d", v, m.yOffset, m.total-h)
		}
		if rows := visibleText(m); !strings.Contains(rows[h-1], fmt.Sprintf("growing %d", v)) {
			t.Fatalf("v%d: last row = %q, want the newest line", v, rows[h-1])
		}
	}

	m.Select("i5")
	y0 := m.yOffset
	for v := 8; v < 12; v++ {
		text += fmt.Sprintf("\ngrowing %d", v)
		m.Upsert(Item{ID: "i19", Version: v, Data: text})
		if m.yOffset != y0 {
			t.Fatalf("selection elsewhere: yOffset moved %d -> %d when the last item grew", y0, m.yOffset)
		}
	}
}

func TestPinned_FollowsAppend(t *testing.T) {
	t.Parallel()
	const h = 5
	m := newList(30, h, oneLiners(10))
	m.Upsert(Item{ID: "new", Version: 1, Data: "appended\nblock"})
	if got := selectedID(t, m); got != "new" {
		t.Errorf("pinned selection did not follow the appended item: %s", got)
	}
	if m.yOffset != m.total-h {
		t.Errorf("yOffset = %d, want bottom %d", m.yOffset, m.total-h)
	}

	m.Select("i2")
	y0 := m.yOffset
	m.Upsert(Item{ID: "new2", Version: 1, Data: "another"})
	if got := selectedID(t, m); got != "i2" {
		t.Errorf("unpinned selection moved to %s on append", got)
	}
	if m.yOffset != y0 {
		t.Errorf("unpinned yOffset moved %d -> %d on append", y0, m.yOffset)
	}

	m.Bottom()
	next := append(testItems(), m.items...)
	next = append(next, Item{ID: "new3", Version: 1, Data: "via SetItems"})
	m.SetItems(next)
	if got := selectedID(t, m); got != "new3" {
		t.Errorf("pinned selection did not follow an item appended by SetItems: %s", got)
	}
}

func TestPinned_OnResize(t *testing.T) {
	t.Parallel()
	m := newList(30, 5, oneLiners(20))
	m.SetSize(30, 8)
	if m.yOffset != m.total-8 {
		t.Errorf("after growing: yOffset = %d, want bottom %d", m.yOffset, m.total-8)
	}
	m.SetSize(30, 3)
	if m.yOffset != m.total-3 {
		t.Errorf("after shrinking: yOffset = %d, want bottom %d", m.yOffset, m.total-3)
	}
}

func TestResize_KeepsTopItemAnchored(t *testing.T) {
	t.Parallel()
	long := "word word word word word word word word word word"
	render := func(it Item, width int, _ Styles) []string {
		// Wrap at width so a resize changes item heights.
		s := it.Data.(string)
		var lines []string
		for len(s) > width {
			lines = append(lines, s[:width])
			s = s[width:]
		}
		return append(lines, s)
	}
	items := make([]Item, 30)
	for i := range items {
		items[i] = Item{ID: fmt.Sprintf("i%d", i), Version: 1, Data: fmt.Sprintf("%02d %s", i, long)}
	}
	m := New(render, WithStyles(pinnedStyles()))
	m.SetSize(40, 10)
	m.SetItems(items)
	m.Select("i10")
	top := itemAt(m.offsets, m.yOffset)
	if m.yOffset != m.offsets[top] {
		t.Fatalf("setup: yOffset %d is not at an item start", m.yOffset)
	}
	m.SetSize(25, 10)
	if got := m.offsets[top]; m.yOffset != got {
		t.Errorf("after rewrap: yOffset = %d, want the old top item's new start %d", m.yOffset, got)
	}
	if !containsRow(visibleText(m), "10 ") {
		t.Errorf("selection not visible after resize")
	}
}

func TestHalfPage(t *testing.T) {
	t.Parallel()
	const h = 10
	m := newList(20, h, oneLiners(40), noGap())
	m.Top()

	m = press(m, "ctrl+d")
	if got := selectedID(t, m); got != "i5" {
		t.Errorf("ctrl+d: selection = %s, want i5", got)
	}
	if m.yOffset != 5 {
		t.Errorf("ctrl+d: yOffset = %d, want 5", m.yOffset)
	}
	m = press(m, "ctrl+d")
	if got := selectedID(t, m); got != "i10" || m.yOffset != 10 {
		t.Errorf("ctrl+d x2: selection %s yOffset %d, want i10 at 10", got, m.yOffset)
	}
	m = press(m, "ctrl+u")
	if got := selectedID(t, m); got != "i5" || m.yOffset != 5 {
		t.Errorf("ctrl+u: selection %s yOffset %d, want i5 at 5", got, m.yOffset)
	}
	m = press(m, "ctrl+u", "ctrl+u")
	if got := selectedID(t, m); got != "i0" || m.yOffset != 0 {
		t.Errorf("ctrl+u to the top: selection %s yOffset %d, want i0 at 0", got, m.yOffset)
	}
	for range 10 {
		m = press(m, "ctrl+d")
	}
	if got := selectedID(t, m); got != "i39" || m.yOffset != 30 {
		t.Errorf("ctrl+d to the end: selection %s yOffset %d, want i39 at 30", got, m.yOffset)
	}
}

func TestHalfPage_NotStuckOnTallItem(t *testing.T) {
	t.Parallel()
	tall := strings.Repeat("x\n", 30) + "x"
	m := newList(20, 6, testItems("a", tall, "b"))
	m.Top()
	m = press(m, "ctrl+d")
	if got := selectedID(t, m); got != "i1" {
		t.Fatalf("ctrl+d: selection = %s, want i1", got)
	}
	m = press(m, "ctrl+d")
	if got := selectedID(t, m); got != "i2" {
		t.Errorf("ctrl+d inside a tall item stayed on %s, want i2", got)
	}
}
