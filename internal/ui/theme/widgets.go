package theme

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/mdrender"
)

// Set holds a Palette mapped into every widget's Styles type. Version
// identifies the shape of Set so callers that cache a Set (or derive
// further styles from it) can detect a stale copy after an upgrade.
type Set struct {
	Version  int
	Markdown mdrender.Styles
	// One field per later widget goes here as each widget's Task adds it.
}

// Build maps p into a Set at version, converting each hex (or ANSI-16
// index) color string into a color.Color via lipgloss.Color.
func Build(p Palette, version int) Set {
	return Set{
		Version:  version,
		Markdown: markdownStyles(p),
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
