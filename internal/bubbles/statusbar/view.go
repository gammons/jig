package statusbar

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// Powerline glyphs (Nerd Font private-use code points).
const (
	sepRight   = "\ue0b0" // solid right-pointing arrow
	sepLeft    = "\ue0b2" // solid left-pointing arrow
	branchIcon = "\ue0a0" // version-control branch
)

// Styles holds the bar's look, in lualine's terms: a Mode* block (fg and
// bg) for sections a and z, B (fg and bg) for the branch and ctx/cost,
// and C (fg and bg) for the filler. Running, Idle, Warn, and Hint are
// foregrounds drawn over C's background.
type Styles struct {
	ModeNormal lipgloss.Style
	ModeInsert lipgloss.Style
	ModePicker lipgloss.Style
	B          lipgloss.Style // branch, ctx/cost
	C          lipgloss.Style // filler: agent · model
	Running    lipgloss.Style // "⠋ running 12s"
	Idle       lipgloss.Style // "idle"
	Warn       lipgloss.Style // "⚠ N", "⏳", "untrusted"
	Hint       lipgloss.Style // the caller-supplied Hint
}

// DefaultStyles returns fixed colors, independent of any theme.
func DefaultStyles() Styles {
	mode := func(bg string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#1c1c1c")).Background(lipgloss.Color(bg))
	}
	return Styles{
		ModeNormal: mode("#5fafff"),
		ModeInsert: mode("#87d787"),
		ModePicker: mode("#ffaf00"),
		B:          lipgloss.NewStyle().Foreground(lipgloss.Color("#e4e4e4")).Background(lipgloss.Color("#444444")),
		C:          lipgloss.NewStyle().Foreground(lipgloss.Color("#bcbcbc")).Background(lipgloss.Color("#262626")),
		Running:    lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
		Idle:       lipgloss.NewStyle().Faint(true),
		Warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		Hint:       lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
	}
}

// Droppable segments, as indices into State-derived text.
const (
	segAgent = iota
	segRun
	segBranch
	segHint
	segIndicators
	segCtx
	segCount
)

// dropOrder lists segments in the order they are dropped when the bar is
// too narrow: cost/context first, then the indicators, the hint, the
// branch, the run state, and agent/model last. The mode and ctrl+p blocks
// are never dropped.
func dropOrder() [segCount]int {
	return [segCount]int{segCtx, segIndicators, segHint, segBranch, segRun, segAgent}
}

// texts returns each droppable segment's plain text ("" when empty).
func texts(s State) [segCount]string {
	var t [segCount]string
	if s.Agent != "" || s.Model != "" {
		t[segAgent] = s.Agent + " · " + s.Model
		if s.Effort != "" {
			t[segAgent] += " · " + s.Effort
		}
	}
	t[segRun] = "idle"
	if s.Running {
		t[segRun] = string(spinnerFrame(s.Frame)) + " running " + formatElapsed(s.Elapsed)
	}
	if s.Branch != "" {
		t[segBranch] = branchIcon + " " + s.Branch
	}
	t[segHint] = s.Hint
	t[segIndicators] = indicatorsText(s)
	t[segCtx] = "ctx " + formatCtx(s.CtxUsed, s.CtxLimit) + " · " + formatCost(s.CostUSD)
	return t
}

// modeStyle picks the block style for mode; unknown modes use NORMAL's.
func (st Styles) modeStyle(mode string) lipgloss.Style {
	switch mode {
	case "INSERT":
		return st.ModeInsert
	case "PICKER":
		return st.ModePicker
	}
	return st.ModeNormal
}

// arrow renders glyph with fg as its foreground and bg as its background,
// the powerline join between two blocks.
func arrow(glyph string, fg, bg color.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Render(glyph)
}

// View renders the bar to exactly m.width cells, or "" when no width has
// been set. Segments are dropped (dropOrder) until the bar fits, then the
// left side is hard-truncated before the right side is ever touched.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	t := texts(m.state)
	var on [segCount]bool
	for i, s := range t {
		on[i] = s != ""
	}
	left, right := m.sides(t, on)
	for _, di := range dropOrder() {
		if ansi.Width(left)+ansi.Width(right) <= m.width {
			break
		}
		if on[di] {
			on[di] = false
			left, right = m.sides(t, on)
		}
	}

	rightW := ansi.Width(right)
	leftAvail := max(0, m.width-rightW)
	if ansi.Width(left) > leftAvail {
		left = ansi.Truncate(left, leftAvail, "…")
	}
	gap := max(0, m.width-ansi.Width(left)-rightW)
	line := left + m.styles.C.Render(strings.Repeat(" ", gap)) + right
	return ansi.Truncate(line, m.width, "")
}

// sides renders the bar's left (a, b, c) and right (x, y, z) halves with
// only the segments marked on. The filler between them is added by View.
func (m Model) sides(t [segCount]string, on [segCount]bool) (string, string) {
	st := m.styles
	mode := st.modeStyle(m.state.Mode)
	modeBg, bBg, cBg := mode.GetBackground(), st.B.GetBackground(), st.C.GetBackground()

	var l strings.Builder
	l.WriteString(mode.Render(" " + m.state.Mode + " "))
	if on[segBranch] {
		l.WriteString(arrow(sepRight, modeBg, bBg))
		l.WriteString(st.B.Render(" " + t[segBranch] + " "))
		l.WriteString(arrow(sepRight, bBg, cBg))
	} else {
		l.WriteString(arrow(sepRight, modeBg, cBg))
	}
	for _, p := range []struct {
		i  int
		st lipgloss.Style
	}{{segAgent, st.C}, {segRun, m.runStyle()}, {segHint, st.Hint}} {
		if on[p.i] {
			l.WriteString(st.C.Render(" "))
			l.WriteString(p.st.Inherit(st.C).Render(t[p.i]))
			l.WriteString(st.C.Render(" "))
		}
	}

	var r strings.Builder
	if on[segIndicators] {
		r.WriteString(st.Warn.Inherit(st.C).Render(t[segIndicators]))
		r.WriteString(st.C.Render(" "))
	}
	zFrom := cBg
	if on[segCtx] {
		r.WriteString(arrow(sepLeft, bBg, cBg))
		r.WriteString(st.B.Render(" " + t[segCtx] + " "))
		zFrom = bBg
	}
	r.WriteString(arrow(sepLeft, modeBg, zFrom))
	r.WriteString(mode.Render(" ctrl+p "))
	return l.String(), r.String()
}

// runStyle is the run-state segment's foreground.
func (m Model) runStyle() lipgloss.Style {
	if m.state.Running {
		return m.styles.Running
	}
	return m.styles.Idle
}
