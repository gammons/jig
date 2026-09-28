package details

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fafff")),
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")),
	}
}

func lines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line"
	}
	return out
}

func TestDetails_ScrollClamp(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 8) // bodyHeight = 6
	m.SetContent(Content{Header: "file.go", Lines: lines(10)})

	m.ScrollBy(100)
	if got, want := m.scroll, 4; got != want { // maxScroll = 10-6
		t.Fatalf("scroll after ScrollBy(100) = %d, want %d", got, want)
	}

	m.ScrollBy(-100)
	if got := m.scroll; got != 0 {
		t.Fatalf("scroll after ScrollBy(-100) = %d, want 0", got)
	}

	m.ScrollBy(2)
	if got, want := m.scroll, 2; got != want {
		t.Fatalf("scroll after ScrollBy(2) = %d, want %d", got, want)
	}
}

func TestDetails_ScrollClampOnResize(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 8) // bodyHeight = 6
	m.SetContent(Content{Header: "file.go", Lines: lines(10)})
	m.ScrollBy(100) // scroll = 4

	m.SetSize(30, 12) // bodyHeight = 10 >= len(Lines): maxScroll = 0
	if got := m.scroll; got != 0 {
		t.Fatalf("scroll after growing the pane = %d, want 0 (clamped)", got)
	}
}

func TestDetails_SetContentResetsScroll(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 8)
	m.SetContent(Content{Header: "a.go", Lines: lines(10)})
	m.ScrollBy(3)

	m.SetContent(Content{Header: "b.go", Lines: lines(10)})
	if got := m.scroll; got != 0 {
		t.Fatalf("scroll after SetContent = %d, want 0", got)
	}
}

func TestDetails_BodyOrigin(t *testing.T) {
	t.Parallel()
	m := New()
	if x, y := m.BodyOrigin(); x != 0 || y != 0 {
		t.Fatalf("BodyOrigin before SetSize = (%d,%d), want (0,0)", x, y)
	}

	m.SetSize(30, 8)
	if x, y := m.BodyOrigin(); x != 0 || y != 2 {
		t.Fatalf("BodyOrigin = (%d,%d), want (0,2) (header row + border row)", x, y)
	}
}

func TestDetails_ViewExactSize(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(20, 5)
	m.SetContent(Content{Header: "a very long header that overflows", Lines: lines(2)})

	got := m.View()
	rows := splitLines(got)
	if len(rows) != 5 {
		t.Fatalf("View produced %d rows, want 5", len(rows))
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != 20 {
			t.Fatalf("row %d width = %d, want 20 (%q)", i, w, r)
		}
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func TestGolden_DetailsCode(t *testing.T) {
	t.Parallel()
	kw := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f87")).Bold(true)
	str := lipgloss.NewStyle().Foreground(lipgloss.Color("#87d787"))
	plain := lipgloss.NewStyle().Foreground(lipgloss.Color("#d0d0d0"))

	content := Content{
		Header: "internal/greet/greet.go",
		Lines: []string{
			kw.Render("package") + " " + plain.Render("greet"),
			"",
			kw.Render("func") + " " + plain.Render("Hello() string {"),
			"    " + kw.Render("return") + " " + str.Render(`"hi"`),
			plain.Render("}"),
		},
	}

	m := New(WithStyles(pinnedStyles()))
	m.SetSize(40, 10)
	m.SetContent(content)
	golden.Assert(t, "details_code", m.View())
}
