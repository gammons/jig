package theme

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/bubbles/confirm"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/mdrender"
	"github.com/gammons/jig/internal/bubbles/permcard"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/bubbles/statusbar"
)

// Set holds a Palette mapped into every widget's Styles type. Version
// identifies the shape of Set so callers that cache a Set (or derive
// further styles from it) can detect a stale copy after an upgrade.
type Set struct {
	Version   int
	Markdown  mdrender.Styles
	Code      coderender.Styles
	Blocklist blocklist.Styles
	Picker    picker.Styles
	Prompt    prompt.Styles
	Details   details.Styles
	Card      permcard.Styles
	Status    statusbar.Styles
	Sidebar   sidebar.Styles
	Confirm   confirm.Styles
	Render    RenderStyles
	// One field per later widget goes here as each widget's Task adds it.
}

// Build maps p into a Set at version, converting each hex (or ANSI-16
// index) color string into a color.Color via lipgloss.Color.
func Build(p Palette, version int) Set {
	return Set{
		Version:   version,
		Markdown:  markdownStyles(p),
		Code:      codeStyles(p),
		Blocklist: blocklistStyles(p),
		Picker:    pickerStyles(p),
		Prompt:    promptStyles(p),
		Details:   detailsStyles(p),
		Card:      cardStyles(p),
		Status:    statusStyles(p),
		Sidebar:   sidebarStyles(p),
		Confirm:   confirmStyles(p),
		Render:    renderStyles(p),
	}
}

// statusStyles maps p onto statusbar.Styles: the mode badge and the
// running indicator use Primary/Accent to stand out, idle and the dim
// ctrl+p hint fall back to TextMuted (faint, like every other hint), and
// the indicators/Hint segments share Warning, the palette's one
// stand-out-and-needs-attention color.
func statusStyles(p Palette) statusbar.Styles {
	return statusbar.Styles{
		Mode:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Text:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Running: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Idle:    lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Warn:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Hint:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Dim:     lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
}

// sidebarStyles maps p onto sidebar.Styles: section headers use Primary
// like every other pane title, the base text/muted tones use the
// palette's dedicated Sidebar* colors (falling back to the message pane's
// via Complete), Success reuses Accent (the palette has no dedicated
// green, the same gap codeStyles works around), and a Gauge's unfilled
// track uses Border, the same role it plays in blocklistStyles.
func sidebarStyles(p Palette) sidebar.Styles {
	return sidebar.Styles{
		Header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Normal:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.SidebarText)),
		Muted:      lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.SidebarTextMuted)),
		Accent:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Success:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Warning:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Error:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.Error)),
		GaugeEmpty: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
	}
}

// confirmStyles maps p onto confirm.Styles: the border uses the palette's
// border color, the title (embedded in the top border) and a choice's key
// use Primary/Accent to match the picker's title and mark/selected
// colors, and the "j/k scroll" hint embedded in the bottom border falls
// back to TextMuted like every other hint.
func confirmStyles(p Palette) confirm.Styles {
	return confirm.Styles{
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
		Title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Text:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Key:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Accent)),
		Label:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Scroll: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
}

// detailsStyles maps p onto details.Styles: the header uses Primary to
// match the other pane titles (the picker's title bar, the sidebar's
// section headers), and the separating rule uses the palette's border
// color.
func detailsStyles(p Palette) details.Styles {
	return details.Styles{
		Header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
	}
}

// cardStyles maps p onto permcard.Styles: line 1 uses Warning, the same
// stand-out color a pending/running state uses elsewhere, and line 2's
// legend falls back to TextMuted like every other hint/detail text.
func cardStyles(p Palette) permcard.Styles {
	return permcard.Styles{
		Text: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Hint: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
}

// promptStyles maps p onto prompt.Styles: the border uses the palette's
// border color, switching to Primary while focused (INSERT mode) so the
// active input stands out, the border title (used for "⏳ queued") uses Warning to
// stand out as a state indicator (the same role Warning plays for code in
// markdownStyles), and text/placeholder mirror the message pane's text
// and its muted, faint variant.
func promptStyles(p Palette) prompt.Styles {
	return prompt.Styles{
		Border:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
		FocusBorder: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)),
		Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Warning)),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Placeholder: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
}

// blocklistStyles maps p onto blocklist.Styles: the selection bar uses
// Accent over the focused-selection tint, search matches use the palette's
// search highlight pair, and the scrollbar draws its track in Border and
// its thumb in TextMuted over the pane Background.
func blocklistStyles(p Palette) blocklist.Styles {
	on, off := ansi.SGR(lipgloss.Color(p.SearchHighlightFg), lipgloss.Color(p.SearchHighlightBg))
	return blocklist.Styles{
		Bar:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		SelectedBg: lipgloss.NewStyle().Background(lipgloss.Color(p.SelectionBgFocused)),
		MatchOn:    on,
		MatchOff:   off,
		Track:      lipgloss.Color(p.Border),
		Thumb:      lipgloss.Color(p.TextMuted),
		ScrollBg:   lipgloss.Color(p.Background),
		Gap:        1,
	}
}

// pickerStyles maps p onto picker.Styles: the title bar and current-item
// marker use Primary, the cursor glyph and multi-select mark share
// Accent, and headers/detail text/disabled items all fall back to
// TextMuted. Matched runes use pickMatchColor: whichever of Warning,
// Primary, Accent, or Text contrasts most with Background, bold and
// underlined. Matches must not use SearchHighlightFg on its own: it is
// meant to sit on SearchHighlightBg and is usually equal to Background,
// so on the picker (which paints no background of its own — Background
// stands in for whatever the terminal shows through) it vanishes.
// Underline (on top of Bold) keeps a match distinguishable even on a
// palette where the highest-contrast candidate is Text itself.
func pickerStyles(p Palette) picker.Styles {
	return picker.Styles{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Header:   lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Text:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Detail:   lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Match:    lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color(pickMatchColor(p))),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Disabled: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Current:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)),
		Mark:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
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
