package permcard

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// hintLine is the card's second line, spec §7.4, verbatim.
const hintLine = "  a allow · A always (this exact command) · d deny · D deny with message"

// disarmedLine stands in for hintLine while the card's keys are disarmed.
const disarmedLine = "  …"

// Styles holds the card's look.
type Styles struct {
	Text lipgloss.Style // line 1: "⚠ ... wants to run: ..."
	Hint lipgloss.Style // line 2: the a/A/d/D legend
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Text: lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Hint: lipgloss.NewStyle().Faint(true),
	}
}

// View renders the card's lines, or "" when there is no request.
func (m Model) View() string {
	if m.req == nil {
		return ""
	}
	l1 := m.styles.Text.Render(cardLine1(*m.req, m.width))
	if m.typing {
		return l1 + "\n" + m.input.View()
	}
	if m.disarmed {
		return l1 + "\n" + m.styles.Hint.Render(disarmedLine)
	}
	return l1 + "\n" + m.styles.Hint.Render(hintLine)
}

// cardLine1 builds the card's first line (spec §7.4): the root-agent form
// has two spaces before the subject, the subagent form one. The subject is
// truncated to fit width; when even the prefix doesn't fit, the prefix
// itself is truncated instead.
func cardLine1(req Request, width int) string {
	var prefix string
	if req.Subagent != "" {
		prefix = "⚠ " + req.Subagent + " (subagent) wants to run " + req.Tool + ": "
	} else {
		prefix = "⚠ " + req.Tool + " wants to run:  "
	}
	if width <= 0 {
		return prefix + req.Subject
	}
	avail := width - ansi.Width(prefix)
	if avail < 0 {
		return ansi.Truncate(prefix, width, "…")
	}
	return prefix + ansi.Truncate(req.Subject, avail, "…")
}
