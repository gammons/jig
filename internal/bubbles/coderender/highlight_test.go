package coderender

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// testStyles is a pinned, terminal-independent Styles used by the tests,
// kept separate from DefaultStyles so a later change to DefaultStyles
// doesn't silently change what these tests assert. Plain is left as the
// lipgloss zero value (no attributes set), so Render is a no-op on it —
// TestHighlight_UnknownExtensionPlain relies on that to assert untouched
// output.
func testStyles() Styles {
	return Styles{
		Plain:    lipgloss.NewStyle(),
		Keyword:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff0000")),
		Type:     lipgloss.NewStyle().Foreground(lipgloss.Color("#00afff")),
		Name:     lipgloss.NewStyle(),
		Func:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		String:   lipgloss.NewStyle().Foreground(lipgloss.Color("#00af00")),
		Number:   lipgloss.NewStyle().Foreground(lipgloss.Color("#af00ff")),
		Comment:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#808080")),
		Operator: lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Added:    lipgloss.NewStyle().Background(lipgloss.Color("#003300")),
		Removed:  lipgloss.NewStyle().Background(lipgloss.Color("#330000")),
		Hunk:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00afff")),
		Gutter:   lipgloss.NewStyle().Foreground(lipgloss.Color("#585858")),
	}
}

func TestHighlight_GoKeywordsStyled(t *testing.T) {
	t.Parallel()
	st := testStyles()
	code := "func main() {\n\tprintln(\"hi\")\n}\n"

	got := Highlight("main.go", code, st)

	wantKeyword := st.Keyword.Render("func")
	if !strings.Contains(got[0], wantKeyword) {
		t.Fatalf("line 0 = %q, want it to contain %q (styled %q)", got[0], wantKeyword, "func")
	}
}

func TestHighlight_UnknownExtensionPlain(t *testing.T) {
	t.Parallel()
	code := "hello there\nplain text with no syntax\n"

	got := Highlight("notes.unknownext", code, testStyles())

	want := strings.Split(code, "\n")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Highlight(...) = %q, want %q (unstyled, since Plain has no attributes)", got, want)
	}
}

func TestHighlight_LineCount(t *testing.T) {
	t.Parallel()
	st := testStyles()

	cases := []string{
		"",
		"single line, no trailing newline",
		"a\nb\nc\n",
		"a\nb\nc",
		"\n\n\n",
		"func main() {\n\tprintln(\"hi\")\n}\n",
	}

	for _, code := range cases {
		want := len(strings.Split(code, "\n"))
		got := len(Highlight("main.go", code, st))
		if got != want {
			t.Errorf("Highlight(%q, ...): got %d lines, want %d (input has %d \\n-split lines)", code, got, want, want)
		}
	}
}
