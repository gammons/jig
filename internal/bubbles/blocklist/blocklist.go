// Package blocklist is jig's transcript list: a vertical list of
// variable-height blocks with a block cursor, a per-item render cache,
// yOffset scrolling that stays pinned to the bottom while the cursor is on
// the last block, and search. It mirrors slk's message-list shape (a
// flattened line space addressed by per-item offsets and a yOffset), not
// bubbles/viewport.
package blocklist

import (
	"image/color"
	"maps"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Item is one block. The widget renders it through RenderFunc and treats
// Data as opaque; bump Version whenever anything that affects the render
// changes, since (ID, Version, width, stylesVersion) is the cache key.
type Item struct {
	ID      string
	Version int
	Data    any
	// HalfEdges marks an item whose first and last lines are half-filled
	// edges (▄/▀, a panel's half-row border). Selected, those rows get
	// the lower/upper half of the bar (▖/▘) and no SelectedBg, so the bar
	// spans exactly the panel.
	HalfEdges bool
}

// RenderFunc renders an item into lines at most width cells wide (wider
// lines are cut, narrower ones padded). It must be deterministic for a
// given (item, width, styles).
type RenderFunc func(item Item, width int, st Styles) []string

// Styles holds the list's look.
type Styles struct {
	Bar, SelectedBg   lipgloss.Style // left bar (accent) + subtle background on the selected item
	MatchOn, MatchOff string         // search highlight SGR on/off
	Track, Thumb      color.Color    // scrollbar glyph colors
	ScrollBg          color.Color    // scrollbar gutter background
	Gap               int            // blank lines between items (default 1)
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	on, off := ansi.SGR(lipgloss.Color("#000000"), lipgloss.Color("#ffaf00"))
	return Styles{
		Bar:        lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		SelectedBg: lipgloss.NewStyle().Background(lipgloss.Color("#262626")),
		MatchOn:    on,
		MatchOff:   off,
		Track:      lipgloss.Color("#3a3a3a"),
		Thumb:      lipgloss.Color("#808080"),
		ScrollBg:   lipgloss.Color("#1c1c1c"),
		Gap:        1,
	}
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles (stylesVersion 0).
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// WithKeyMap sets the key bindings.
func WithKeyMap(km KeyMap) Option { return func(m *Model) { m.keys = km } }

// Model is the list. It is a value; copies share only the render cache,
// which is a memo keyed by (ID, Version, width, stylesVersion), so View's
// output is a pure function of (items, size, selection, search, styles)
// even though View fills and evicts that cache. Use the Model most recently
// returned by Update; it is not safe for concurrent use.
type Model struct {
	render  RenderFunc
	styles  Styles
	sv      int // stylesVersion
	keys    KeyMap
	w, h    int
	items   []Item
	index   map[string]int // ID -> position in items
	offsets []int          // offsets[i] = first line of items[i]; offsets[n] = total + gap
	total   int            // lines in the flattened list
	sel     int            // selected position, -1 when empty
	yOffset int            // first visible line
	query   string
	noHL    bool // hide the selection bar and background (SetHighlight(false))
	c       *cache
}

// New builds an empty list that renders items with render.
func New(render RenderFunc, opts ...Option) Model {
	m := Model{
		render: render,
		styles: DefaultStyles(),
		keys:   DefaultKeyMap(),
		index:  map[string]int{},
		sel:    -1,
		c:      newCache(),
	}
	for _, o := range opts {
		o(&m)
	}
	m.relayout("", 0)
	return m
}

// SetSize sets the outer size: View returns exactly h lines of w cells.
// The view keeps its top item in place (or stays pinned to the bottom) and
// keeps the selection visible.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	id, within := m.anchor()
	m.w, m.h = w, h
	m.relayout(id, within)
	if m.sel != len(m.items)-1 {
		m.ensureVisible()
	}
}

// SetStyles replaces the Styles. version is the App's stylesVersion; a new
// version invalidates every cached render.
func (m *Model) SetStyles(st Styles, version int) {
	id, within := m.anchor()
	m.styles, m.sv = st, version
	m.relayout(id, within)
}

// SetItems replaces every item. The selection stays on the same ID, or
// moves to the last item when that ID is gone or the selection was on the
// last item (so a pinned view follows new blocks).
func (m *Model) SetItems(items []Item) {
	prev, hadSel := m.Selected()
	wasLast := hadSel && m.sel == len(m.items)-1
	id, within := m.anchor()

	m.items = slices.Clone(items)
	m.index = make(map[string]int, len(items))
	for i, it := range m.items {
		m.index[it.ID] = i
	}
	m.c.prune(m.index)

	m.sel = len(m.items) - 1
	if i, ok := m.index[prev.ID]; ok && hadSel && !wasLast {
		m.sel = i
	}
	m.relayout(id, within)
}

// Upsert replaces each item with the same ID, or appends it. If the
// selection was on the last item, it moves to the new last item.
func (m *Model) Upsert(items ...Item) {
	if len(items) == 0 {
		return
	}
	wasLast := m.sel == len(m.items)-1
	id, within := m.anchor()

	// Copy on write: copies of m keep their own items and index.
	next := slices.Clone(m.items)
	index, cloned := m.index, false
	for _, it := range items {
		if i, ok := index[it.ID]; ok {
			next[i] = it
			continue
		}
		if !cloned {
			index, cloned = maps.Clone(index), true
		}
		index[it.ID] = len(next)
		next = append(next, it)
	}
	m.items, m.index = next, index
	if wasLast {
		m.sel = len(m.items) - 1
	}
	m.relayout(id, within)
}

// Len is the number of items.
func (m Model) Len() int { return len(m.items) }

// SetHighlight shows (the default) or hides the selected item's bar and
// background. The selection itself, and scrolling, are unaffected.
func (m *Model) SetHighlight(on bool) { m.noHL = !on }

// Selected returns the selected item; false when the list is empty.
func (m Model) Selected() (Item, bool) {
	if m.sel < 0 || m.sel >= len(m.items) {
		return Item{}, false
	}
	return m.items[m.sel], true
}

// Select moves the selection to id, scrolling it into view. It reports
// whether id exists.
func (m *Model) Select(id string) bool {
	i, ok := m.index[id]
	if ok {
		m.moveTo(i)
	}
	return ok
}

// Top selects the first item.
func (m *Model) Top() { m.moveTo(0) }

// Bottom selects the last item and bottom-aligns the view.
func (m *Model) Bottom() {
	m.moveTo(len(m.items) - 1)
	m.yOffset = max(0, m.total-m.h)
}

// Update handles the KeyMap's keys; everything else is ignored.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(k, m.keys.Down):
		m.moveTo(m.sel + 1)
	case key.Matches(k, m.keys.Up):
		m.moveTo(m.sel - 1)
	case key.Matches(k, m.keys.Bottom):
		m.Bottom()
	case key.Matches(k, m.keys.HalfDown):
		m.halfPage(1)
	case key.Matches(k, m.keys.HalfUp):
		m.halfPage(-1)
	case key.Matches(k, m.keys.NextMatch):
		m.nextMatch(1)
	case key.Matches(k, m.keys.PrevMatch):
		m.nextMatch(-1)
	}
	return m, nil
}
