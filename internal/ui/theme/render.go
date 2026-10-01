package theme

import "charm.land/lipgloss/v2"

// RenderStyles holds the colors ui/render.go uses to color the transcript's
// one-line tool and subagent blocks, notices, reasoning, and the user
// block. It has no widget package of its own: render.go is ui's own block
// renderer, not a bubbles widget, so its Styles type lives here instead.
type RenderStyles struct {
	Tool   lipgloss.Style // a running tool/subagent's default line color
	OK     lipgloss.Style // a tool/subagent that finished successfully
	Error  lipgloss.Style // an errored tool/subagent, and error-level notices
	Denied lipgloss.Style // a denied tool/subagent
	Warn   lipgloss.Style // a tool/subagent awaiting permission
	Dim    lipgloss.Style // cancelled/pending states, notices, reasoning
	User   lipgloss.Style // a user block's filled panel: text color over panelFill
	// Added and Removed color an edit/write line's "+N" and "-N" counts.
	Added   lipgloss.Style
	Removed lipgloss.Style
}

// renderStyles maps p onto RenderStyles: OK is TextMuted (a finished tool
// line recedes to gray; only its outcome detail stands out), Added is
// successColor (Accent softened toward TextMuted, the same color as
// sidebar.Success in sidebarStyles), Error, Denied, and Removed
// share the palette's Error color (jig has no separate "denied" hue),
// Warn uses Warning like every other awaiting/pending indicator, and
// Tool/Dim fall back to Text/TextMuted like every other one-liner and
// hint text elsewhere in this file. User is Text over panelFill, the
// prompt's own unfocused fill.
func renderStyles(p Palette) RenderStyles {
	return RenderStyles{
		Tool:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		OK:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.Error)),
		Denied:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.Error)),
		Warn:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Dim:     lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		User:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)).Background(lipgloss.Color(panelFill(p))),
		Added:   lipgloss.NewStyle().Foreground(lipgloss.Color(successColor(p))),
		Removed: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Error)),
	}
}
