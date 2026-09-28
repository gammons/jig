package theme

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/mdrender"
	"github.com/gammons/jig/internal/bubbles/permcard"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
)

func TestBuild_MarkdownFromPalette(t *testing.T) {
	t.Parallel()

	p := Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
	}

	set := Build(p, 3)

	if set.Version != 3 {
		t.Errorf("Version = %d, want 3", set.Version)
	}

	want := mdrender.Styles{
		Text:    lipgloss.Color(p.Text),
		Muted:   lipgloss.Color(p.TextMuted),
		Heading: lipgloss.Color(p.Primary),
		Link:    lipgloss.Color(p.Accent),
		Code:    lipgloss.Color(p.Warning),
		CodeBg:  lipgloss.Color(p.Surface),
		Quote:   lipgloss.Color(p.TextMuted),
		Rule:    lipgloss.Color(p.Border),
	}
	if !reflect.DeepEqual(set.Markdown, want) {
		t.Errorf("Markdown = %+v, want %+v", set.Markdown, want)
	}
}

func TestBuild_CodeFromPalette(t *testing.T) {
	t.Parallel()

	p := Complete(Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
	})

	set := Build(p, 3)

	want := coderender.Styles{
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
	if !reflect.DeepEqual(set.Code, want) {
		t.Errorf("Code = %+v, want %+v", set.Code, want)
	}
}

func TestBuild_BlocklistFromPalette(t *testing.T) {
	t.Parallel()

	p := Complete(Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
		SelectionColors: SelectionColors{SearchHighlightBg: "#bbbbbb", SearchHighlightFg: "#cccccc"},
	})

	set := Build(p, 3)

	want := blocklist.Styles{
		Bar:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		SelectedBg: lipgloss.NewStyle().Background(lipgloss.Color(p.SelectionBgFocused)),
		MatchOn:    "\x1b[38;2;204;204;204;48;2;187;187;187m",
		MatchOff:   "\x1b[39;49m",
		Track:      lipgloss.Color(p.Border),
		Thumb:      lipgloss.Color(p.TextMuted),
		ScrollBg:   lipgloss.Color(p.Background),
		Gap:        1,
	}
	if !reflect.DeepEqual(set.Blocklist, want) {
		t.Errorf("Blocklist = %+v, want %+v", set.Blocklist, want)
	}
}

func TestBuild_PickerFromPalette(t *testing.T) {
	t.Parallel()

	p := Complete(Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
		SelectionColors: SelectionColors{SearchHighlightBg: "#bbbbbb", SearchHighlightFg: "#cccccc"},
	})

	set := Build(p, 3)

	want := picker.Styles{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Header:   lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Text:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Detail:   lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Match:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.SearchHighlightFg)),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
		Disabled: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
		Current:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)),
		Mark:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)),
	}
	if !reflect.DeepEqual(set.Picker, want) {
		t.Errorf("Picker = %+v, want %+v", set.Picker, want)
	}
}

func TestBuild_PromptFromPalette(t *testing.T) {
	t.Parallel()

	p := Complete(Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
	})

	set := Build(p, 3)

	want := prompt.Styles{
		Border:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
		Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Warning)),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Text)),
		Placeholder: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
	if !reflect.DeepEqual(set.Prompt, want) {
		t.Errorf("Prompt = %+v, want %+v", set.Prompt, want)
	}
}

func TestBuild_DetailsFromPalette(t *testing.T) {
	t.Parallel()

	p := Complete(Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
	})

	set := Build(p, 3)

	want := details.Styles{
		Header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.Primary)),
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Border)),
	}
	if !reflect.DeepEqual(set.Details, want) {
		t.Errorf("Details = %+v, want %+v", set.Details, want)
	}
}

func TestBuild_CardFromPalette(t *testing.T) {
	t.Parallel()

	p := Complete(Palette{
		Name: "test",
		BaseColors: BaseColors{
			Primary: "#111111", Accent: "#222222", Warning: "#333333", Error: "#444444",
			Background: "#555555", Surface: "#666666", SurfaceDark: "#777777",
			Text: "#888888", TextMuted: "#999999", Border: "#aaaaaa",
		},
	})

	set := Build(p, 3)

	want := permcard.Styles{
		Text: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)),
		Hint: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(p.TextMuted)),
	}
	if !reflect.DeepEqual(set.Card, want) {
		t.Errorf("Card = %+v, want %+v", set.Card, want)
	}
}
