package sidebar

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Styles holds the sidebar's look.
type Styles struct {
	Header     lipgloss.Style // section title
	Normal     lipgloss.Style
	Muted      lipgloss.Style
	Accent     lipgloss.Style
	Success    lipgloss.Style
	Warning    lipgloss.Style
	Error      lipgloss.Style
	GaugeEmpty lipgloss.Style // the unfilled part of a Gauge row's bar
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Header:     lipgloss.NewStyle().Bold(true),
		Normal:     lipgloss.NewStyle(),
		Muted:      lipgloss.NewStyle().Faint(true),
		Accent:     lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Success:    lipgloss.NewStyle().Foreground(lipgloss.Color("#5fff5f")),
		Warning:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Error:      lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")),
		GaugeEmpty: lipgloss.NewStyle().Faint(true),
	}
}

// toneStyle picks st's style for t.
func toneStyle(t Tone, st Styles) lipgloss.Style {
	switch t {
	case Muted:
		return st.Muted
	case Accent:
		return st.Accent
	case Success:
		return st.Success
	case Warning:
		return st.Warning
	case Error:
		return st.Error
	default:
		return st.Normal
	}
}

// padX is the number of blank columns on each side of every sidebar line.
const padX = 2

// View renders the sidebar to exactly m.width x m.height cells, or ""
// when no size has been set. Sections with no rows are skipped entirely.
// Content is laid out padX columns in from each side.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	inner := max(0, m.width-2*padX)
	var lines []string
	first := true
	for _, sec := range m.sections {
		if len(sec.Rows) == 0 {
			continue
		}
		if !first {
			lines = append(lines, blankLine(inner))
		}
		first = false
		lines = append(lines, padLine(m.styles.Header.Render(ansi.Truncate(sec.Title, inner, "…")), inner))
		for _, row := range sec.Rows {
			lines = append(lines, rowLine(row, m.styles, inner))
		}
	}
	margin := strings.Repeat(" ", min(padX, m.width/2))
	for i, l := range lines {
		lines[i] = margin + l + margin
	}
	return clip(lines, m.width, m.height)
}

// rowLine renders one indented row, padded to exactly width cells.
func rowLine(row Row, st Styles, width int) string {
	inner := max(0, width-2)
	var content string
	if row.Gauge != nil {
		content = renderGauge(*row.Gauge, inner, toneStyle(row.Tone, st), st.GaugeEmpty)
	} else {
		text := row.Text
		if row.Icon != "" {
			text = row.Icon + " " + text
		}
		content = toneStyle(row.Tone, st).Render(ansi.Truncate(text, inner, "…"))
	}
	return padLine("  "+content, width)
}

// renderGauge draws a Used-of-Limit bar filling width cells, ending in a
// percentage label, e.g. "██████░░░░░░░░ 50%".
func renderGauge(g Gauge, width int, fill, empty lipgloss.Style) string {
	pct := 0
	if g.Limit > 0 {
		pct = int(float64(g.Used) * 100 / float64(g.Limit))
	}
	pct = clampPct(pct)
	label := fmt.Sprintf(" %d%%", pct)

	barW := max(0, width-ansi.Width(label))
	filled := 0
	if g.Limit > 0 && barW > 0 {
		filled = int(float64(barW) * float64(g.Used) / float64(g.Limit))
	}
	if filled > barW {
		filled = barW
	}
	if filled < 0 {
		filled = 0
	}
	bar := fill.Render(strings.Repeat("█", filled)) + empty.Render(strings.Repeat("░", barW-filled))
	return padLine(bar+label, width)
}

func clampPct(pct int) int {
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// clip pads lines with blank rows or cuts it to exactly h lines of w
// cells. Every line is padded or cut to w, so an odd width's leftover
// column (or a width too small for both margins) still yields w cells.
func clip(lines []string, w, h int) string {
	for i, l := range lines {
		lines[i] = padLine(l, w)
	}
	for len(lines) < h {
		lines = append(lines, blankLine(w))
	}
	return strings.Join(lines[:h], "\n")
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
