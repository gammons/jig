package confirm

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update handles a key equal to a Choice's Key (emitting ChosenMsg) and,
// when the body lines overflow the visible window, j/k scrolling.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if c, ok := keyMatches(k, m.choices); ok {
		return m, func() tea.Msg { return ChosenMsg{Key: c.Key} }
	}

	visN := visibleWindow(m.termH, len(m.lines))
	if visN >= len(m.lines) {
		return m, nil
	}
	switch {
	case key.Matches(k, m.keys.Up):
		m.yOffset = clampOffset(m.yOffset-1, len(m.lines), visN)
	case key.Matches(k, m.keys.Down):
		m.yOffset = clampOffset(m.yOffset+1, len(m.lines), visN)
	}
	return m, nil
}

// clampOffset keeps yOffset within [0, max(0, total-visN)].
func clampOffset(yOffset, total, visN int) int {
	max := total - visN
	if max < 0 {
		max = 0
	}
	if yOffset < 0 {
		return 0
	}
	if yOffset > max {
		return max
	}
	return yOffset
}
