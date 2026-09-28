// Package details is jig's details pane: it shows the pre-rendered lines
// of the selected block (text, syntax-highlighted code, or image cells)
// under a header, with a bordered separator and a clamped scroll. It never
// takes focus; the App drives ScrollBy directly from NORMAL-mode keys.
package details

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Content is one selection's details: a header line and the pre-rendered
// body lines (text, code, or image cells). Lines may carry SGR escapes or
// kitty placeholder cells; View never re-wraps or cuts inside them.
type Content struct {
	Header string
	Lines  []string
}

// Styles holds the pane's look.
type Styles struct {
	Header lipgloss.Style // header line text
	Border lipgloss.Style // the rule between the header and the body
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().Bold(true),
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")),
	}
}

// Option configures a Model built by New.
type Option func(*Model)

// WithStyles sets the initial Styles.
func WithStyles(st Styles) Option { return func(m *Model) { m.styles = st } }

// Model is the details pane. Use the Model most recently returned by any
// pointer method; it is not safe for concurrent use.
type Model struct {
	styles  Styles
	w, h    int
	content Content
	scroll  int
}

// New builds an empty details pane.
func New(opts ...Option) Model {
	m := Model{styles: DefaultStyles()}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// SetSize sets the outer size: View returns exactly h lines of w cells.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.scroll = clamp(m.scroll, 0, m.maxScroll())
}

// SetStyles replaces the Styles.
func (m *Model) SetStyles(st Styles) { m.styles = st }

// SetContent replaces the shown content and resets the scroll to the top.
func (m *Model) SetContent(c Content) {
	m.content = c
	m.scroll = 0
}

// ReplaceContent replaces the shown content keeping the scroll offset,
// clamped to the new content (a live refresh of the same block).
func (m *Model) ReplaceContent(c Content) {
	m.content = c
	m.scroll = clamp(m.scroll, 0, m.maxScroll())
}

// ScrollBy moves the scroll by n lines (negative scrolls up), clamped to
// [0, len(Lines)-bodyHeight].
func (m *Model) ScrollBy(n int) {
	m.scroll = clamp(m.scroll+n, 0, m.maxScroll())
}

// bodyHeight is the number of body rows: the total height minus the
// header row and the border row.
func (m Model) bodyHeight() int {
	return max(0, m.h-2)
}

func (m Model) maxScroll() int {
	return max(0, len(m.content.Lines)-m.bodyHeight())
}

// BodyOrigin is the cell offset of the first body line inside the pane
// (the App uses it to place a sixel image over the body). ok is false
// when the pane has no body row to place anything in (too short, or
// zero-width), so a caller must not place a sixel at the returned (x, y).
func (m Model) BodyOrigin() (x, y int, ok bool) {
	if m.w <= 0 || m.bodyHeight() <= 0 {
		return 0, 0, false
	}
	return 0, 2, true
}

// View renders the header line, a border rule, and the body: exactly w×h
// cells. Body lines are cut or padded to width with ansi.Cut, which is
// escape- and wide-rune-aware, so a line carrying SGR or a kitty
// placeholder cell is never cut mid-escape. With w<=0 (and h>0) it still
// returns exactly h rows, each zero cells wide.
func (m Model) View() string {
	if m.h <= 0 {
		return ""
	}
	if m.w <= 0 {
		return strings.Repeat("\n", m.h-1)
	}
	lines := make([]string, 0, m.h)
	lines = append(lines, m.headerLine())
	lines = append(lines, m.borderLine())

	bh := m.bodyHeight()
	expanded := m.Lines()
	for i := 0; i < bh; i++ {
		idx := m.scroll + i
		line := ""
		if idx < len(expanded) {
			line = expanded[idx]
		}
		lines = append(lines, padLine(line, m.w))
	}
	return strings.Join(lines[:m.h], "\n")
}

// Lines returns the content lines with tabs expanded to 4 spaces, exactly
// as View draws them (before cutting or padding to width).
func (m Model) Lines() []string {
	out := make([]string, len(m.content.Lines))
	for i, l := range m.content.Lines {
		// A tab has no cell width of its own: expand it before
		// fitting, or the terminal's tab stops break the layout.
		out[i] = strings.ReplaceAll(l, "\t", "    ")
	}
	return out
}

// HitTest maps a cell (x, y) inside the pane to a content line and column.
// ok is false for the header row (y=0), the rule row (y=1), any row at or
// beyond the pane's height, any column at or beyond the pane's width, and
// a body row past the end of the content.
func (m Model) HitTest(x, y int) (line, col int, ok bool) {
	if x < 0 || x >= m.w || y < 2 || y >= m.h {
		return 0, 0, false
	}
	idx := m.scroll + y - 2
	if idx >= len(m.content.Lines) {
		return 0, 0, false
	}
	return idx, x, true
}

func (m Model) headerLine() string {
	text := ansi.Truncate(m.content.Header, m.w, "…")
	return padLine(m.styles.Header.Render(text), m.w)
}

func (m Model) borderLine() string {
	if m.w <= 0 {
		return ""
	}
	return m.styles.Border.Render(strings.Repeat("─", m.w))
}

// padLine pads or cuts s to exactly width cells.
func padLine(s string, width int) string {
	w := ansi.Width(s)
	if w >= width {
		return ansi.Cut(s, 0, width)
	}
	return s + strings.Repeat(" ", width-w)
}

// clamp limits v to [lo, hi]; lo wins when hi < lo.
func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
