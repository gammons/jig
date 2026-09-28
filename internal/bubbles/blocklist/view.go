package blocklist

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/scrollbar"
)

// View renders exactly h lines of w cells: a 1-cell selection prefix, the
// item lines (w-2 cells), and a scrollbar column (a bare track when nothing
// overflows, so it always borders the side slot). It renders only the
// items it shows (from the cache when warm), then evicts the lines of items
// entirely outside [yOffset-2h, yOffset+3h) at both cached widths; heights
// stay cached.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	iw := m.w - 2
	rows := make([]string, 0, m.h)
	if iw >= 1 {
		rows = m.visibleRows(iw, rows)
	}
	blank := strings.Repeat(" ", m.w-1)
	for len(rows) < m.h {
		rows = append(rows, blank)
	}
	st := m.styles
	if iw >= 1 && scrollbar.Visible(m.total, m.h) {
		rows = scrollbar.Overlay(rows, m.w, m.total, m.yOffset, m.h, orNone(st.ScrollBg), orNone(st.Track), orNone(st.Thumb))
	} else {
		// No overflow: the bare track still draws the column, so the
		// border against the side slot is always there.
		track := lipgloss.NewStyle().Background(orNone(st.ScrollBg)).Foreground(orNone(st.Track)).Render("│")
		for i := range rows {
			rows[i] += track
		}
	}

	// Evict the lines of items outside [yOffset-2h, yOffset+3h), of
	// removed items, and of stale renders. A render at the previous width
	// is kept under the same window, so toggling back re-renders nothing.
	lo, hi, gap := m.yOffset-2*m.h, m.yOffset+3*m.h, gapOf(m.styles)
	m.c.evict(func(id string, s *slot) bool {
		i, ok := m.index[id]
		if !ok || !s.valid(m.items[i], s.width, m.sv) {
			return false
		}
		return m.offsets[i] < hi && m.offsets[i+1]-gap > lo
	})
	return strings.Join(rows, "\n")
}

// visibleRows appends the prefixed lines of [yOffset, yOffset+h) to rows.
// Item heights come from m.offsets, so the frame's geometry never depends
// on what the shared cache holds.
func (m Model) visibleRows(iw int, rows []string) []string {
	p := newPainter(m.styles, m.query)
	gapLine := strings.Repeat(" ", iw+1)
	gap, y := gapOf(m.styles), m.yOffset
	for i := itemAt(m.offsets, y); i < len(m.items) && len(rows) < m.h; i++ {
		start := m.offsets[i]
		ht := m.offsets[i+1] - gap - start
		e := m.c.get(m.items[i], iw, m.sv, m.styles, m.render)
		match := m.query != "" && m.c.matches(m.items[i], iw, m.sv, m.styles, m.render, m.query)
		for k := max(0, y-start); k < ht && len(rows) < m.h; k++ {
			line := strings.Repeat(" ", iw)
			if k < len(e.lines) {
				line = e.lines[k]
			}
			rows = append(rows, p.line(line, i == m.sel && !m.flags.noHL, match))
		}
		for g := range gap {
			if i == len(m.items)-1 || len(rows) >= m.h {
				break
			}
			if start+ht+g >= y {
				rows = append(rows, gapLine)
			}
		}
	}
	return rows
}

// painter draws one item line with the selection prefix, background, and
// search highlight.
type painter struct {
	st     Styles
	query  string
	bar    string
	reBg   *strings.Replacer // re-applies SelectedBg's background after resets; nil without one
	prefix string
}

func newPainter(st Styles, query string) painter {
	p := painter{st: st, query: query, bar: st.Bar.Render("▌"), prefix: " "}
	bg := st.SelectedBg.GetBackground()
	if _, none := bg.(lipgloss.NoColor); !none {
		on := xansi.Style{}.BackgroundColor(bg).String()
		pairs := []string{xansi.ResetStyle, xansi.ResetStyle + on, "\x1b[0m", "\x1b[0m" + on}
		if st.MatchOff != "" && st.MatchOff != xansi.ResetStyle && st.MatchOff != "\x1b[0m" {
			pairs = append(pairs, st.MatchOff, st.MatchOff+on)
		}
		p.reBg = strings.NewReplacer(pairs...)
	}
	return p
}

func (p painter) line(s string, selected, match bool) string {
	if match {
		s = ansi.Highlight(s, p.query, p.st.MatchOn, p.st.MatchOff)
	}
	if !selected {
		return p.prefix + s
	}
	if p.reBg != nil {
		s = p.reBg.Replace(s)
	}
	return p.bar + p.st.SelectedBg.Render(s)
}

// orNone maps a nil color to lipgloss.NoColor.
func orNone(c color.Color) color.Color {
	if c == nil {
		return lipgloss.NoColor{}
	}
	return c
}
