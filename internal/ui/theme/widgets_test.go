package theme

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/bubbles/mdrender"
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
