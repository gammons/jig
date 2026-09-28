package theme

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/bubbles/mdrender"
)

// Set holds a Palette mapped into every widget's Styles type. Version
// identifies the shape of Set so callers that cache a Set (or derive
// further styles from it) can detect a stale copy after an upgrade.
type Set struct {
	Version  int
	Markdown mdrender.Styles
	Code     coderender.Styles
	// One field per later widget goes here as each widget's Task adds it.
}

// Build maps p into a Set at version, converting each hex (or ANSI-16
// index) color string into a color.Color via lipgloss.Color.
func Build(p Palette, version int) Set {
	return Set{
		Version:  version,
		Markdown: markdownStyles(p),
		Code:     codeStyles(p),
	}
}

// markdownStyles maps p onto mdrender.Styles: headings and links use the
// theme's primary/accent colors, inline and fenced code use Warning (to
// stand out from prose) over Surface, and quotes/rules use the muted and
// border colors.
func markdownStyles(p Palette) mdrender.Styles {
	return mdrender.Styles{
		Text:    lipgloss.Color(p.Text),
		Muted:   lipgloss.Color(p.TextMuted),
		Heading: lipgloss.Color(p.Primary),
		Link:    lipgloss.Color(p.Accent),
		Code:    lipgloss.Color(p.Warning),
		CodeBg:  lipgloss.Color(p.Surface),
		Quote:   lipgloss.Color(p.TextMuted),
		Rule:    lipgloss.Color(p.Border),
	}
}

// codeStyles maps p onto coderender.Styles. Keyword and Hunk headers use
// Primary/Accent to match the rest of the UI's accent colors; Type and
// Func also draw from Accent (bolded for Func) since the palette has no
// dedicated third accent. String and Number share Warning, the same
// stand-out color markdownStyles uses for code. Added and Removed use the
// palette's derived focused-selection tint and its Error color as
// backgrounds — the palette has no dedicated green/red pair, so Error is
// the only color guaranteed to read as "bad" across every theme.
func codeStyles(p Palette) coderender.Styles {
	return coderender.Styles{
		Plain:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Keyword:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Type:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Name:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Func:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)),
		String:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Number:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Comment:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color(p.TextMuted)),
		Operator: lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
		Added:    lipgloss.NewStyle().Background(lipgloss.Color(p.SelectionBgFocused)),
		Removed:  lipgloss.NewStyle().Background(lipgloss.Color(p.Error)),
		Hunk:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)),
		Gutter:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
	}
}
