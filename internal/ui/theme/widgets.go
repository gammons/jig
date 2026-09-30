package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/breadcrumb"
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
	Version    int
	Markdown   mdrender.Styles
	Code       coderender.Styles
	Blocklist  blocklist.Styles
	Breadcrumb breadcrumb.Styles
	Picker     picker.Styles
	Prompt     prompt.Styles
	Details    details.Styles
	Card       permcard.Styles
	Status     statusbar.Styles
	Sidebar    sidebar.Styles
	Confirm    confirm.Styles
	Render     RenderStyles
	Selection  SelectionStyles
	Screen     ScreenColors
	// One field per later widget goes here as each widget's Task adds it.
}

// ScreenColors holds the screen's default colors (the palette's
// Background and Text): the App sets them as the terminal's default
// background/foreground, so every cell no widget colors explicitly takes
// the theme's colors.
type ScreenColors struct {
	Background color.Color
	Foreground color.Color
}

// Build maps p into a Set at version, converting each hex (or ANSI-16
// index) color string into a color.Color via lipgloss.Color.
func Build(p Palette, version int) Set {
	return Set{
		Version:    version,
		Markdown:   markdownStyles(p),
		Code:       codeStyles(p),
		Blocklist:  blocklistStyles(p),
		Breadcrumb: breadcrumbStyles(p),
		Picker:     pickerStyles(p),
		Prompt:     promptStyles(p),
		Details:    detailsStyles(p),
		Card:       cardStyles(p),
		Status:     statusStyles(p),
		Sidebar:    sidebarStyles(p),
		Confirm:    confirmStyles(p),
		Render:     renderStyles(p),
		Selection:  selectionStyles(p),

		Screen: ScreenColors{
			Background: lipgloss.Color(p.Background),
			Foreground: lipgloss.Color(p.Text),
		},
	}
}

// statusStyles maps p onto statusbar.Styles, lualine-style: the mode
// blocks (sections a and z) are Background text on a per-mode color —
// Primary for NORMAL, Accent for INSERT, Warning for PICKER; the branch
// and ctx/cost blocks sit on Border (lighter than every surface, like
// lualine's section b) and the filler on SurfaceDark. The
// running indicator uses Accent, idle TextMuted, and the indicators/Hint
// share Warning, the palette's one needs-attention color.
func statusStyles(p Palette) statusbar.Styles {
	mode := func(bg string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(p.Background)).Background(lipgloss.Color(bg))
	}
	return statusbar.Styles{
		ModeNormal: mode(p.Primary),
		ModeInsert: mode(p.Accent),
		ModePicker: mode(p.Warning),
		B:          lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)).Background(lipgloss.Color(p.Border)),
		C:          lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)).Background(lipgloss.Color(p.SurfaceDark)),
		Running:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Idle:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
		Warn:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Hint:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
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

// breadcrumbStyles maps p onto breadcrumb.Styles: the current segment uses
// bold Primary when focused, falling back to plain Text when not; earlier
// segments and the hint share TextMuted like every other muted/hint text,
// and the rule row below the breadcrumb uses Border, switching to Primary
// when focused (the same role Primary plays for the prompt's border).
func breadcrumbStyles(p Palette) breadcrumb.Styles {
	return breadcrumb.Styles{
		Muted:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
		Current:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		CurrentDim:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Hint:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
		Rule:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
		RuleFocused: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)),
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

// promptStyles maps p onto prompt.Styles: the panel fill is panelFill
// (the same color as a sent user block, so what you type looks like what
// you sent), and the focus bar shown in INSERT mode is Accent, the
// transcript's own selection bar color (blocklistStyles); the top-edge
// label (used for "⏳ queued") uses Warning to stand out as a state
// indicator, and text/placeholder mirror the message pane's text and its
// muted, faint variant.
func promptStyles(p Palette) prompt.Styles {
	return prompt.Styles{
		Fill:        lipgloss.NewStyle().Background(lipgloss.Color(panelFill(p))),
		Bar:         lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Warning)),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Placeholder: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
}

// panelFill is the background of user-entered text: sent user blocks and
// the unfocused prompt. It is Surface, unless the theme's Surface is its
// Background (the panel would be invisible), then a light Text tint.
func panelFill(p Palette) string {
	if strings.EqualFold(p.Surface, p.Background) {
		return mixColors(p.Text, p.Background, defaultTintAlpha)
	}
	return p.Surface
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

// SelectionStyles holds the SGR on/off pair used to highlight a dragged
// text selection over already-rendered rows (selection.Highlight).
type SelectionStyles struct{ On, Off string }

// selectionStyles maps p onto SelectionStyles, the same way
// blocklistStyles builds the search highlight, using the palette's
// selected-text colors.
func selectionStyles(p Palette) SelectionStyles {
	on, off := ansi.SGR(lipgloss.Color(p.SelectionForeground), lipgloss.Color(p.SelectionBackground))
	return SelectionStyles{On: on, Off: off}
}

// pickerStyles maps p onto picker.Styles: the title bar, current-item
// marker, and the box border use Primary (the picker has focus while it
// is open, like the prompt's focused border), group headers are bold
// Primary like the sidebar's section headers (so they never read as a
// disabled item), the cursor glyph and multi-select mark share Accent,
// and detail text/disabled items fall back to TextMuted. The box is filled with Surface so it stands
// apart from the Background-colored panes it floats over. Matched runes
// use pickMatchColor: whichever of Warning, Primary, Accent, or Text
// contrasts most with Surface, bold and underlined. Underline (on top of
// Bold) keeps a match distinguishable even on a palette where the
// highest-contrast candidate is Text itself.
func pickerStyles(p Palette) picker.Styles {
	return picker.Styles{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Text:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Detail:   lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Match:    lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color(pickMatchColor(p))),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Disabled: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Current:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)),
		Mark:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Border:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)),
		// Background is Surface, the picker's panel color.
		Background: lipgloss.Color(p.Surface),
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
