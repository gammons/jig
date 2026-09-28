package confirm

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Styles holds the dialog's look.
type Styles struct {
	Border lipgloss.Style // the box border
	Title  lipgloss.Style // the title embedded in the top border
	Text   lipgloss.Style // body lines
	Key    lipgloss.Style // a choice's key, e.g. "t"
	Label  lipgloss.Style // a choice's label, e.g. "Trust"
	Scroll lipgloss.Style // the "j/k scroll" hint in the bottom border
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Border: lipgloss.NewStyle(),
		Title:  lipgloss.NewStyle().Bold(true),
		Text:   lipgloss.NewStyle(),
		Key:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fafff")),
		Label:  lipgloss.NewStyle(),
		Scroll: lipgloss.NewStyle().Faint(true),
	}
}

const (
	maxBoxWidth = 70
	minBoxWidth = 24
	// boxOverhead is the box's non-content lines: top border, a blank
	// separator, the choices row, and the bottom border.
	boxOverhead = 4
)

// boxWidth clamps the dialog to a comfortable reading width without
// exceeding the terminal.
func boxWidth(termW int) int {
	w := min(maxBoxWidth, termW-6)
	if w < minBoxWidth {
		w = min(minBoxWidth, termW)
	}
	return max(1, w)
}

// visibleWindow returns how many of lineCount body lines fit given termH.
func visibleWindow(termH, lineCount int) int {
	avail := termH - boxOverhead
	if avail < 1 {
		avail = 1
	}
	if lineCount < avail {
		return lineCount
	}
	return avail
}

// View renders the box only (title, visible body lines, and the choices
// row); the caller centers it — with overlay.Center over the rest of the
// TUI, or alone for the pre-TUI trust dialog.
func (m Model) View() string {
	if m.termW <= 0 || m.termH <= 0 {
		return ""
	}
	w := boxWidth(m.termW)
	inner := max(0, w-2)
	visN := visibleWindow(m.termH, len(m.lines))
	yOff := clampOffset(m.yOffset, len(m.lines), visN)

	b := lipgloss.RoundedBorder()
	rows := make([]string, 0, visN+boxOverhead)
	rows = append(rows, borderRow(w, b.TopLeft, b.Top, b.TopRight, m.title, m.styles.Border, m.styles.Title))
	for i := yOff; i < yOff+visN; i++ {
		rows = append(rows, lineRow(inner, m.lines[i], m.styles.Text, m.styles.Border, b))
	}
	rows = append(rows, lineRow(inner, "", m.styles.Text, m.styles.Border, b))
	rows = append(rows, lineRow(inner, choicesText(m.choices, m.styles), m.styles.Text, m.styles.Border, b))
	bottomHint := ""
	if visN < len(m.lines) {
		bottomHint = "j/k scroll"
	}
	rows = append(rows, borderRow(w, b.BottomLeft, b.Bottom, b.BottomRight, bottomHint, m.styles.Border, m.styles.Scroll))
	return strings.Join(rows, "\n")
}

// choicesText joins choices as "<key> <label>", each styled and separated
// by three spaces, pre-rendered so lineRow can treat it as plain text for
// width purposes.
func choicesText(choices []Choice, st Styles) string {
	parts := make([]string, len(choices))
	for i, c := range choices {
		parts[i] = st.Key.Render(c.Key) + " " + st.Label.Render(c.Label)
	}
	return strings.Join(parts, "   ")
}

// lineRow renders one bordered content row: a left border, one space of
// margin, text truncated and styled to fit inner-1 cells, padding, and a
// right border.
func lineRow(inner int, text string, style, borderStyle lipgloss.Style, b lipgloss.Border) string {
	avail := max(0, inner-1)
	content := " " + ansi.Truncate(text, avail, "…")
	return borderStyle.Render(b.Left) + padLine(style.Inline(true).Render(content), inner) + borderStyle.Render(b.Right)
}

// borderRow renders one border line of width w, embedding title (if any)
// centered with one fill rune of lead padding.
func borderRow(w int, left, fill, right, title string, borderStyle, titleStyle lipgloss.Style) string {
	inner := max(0, w-2)
	var mid string
	switch title {
	case "":
		mid = borderStyle.Render(strings.Repeat(fill, inner))
	default:
		label := " " + title + " "
		lw := ansi.Width(label)
		if lw >= inner {
			mid = titleStyle.Render(ansi.Truncate(label, inner, ""))
		} else {
			lead, trail := 1, inner-lw-1
			mid = borderStyle.Render(strings.Repeat(fill, lead)) +
				titleStyle.Render(label) +
				borderStyle.Render(strings.Repeat(fill, trail))
		}
	}
	return borderStyle.Render(left) + mid + borderStyle.Render(right)
}

// padLine pads or cuts s to exactly width cells.
func padLine(s string, width int) string {
	w := ansi.Width(s)
	if w >= width {
		return ansi.Cut(s, 0, width)
	}
	return s + strings.Repeat(" ", width-w)
}
