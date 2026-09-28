package blocklist

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/golden"
)

// textRender is the tests' RenderFunc: the item's Data string, split on \n.
func textRender(it Item, _ int, _ Styles) []string {
	return strings.Split(it.Data.(string), "\n")
}

// testItems builds items with IDs i0..iN-1 at Version 1.
func testItems(texts ...string) []Item {
	out := make([]Item, len(texts))
	for i, s := range texts {
		out[i] = Item{ID: fmt.Sprintf("i%d", i), Version: 1, Data: s}
	}
	return out
}

// oneLiners builds n single-line items "item 0".."item n-1".
func oneLiners(n int) []Item {
	texts := make([]string, n)
	for i := range texts {
		texts[i] = fmt.Sprintf("item %d", i)
	}
	return testItems(texts...)
}

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Bar:        lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")),
		SelectedBg: lipgloss.NewStyle().Background(lipgloss.Color("#202020")),
		MatchOn:    "\x1b[7m",
		MatchOff:   "\x1b[27m",
		Track:      lipgloss.Color("#444444"),
		Thumb:      lipgloss.Color("#aaaaaa"),
		ScrollBg:   lipgloss.Color("#000000"),
		Gap:        1,
	}
}

func newList(w, h int, items []Item, opts ...Option) Model {
	m := New(textRender, append([]Option{WithStyles(pinnedStyles())}, opts...)...)
	m.SetSize(w, h)
	m.SetItems(items)
	return m
}

func keyMsg(k string) tea.KeyPressMsg {
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	r := rune(k[0])
	return tea.KeyPressMsg{Code: r, Text: k}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(keyMsg(k))
	}
	return m
}

func selectedID(t *testing.T, m Model) string {
	t.Helper()
	it, ok := m.Selected()
	if !ok {
		t.Fatalf("no selection")
	}
	return it.ID
}

// visibleText returns View's rows with escapes stripped.
func visibleText(m Model) []string {
	return strings.Split(xansi.Strip(m.View()), "\n")
}

func containsRow(rows []string, sub string) bool {
	for _, r := range rows {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestNav_JKGgBottom(t *testing.T) {
	t.Parallel()
	m := newList(20, 10, oneLiners(5))

	if got := selectedID(t, m); got != "i4" {
		t.Fatalf("initial selection = %s, want the last item i4", got)
	}
	steps := []struct {
		key, want string
	}{
		{"j", "i4"}, // already last
		{"k", "i3"},
		{"k", "i2"},
		{"k", "i1"},
		{"k", "i0"},
		{"k", "i0"}, // already first
		{"j", "i1"},
		{"G", "i4"},
	}
	for i, s := range steps {
		m = press(m, s.key)
		if got := selectedID(t, m); got != s.want {
			t.Fatalf("step %d (%s): selection = %s, want %s", i, s.key, got, s.want)
		}
	}
	m.Top()
	if got := selectedID(t, m); got != "i0" {
		t.Errorf("Top: selection = %s, want i0", got)
	}
	m.Bottom()
	if got := selectedID(t, m); got != "i4" {
		t.Errorf("Bottom: selection = %s, want i4", got)
	}
	if m.Len() != 5 {
		t.Errorf("Len = %d, want 5", m.Len())
	}
}

func TestNav_EmptyList(t *testing.T) {
	t.Parallel()
	m := newList(20, 4, nil)
	m = press(m, "j", "k", "G", "ctrl+d", "ctrl+u", "n", "N")
	m.Top()
	m.Bottom()
	if _, ok := m.Selected(); ok {
		t.Errorf("empty list has a selection")
	}
	if m.Select("nope") {
		t.Errorf("Select on an empty list returned true")
	}
	rows := strings.Split(m.View(), "\n")
	if len(rows) != 4 {
		t.Errorf("View has %d rows, want 4", len(rows))
	}
}

func TestView_ExactSize(t *testing.T) {
	t.Parallel()
	mixes := map[string][]Item{
		"empty": nil,
		"one":   testItems("hello"),
		"mixed": testItems(
			"short",
			"a line that is much wider than any of the tested widths, so it must be truncated",
			"tall\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12",
			"",
			"\x1b[31mred text\x1b[0m and more \x1b[1mbold that is also long enough to cut\x1b[0m",
			"wide 漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字",
			"tab\there",
			"needle one\nneedle two",
		),
	}
	sizes := [][2]int{{20, 5}, {40, 10}, {3, 2}, {10, 1}, {7, 30}, {1, 3}, {2, 2}}
	for name, items := range mixes {
		for _, sz := range sizes {
			m := newList(sz[0], sz[1], items)
			m.SetSearch("needle")
			check := func(when string) {
				t.Helper()
				rows := strings.Split(m.View(), "\n")
				if len(rows) != sz[1] {
					t.Fatalf("%s %dx%d %s: %d rows, want %d", name, sz[0], sz[1], when, len(rows), sz[1])
				}
				for i, r := range rows {
					if w := ansi.Width(r); w != sz[0] {
						t.Fatalf("%s %dx%d %s: row %d width %d, want %d: %q", name, sz[0], sz[1], when, i, w, sz[0], r)
					}
				}
			}
			check("bottom")
			m.Top()
			check("top")
			m = press(m, "j", "j")
			check("j j")
			m = press(m, "ctrl+d")
			check("ctrl+d")
		}
	}
}

func TestView_ZeroSize(t *testing.T) {
	t.Parallel()
	m := newList(0, 0, oneLiners(3))
	if got := m.View(); got != "" {
		t.Errorf("View at 0x0 = %q, want empty", got)
	}
}

func TestSetItems_PreservesSelectionByID(t *testing.T) {
	t.Parallel()
	m := newList(20, 10, testItems("a", "b", "c", "d", "e"))
	if !m.Select("i2") {
		t.Fatalf("Select(i2) = false")
	}
	next := []Item{
		{ID: "x", Version: 1, Data: "x"},
		{ID: "i0", Version: 1, Data: "a"},
		{ID: "i2", Version: 2, Data: "c changed"},
		{ID: "i4", Version: 1, Data: "e"},
	}
	m.SetItems(next)
	it, ok := m.Selected()
	if !ok || it.ID != "i2" || it.Data != "c changed" {
		t.Fatalf("after SetItems selection = %+v, %v; want i2 (new version)", it, ok)
	}
	m.SetItems([]Item{{ID: "p", Version: 1, Data: "p"}, {ID: "q", Version: 1, Data: "q"}})
	if got := selectedID(t, m); got != "q" {
		t.Errorf("selection missing from new items: selection = %s, want the last item q", got)
	}
	if m.Select("i2") {
		t.Errorf("Select of a removed ID returned true")
	}
}

func TestSetItems_CopiesInput(t *testing.T) {
	t.Parallel()
	items := testItems("a", "b")
	m := newList(20, 5, items)
	items[0].Data = "mutated"
	if containsRow(visibleText(m), "mutated") {
		t.Errorf("SetItems kept a reference to the caller's slice")
	}
}

func TestBlocklist_EvictsFarLines(t *testing.T) {
	t.Parallel()
	const w, h = 40, 10
	texts := make([]string, 500)
	heights := make([]int, 500)
	for i := range texts {
		heights[i] = 1 + i%3
		lines := make([]string, heights[i])
		for k := range lines {
			lines[k] = fmt.Sprintf("item %d line %d", i, k)
		}
		texts[i] = strings.Join(lines, "\n")
	}
	m := newList(w, h, testItems(texts...))
	_ = m.View()

	m.Select("i250")
	_ = m.View()

	// Items overlapping [yOffset-2h, yOffset+3h), from the known heights
	// and the 1-line gap.
	lo, hi := m.yOffset-2*h, m.yOffset+3*h
	within := 0
	start := 0
	for _, ht := range heights {
		if start < hi && start+ht > lo {
			within++
		}
		start += ht + 1
	}
	if got := m.c.cachedLines(); got > within {
		t.Errorf("cached line entries = %d, want <= %d (items within 5h lines)", got, within)
	}
	if got := len(m.c.entries); got != 500 {
		t.Errorf("entries (heights) = %d, want all 500 retained", got)
	}

	before := m.c.renders
	m.Top()
	_ = m.View()
	if m.c.renders <= before {
		t.Errorf("scrolling back to the top did not re-render the evicted items (renders %d -> %d)", before, m.c.renders)
	}
	if !containsRow(visibleText(m), "item 0 line 0") {
		t.Errorf("item 0 not visible after scrolling back")
	}
}

func TestBlocklist_WarmViewDoesNotRender(t *testing.T) {
	t.Parallel()
	m := newList(40, 10, oneLiners(100))
	_ = m.View()
	before := m.c.renders
	m = press(m, "k")
	_ = m.View()
	if m.c.renders != before {
		t.Errorf("a warm View after k rendered %d items, want 0", m.c.renders-before)
	}
}

func TestBlocklist_PreviousWidthStaysCached(t *testing.T) {
	t.Parallel()
	m := newList(40, 10, oneLiners(500))
	_ = m.View()

	m.SetSize(60, 10) // e.g. the details split closing
	_ = m.View()
	before := m.c.renders
	m.SetSize(40, 10) // and opening again
	_ = m.View()
	if got := m.c.renders - before; got != 0 {
		t.Errorf("toggling back to the previous width rendered %d items, want 0", got)
	}
	m.SetSize(60, 10)
	_ = m.View()
	if got := m.c.renders - before; got != 0 {
		t.Errorf("toggling to the other cached width rendered %d items, want 0", got)
	}

	m.SetSize(50, 10) // a third width is new
	if got := m.c.renders - before; got < 500 {
		t.Errorf("a new width rendered %d items for heights, want all 500", got)
	}
}

func TestBlocklist_NewVersionAtPreviousWidthRenders(t *testing.T) {
	t.Parallel()
	m := newList(40, 10, testItems("a", "b"))
	_ = m.View()
	m.SetSize(60, 10)
	_ = m.View()
	m.Upsert(Item{ID: "i1", Version: 2, Data: "b changed"})
	m.SetSize(40, 10)
	if !containsRow(visibleText(m), "b changed") {
		t.Errorf("a stale previous-width render was shown for a new version")
	}
}

func TestBlocklist_StylesVersionInvalidatesCache(t *testing.T) {
	t.Parallel()
	m := newList(40, 10, oneLiners(50))
	_ = m.View()

	before := m.c.renders
	m.SetStyles(pinnedStyles(), 0)
	_ = m.View()
	if m.c.renders != before {
		t.Errorf("SetStyles at the same version re-rendered %d items", m.c.renders-before)
	}

	m.SetStyles(pinnedStyles(), 2)
	_ = m.View()
	// 10 rows with a 1-line gap show 5 items.
	if got := m.c.renders - before; got < 5 {
		t.Errorf("SetStyles(st, 2) re-rendered %d items, want every visible item (>= 5)", got)
	}
}

func TestBlocklist_RenderGetsInnerWidthAndStyles(t *testing.T) {
	t.Parallel()
	var gotWidth int
	var gotGap int
	render := func(it Item, width int, st Styles) []string {
		gotWidth, gotGap = width, st.Gap
		return []string{"x"}
	}
	st := pinnedStyles()
	st.Gap = 3
	m := New(render, WithStyles(st))
	m.SetSize(30, 5)
	m.SetItems(oneLiners(1))
	_ = m.View()
	if gotWidth != 28 || gotGap != 3 {
		t.Errorf("RenderFunc got width %d, Gap %d; want 28, 3", gotWidth, gotGap)
	}
}

func TestBlocklist_WithKeyMap(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	km.Down.SetKeys("x")
	m := newList(20, 10, oneLiners(3), WithKeyMap(km))
	m.Top()
	m = press(m, "j")
	if got := selectedID(t, m); got != "i0" {
		t.Errorf("unbound j moved the selection to %s", got)
	}
	m = press(m, "x")
	if got := selectedID(t, m); got != "i1" {
		t.Errorf("remapped Down: selection = %s, want i1", got)
	}
}

func TestView_SelectionDrawing(t *testing.T) {
	t.Parallel()
	st := pinnedStyles()
	m := newList(12, 3, testItems("aa", "bb"))
	rows := strings.Split(m.View(), "\n")
	// The selected item (bb, the last) is at row 2; rows 0 and 1 are aa and the gap.
	bar := st.Bar.Render("▌")
	if !strings.HasPrefix(rows[2], bar) {
		t.Errorf("selected row %q does not start with the bar %q", rows[2], bar)
	}
	if !strings.HasPrefix(rows[0], " aa") {
		t.Errorf("unselected row %q does not start with a 1-space prefix", rows[0])
	}
	if !strings.Contains(rows[2], "\x1b[48;2;32;32;32m") {
		t.Errorf("selected row %q lacks the SelectedBg background", rows[2])
	}
}

func TestView_HighlightOff(t *testing.T) {
	t.Parallel()
	m := newList(12, 3, testItems("aa", "bb"))
	m.SetHighlight(false)
	rows := strings.Split(m.View(), "\n")
	if !strings.HasPrefix(rows[2], " bb") {
		t.Errorf("with highlight off, selected row %q does not start with a 1-space prefix", rows[2])
	}
	if strings.Contains(rows[2], "\x1b[48;2;32;32;32m") {
		t.Errorf("with highlight off, selected row %q still has the SelectedBg background", rows[2])
	}
	if got := selectedID(t, m); got != "i1" {
		t.Errorf("highlight off moved the selection to %s", got)
	}
	m.SetHighlight(true)
	if rows := strings.Split(m.View(), "\n"); !strings.HasPrefix(rows[2], pinnedStyles().Bar.Render("▌")) {
		t.Errorf("highlight back on: selected row %q lacks the bar", rows[2])
	}
}

func TestGolden_ListSelected(t *testing.T) {
	t.Parallel()
	m := newList(30, 8, testItems(
		"first item",
		"second item\nwith two lines",
		"\x1b[1mbold\x1b[0m third",
		"fourth",
		"fifth\nis\ntall",
		"sixth",
	))
	m.Top()
	m = press(m, "j", "j")
	golden.Assert(t, "list_selected", m.View())
}

func TestGolden_ListSearch(t *testing.T) {
	t.Parallel()
	m := newList(30, 8, testItems(
		"alpha",
		"find the Needle here",
		"beta\nneedle again",
		"gamma",
	))
	m.SetSearch("needle")
	m.Top()
	m = press(m, "n")
	golden.Assert(t, "list_search", m.View())
}
