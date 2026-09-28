package statusbar

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Styles holds the bar's look. Each segment has its own style so the run
// state and indicators can stand out from the plain text around them.
type Styles struct {
	Mode    lipgloss.Style // "[MODE]"
	Text    lipgloss.Style // agent · model, ctx/cost
	Running lipgloss.Style // "⠋ running 12s"
	Idle    lipgloss.Style // "idle"
	Warn    lipgloss.Style // "⚠ N", "⏳", "untrusted"
	Hint    lipgloss.Style // the caller-supplied Hint
	Dim     lipgloss.Style // the right-aligned "ctrl+p" reminder
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	return Styles{
		Mode:    lipgloss.NewStyle().Bold(true),
		Text:    lipgloss.NewStyle(),
		Running: lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Idle:    lipgloss.NewStyle().Faint(true),
		Warn:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Hint:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Dim:     lipgloss.NewStyle().Faint(true),
	}
}

// segment is one piece of the bar's left side, in display order.
type segment struct {
	text  string
	style lipgloss.Style
}

// dropOrder lists segment indices (into the array segments() returns) in
// the order they are dropped when the bar is too narrow: cost/context
// first, then the indicators, then the hint, then the run state, and
// agent/model last. The mode badge (index 0) is never dropped.
func dropOrder() [5]int { return [5]int{3, 4, 5, 2, 1} }

// segments returns the bar's segments in display order: mode, agent ·
// model, run state, ctx/cost, indicators, hint.
func segments(s State, st Styles) [6]segment {
	runState := "idle"
	runStyle := st.Idle
	if s.Running {
		runState = string(spinnerFrame(s.Frame)) + " running " + formatElapsed(s.Elapsed)
		runStyle = st.Running
	}
	agentModel := ""
	if s.Agent != "" || s.Model != "" {
		agentModel = s.Agent + " · " + s.Model
	}
	return [6]segment{
		{"[" + s.Mode + "]", st.Mode},
		{agentModel, st.Text},
		{runState, runStyle},
		{"ctx " + formatCtx(s.CtxUsed, s.CtxLimit) + " · " + formatCost(s.CostUSD), st.Text},
		{indicatorsText(s), st.Warn},
		{s.Hint, st.Hint},
	}
}

// View renders the bar to exactly m.width cells, or "" when no width has
// been set. Segments are dropped from the middle (dropOrder), then the
// remaining left content is hard-truncated, before the right-aligned
// ctrl+p hint is ever touched.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	segs := segments(m.state, m.styles)
	active := [6]bool{}
	for i, sg := range segs {
		active[i] = sg.text != ""
	}

	right := m.styles.Dim.Render("ctrl+p")
	rightW := ansi.Width(right)

	left := joinActive(segs, active)
	for _, di := range dropOrder() {
		if ansi.Width(left)+1+rightW <= m.width {
			break
		}
		if active[di] {
			active[di] = false
			left = joinActive(segs, active)
		}
	}

	leftAvail := max(0, m.width-rightW-1)
	if ansi.Width(left) > leftAvail {
		left = ansi.Truncate(left, leftAvail, "…")
	}

	gap := max(0, m.width-ansi.Width(left)-rightW)
	line := left + strings.Repeat(" ", gap) + right
	return ansi.Truncate(line, m.width, "")
}

// joinActive renders every active segment and joins them with two spaces.
func joinActive(segs [6]segment, active [6]bool) string {
	parts := make([]string, 0, len(segs))
	for i, sg := range segs {
		if active[i] {
			parts = append(parts, sg.style.Render(sg.text))
		}
	}
	return strings.Join(parts, "  ")
}
