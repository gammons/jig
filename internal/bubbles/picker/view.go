package picker

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Styles holds the picker's look.
type Styles struct {
	Title    lipgloss.Style // breadcrumb bar
	Header   lipgloss.Style // group header text
	Text     lipgloss.Style // normal item title
	Detail   lipgloss.Style // item detail text
	Match    lipgloss.Style // matched-rune highlight, merged over Text/Disabled
	Selected lipgloss.Style // cursor glyph
	Disabled lipgloss.Style // disabled item title
	Current  lipgloss.Style // "●" current-item marker
	Mark     lipgloss.Style // multi-select mark glyph
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Title:    lipgloss.NewStyle().Bold(true),
		Header:   lipgloss.NewStyle().Faint(true),
		Text:     lipgloss.NewStyle(),
		Detail:   lipgloss.NewStyle().Faint(true),
		Match:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaf00")),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Disabled: lipgloss.NewStyle().Faint(true),
		Current:  lipgloss.NewStyle().Foreground(lipgloss.Color("#5fff5f")),
		Mark:     lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
	}
}

// View renders the box only (title bar, query/input line, and the item
// list); the caller centers it with overlay.Center.
func (m Model) View() string {
	if m.boxW <= 0 || m.boxH <= 0 || len(m.stack) == 0 {
		return ""
	}
	top := m.stack[len(m.stack)-1]

	lines := make([]string, 0, m.boxH)
	lines = append(lines, titleLine(m.styles, m.boxW, m.stack))
	lines = append(lines, queryLine(m.boxW, top))
	if !top.level.Input {
		lines = append(lines, itemLines(m.styles, m.boxW, top, m.boxH-len(lines))...)
	}
	for len(lines) < m.boxH {
		lines = append(lines, blankLine(m.boxW))
	}
	return strings.Join(lines[:m.boxH], "\n")
}

func titleLine(st Styles, boxW int, stack []frame) string {
	titles := make([]string, len(stack))
	for i, f := range stack {
		titles[i] = f.level.Title
	}
	top := stack[len(stack)-1]
	text := strings.Join(titles, " › ")
	switch {
	case top.loading:
		text += "  loading…"
	case top.err != nil:
		text += "  error"
	}
	return padLine(st.Title.Render(ansi.Truncate(text, boxW, "…")), boxW)
}

func queryLine(boxW int, top frame) string {
	return padLine(ansi.Truncate(top.input.View(), boxW, ""), boxW)
}

// itemLines renders up to h rows around the cursor.
func itemLines(st Styles, boxW int, top frame, h int) []string {
	if h <= 0 {
		return nil
	}
	start := windowStart(len(top.rows), top.cursor, h)
	lines := make([]string, 0, h)
	for i := start; i < len(top.rows) && len(lines) < h; i++ {
		lines = append(lines, rowLine(st, boxW, top, i))
	}
	return lines
}

func windowStart(n, cursor, h int) int {
	if n <= h {
		return 0
	}
	start := cursor - h/2
	if start < 0 {
		start = 0
	}
	if start+h > n {
		start = n - h
	}
	return start
}

func rowLine(st Styles, boxW int, top frame, i int) string {
	r := top.rows[i]
	if r.kind == rowHeader {
		return padLine(st.Header.Render(ansi.Truncate(r.header, boxW, "")), boxW)
	}

	it := r.item
	prefix := "  "
	if i == top.cursor {
		prefix = st.Selected.Render("❯") + " "
	}
	mark := ""
	if top.level.Multi {
		mark = "☐ "
		if top.marks[it.ID] {
			mark = st.Mark.Render("☑") + " "
		}
	}
	cur := ""
	if it.Current {
		cur = st.Current.Render("●") + " "
	}

	base := st.Text
	if it.Disabled {
		base = st.Disabled
	}
	title := styledTitle(it.Title, r.matched, base, st.Match)

	detail := ""
	if it.Detail != "" {
		detail = "  " + st.Detail.Render(ansi.Truncate(it.Detail, max(0, boxW/3), ""))
	}

	line := ansi.Truncate(prefix+mark+cur+title+detail, boxW, "…")
	return padLine(line, boxW)
}

// styledTitle renders title, wrapping the runs at matched into base
// merged with match, and every other run into base alone.
func styledTitle(title string, matched []int, base, match lipgloss.Style) string {
	baseStyle := base.Inline(true)
	if len(matched) == 0 {
		return baseStyle.Render(title)
	}
	want := make(map[int]bool, len(matched))
	for _, i := range matched {
		want[i] = true
	}
	matchedStyle := withMatch(base, match).Inline(true)

	runes := []rune(title)
	var b strings.Builder
	var group []rune
	inMatch := want[0]
	flush := func() {
		if len(group) == 0 {
			return
		}
		if inMatch {
			b.WriteString(matchedStyle.Render(string(group)))
		} else {
			b.WriteString(baseStyle.Render(string(group)))
		}
		group = group[:0]
	}
	for i, r := range runes {
		if cur := want[i]; i > 0 && cur != inMatch {
			flush()
			inMatch = cur
		}
		group = append(group, r)
	}
	flush()
	return b.String()
}

// padLine pads or cuts s to exactly width cells.
func padLine(s string, width int) string {
	w := ansi.Width(s)
	if w >= width {
		return ansi.Cut(s, 0, width)
	}
	return s + strings.Repeat(" ", width-w)
}

func blankLine(w int) string {
	if w <= 0 {
		return ""
	}
	return strings.Repeat(" ", w)
}
