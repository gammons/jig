package breadcrumb

import (
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// View renders exactly one row, w cells wide, following the truncation
// order: drop the hint, collapse middle segments from the left into a
// single "…", truncate the last segment with "…", then hard-cut the whole
// row. "" is returned when w <= 0.
func (m Model) View() string {
	if m.w <= 0 {
		return ""
	}
	if len(m.segments) == 0 {
		return strings.Repeat(" ", m.w)
	}

	row, hintShown := m.buildRow()
	if ansi.Width(row) > m.w {
		row = ansi.Truncate(row, m.w, "")
		hintShown = false
	}

	if hintShown {
		hint := m.styles.Hint.Render(m.hint)
		gap := m.w - ansi.Width(row) - ansi.Width(hint)
		return row + strings.Repeat(" ", gap) + hint
	}

	gap := m.w - ansi.Width(row)
	if gap > 0 {
		return row + strings.Repeat(" ", gap)
	}
	return row
}

// buildRow returns the styled, unpadded row content and whether the hint
// is shown alongside it.
func (m Model) buildRow() (string, bool) {
	full := joinPlain(m.segments)
	if m.hint != "" && ansi.Width(full)+2+ansi.Width(m.hint) <= m.w {
		return m.renderShown(m.segments), true
	}
	if ansi.Width(full) <= m.w {
		return m.renderShown(m.segments), false
	}

	first := m.segments[0]
	last := m.segments[len(m.segments)-1]
	middle := []string{}
	if len(m.segments) > 2 {
		middle = append([]string(nil), m.segments[1:len(m.segments)-1]...)
	}
	collapsedAny := false

	buildShown := func() []string {
		if len(m.segments) <= 1 {
			return m.segments
		}
		shown := []string{first}
		if collapsedAny {
			shown = append(shown, "…")
		}
		shown = append(shown, middle...)
		shown = append(shown, last)
		return shown
	}

	for {
		shown := buildShown()
		if ansi.Width(joinPlain(shown)) <= m.w {
			return m.renderShown(shown), false
		}
		if len(middle) == 0 {
			return m.truncateOrHardCut(shown), false
		}
		middle = middle[1:]
		collapsedAny = true
	}
}

// truncateOrHardCut applies step 3 (truncate the last segment with "…")
// when the prefix before it still fits, else step 4 (hard-cut the whole
// row, no tail).
func (m Model) truncateOrHardCut(shown []string) string {
	prefixSegs := shown[:len(shown)-1]
	last := shown[len(shown)-1]

	prefixPlain := ""
	if len(prefixSegs) > 0 {
		prefixPlain = joinPlain(prefixSegs) + " › "
	}
	prefixWidth := ansi.Width(prefixPlain)

	if prefixWidth < m.w {
		avail := m.w - prefixWidth
		lastStyle := m.styles.CurrentDim
		if m.focused {
			lastStyle = m.styles.Current
		}
		truncatedLast := ansi.Truncate(lastStyle.Render(last), avail, "…")
		return m.stylePrefix(prefixSegs) + truncatedLast
	}

	full := m.renderShown(shown)
	return ansi.Truncate(full, m.w, "")
}

// renderShown styles shown's segments: every segment but the last in
// Muted, the last in Current (focused) or CurrentDim, joined by a Muted
// " › " separator.
func (m Model) renderShown(shown []string) string {
	if len(shown) == 0 {
		return ""
	}
	lastIdx := len(shown) - 1
	parts := make([]string, len(shown))
	for i, s := range shown {
		if i == lastIdx {
			st := m.styles.CurrentDim
			if m.focused {
				st = m.styles.Current
			}
			parts[i] = st.Render(s)
		} else {
			parts[i] = m.styles.Muted.Render(s)
		}
	}
	return strings.Join(parts, m.styles.Muted.Render(" › "))
}

// stylePrefix renders segs (every one Muted) followed by a trailing Muted
// " › " separator, ready to be followed by the (separately styled) last
// segment.
func (m Model) stylePrefix(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = m.styles.Muted.Render(s)
	}
	return strings.Join(parts, m.styles.Muted.Render(" › ")) + m.styles.Muted.Render(" › ")
}

// joinPlain joins segs with " › ", unstyled: used only to measure width.
func joinPlain(segs []string) string {
	return strings.Join(segs, " › ")
}

// repeatRule returns n "─" cells.
func repeatRule(n int) string {
	return strings.Repeat("─", n)
}
