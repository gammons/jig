package details

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

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
	if x, y, ok := m.BodyOrigin(); ok || x != 0 || y != 0 {
		t.Fatalf("BodyOrigin before SetSize = (%d,%d,%v), want (0,0,false)", x, y, ok)
	}

	m.SetSize(30, 8)
	if x, y, ok := m.BodyOrigin(); !ok || x != 0 || y != 2 {
		t.Fatalf("BodyOrigin = (%d,%d,%v), want (0,2,true) (header row + border row)", x, y, ok)
	}
}

// TestDetails_BodyOriginNoBodyRow covers panes too short to have a body
// row: h=1 (header only) and h=2 (header + border, no body), where a
// caller must not place a sixel at BodyOrigin's (x, y). h=3 is the
// smallest pane with one body row, where BodyOrigin must report ok.
func TestDetails_BodyOriginNoBodyRow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		h    int
		want bool
	}{
		{h: 1, want: false},
		{h: 2, want: false},
		{h: 3, want: true},
	}
	for _, tt := range tests {
		m := New()
		m.SetSize(20, tt.h)
		if _, _, ok := m.BodyOrigin(); ok != tt.want {
			t.Errorf("h=%d: BodyOrigin ok = %v, want %v", tt.h, ok, tt.want)
		}
	}
}

func TestDetails_BodyOriginZeroWidth(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(0, 8)
	if x, y, ok := m.BodyOrigin(); ok || x != 0 || y != 0 {
		t.Fatalf("BodyOrigin at w=0 = (%d,%d,%v), want (0,0,false)", x, y, ok)
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

func TestDetails_ViewZeroWidth(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(0, 4)
	m.SetContent(Content{Header: "h", Lines: lines(2)})

	got := m.View()
	rows := splitLines(got)
	if len(rows) != 4 {
		t.Fatalf("View at w=0 produced %d rows, want 4 (exactly w×h)", len(rows))
	}
	for i, r := range rows {
		if r != "" {
			t.Fatalf("row %d = %q, want \"\" (zero cells wide)", i, r)
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

func TestDetails_HitTest(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(30, 6) // bodyHeight = 4
	m.SetContent(Content{Header: "file.go", Lines: lines(10)})
	m.ScrollBy(3) // scroll = 3

	// Rejected rows: header and rule rows.
	if _, _, ok := m.HitTest(0, 0); ok {
		t.Errorf("HitTest(0,0) ok = true, want false (header row)")
	}
	if _, _, ok := m.HitTest(0, 1); ok {
		t.Errorf("HitTest(0,1) ok = true, want false (rule row)")
	}

	// Body cell.
	if line, col, ok := m.HitTest(4, 2); !ok || line != 3 || col != 4 {
		t.Errorf("HitTest(4,2) = (%d,%d,%v), want (3,4,true)", line, col, ok)
	}

	// Past the content: still within the pane's body rows (y < h), and
	// line 6 exists among the 10 content lines.
	if line, col, ok := m.HitTest(0, 5); !ok || line != 6 || col != 0 {
		t.Errorf("HitTest(0,5) = (%d,%d,%v), want (6,0,true)", line, col, ok)
	}

	// y >= h is outside the pane entirely.
	if _, _, ok := m.HitTest(0, 6); ok {
		t.Errorf("HitTest(0,6) ok = true, want false (y >= h)")
	}

	// With only 5 content lines (scroll still 3, set directly since
	// clamping through the exported API would mask the bounds check
	// under test), the same cell is past the end.
	m2 := m
	m2.lines = lines(5)
	if _, _, ok := m2.HitTest(0, 5); ok {
		t.Errorf("HitTest(0,5) with 5 lines ok = true, want false (past content)")
	}

	// x >= w is outside the pane.
	if _, _, ok := m.HitTest(30, 2); ok {
		t.Errorf("HitTest(30,2) ok = true, want false (x >= w)")
	}
}

func TestDetails_Lines(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(20, 5)
	content := []string{"a\tb\tc", "plain", ""}
	m.SetContent(Content{Header: "h", Lines: content})

	got := m.Lines()
	want := []string{"a    b    c", "plain", ""}
	if len(got) != len(want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWithoutHeader_BodyFillsPane(t *testing.T) {
	t.Parallel()
	m := New(WithoutHeader())
	m.SetSize(20, 3)
	m.SetContent(Content{Header: "file.go", Lines: lines(5)})

	got := m.View()
	rows := splitLines(got)
	if len(rows) != 3 {
		t.Fatalf("View produced %d rows, want 3 (no header/rule rows)", len(rows))
	}
	if !strings.HasPrefix(rows[0], "line") {
		t.Fatalf("row 0 = %q, want to start with first body line %q", rows[0], "line")
	}
}

func TestWithoutHeader_ScrollBy(t *testing.T) {
	t.Parallel()
	m := New(WithoutHeader())
	m.SetSize(20, 3)
	m.SetContent(Content{Header: "file.go", Lines: lines(10)})

	m.ScrollBy(100)
	if got, want := m.scroll, 7; got != want { // maxScroll = 10-3
		t.Fatalf("scroll after ScrollBy(100) = %d, want %d", got, want)
	}
}

func TestWithoutHeader_BodyOrigin(t *testing.T) {
	t.Parallel()
	m := New(WithoutHeader())
	m.SetSize(20, 3)
	if x, y, ok := m.BodyOrigin(); !ok || x != 0 || y != 0 {
		t.Fatalf("BodyOrigin = (%d,%d,%v), want (0,0,true)", x, y, ok)
	}
}

func TestHeader_ReturnsContentHeader(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetContent(Content{Header: "file.go", Lines: lines(2)})
	if got, want := m.Header(), "file.go"; got != want {
		t.Fatalf("Header() = %q, want %q", got, want)
	}
}

func TestGolden_DetailsWithoutHeader(t *testing.T) {
	t.Parallel()
	m := New(WithoutHeader(), WithStyles(pinnedStyles()))
	m.SetSize(30, 4)
	m.SetContent(Content{Header: "file.go", Lines: lines(2)})
	golden.Assert(t, "details_without_header", m.View())
}

// TestDetails_HitTestWithoutHeader checks that with WithoutHeader a hit
// on row 0 maps to the first content line (scroll+0), since there is no
// header/rule row to skip.
func TestDetails_HitTestWithoutHeader(t *testing.T) {
	t.Parallel()
	m := New(WithoutHeader())
	m.SetSize(30, 4)
	m.SetContent(Content{Header: "file.go", Lines: lines(10)})
	m.ScrollBy(2)

	if line, col, ok := m.HitTest(3, 0); !ok || line != 2 || col != 3 {
		t.Fatalf("HitTest(3,0) = (%d,%d,%v), want (2,3,true) (row 0 is content with no header)", line, col, ok)
	}
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

func TestDetails_TabsExpandBeforeFitting(t *testing.T) {
	t.Parallel()
	m := New(WithStyles(pinnedStyles()))
	m.SetSize(20, 3)
	m.SetContent(Content{Header: "h", Lines: []string{"a\tb\tc"}})
	body := strings.Split(m.View(), "\n")[2]
	if want := "a    b    c         "; body != want {
		t.Errorf("body row = %q, want %q (tabs as 4 spaces, exactly 20 cells)", body, want)
	}
}

// TestDetails_WrapsLongLines: a body line wider than the pane wraps onto
// further lines (never cut), keeping its style on each continuation;
// scrolling, Lines, and a width change all follow the wrapped lines.
func TestDetails_WrapsLongLines(t *testing.T) {
	t.Parallel()
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	m := New(WithoutHeader())
	m.SetSize(10, 5)
	m.SetContent(Content{Lines: []string{red.Render("aaaa bbbb cccc dddd"), "short"}})

	got := m.Lines()
	plain := make([]string, len(got))
	for i, l := range got {
		plain[i] = strings.TrimRight(xansi.Strip(l), " ")
	}
	if want := []string{"aaaa bbbb", "cccc dddd", "short"}; !slices.Equal(plain, want) {
		t.Fatalf("Lines() = %q, want %q", plain, want)
	}
	if !strings.Contains(got[1], "38;2;255;0;0") {
		t.Errorf("continuation line %q lost the line's color", got[1])
	}
	view := strings.Split(xansi.Strip(m.View()), "\n")
	if strings.TrimSpace(view[1]) != "cccc dddd" {
		t.Errorf("View() row 1 = %q, want the wrapped continuation", view[1])
	}
	if line, _, ok := m.HitTest(0, 2); !ok || line != 2 {
		t.Errorf("HitTest(0,2) = (%d,%v), want line 2 (the drawn line index)", line, ok)
	}

	m.SetSize(40, 5)
	if n := len(m.Lines()); n != 2 {
		t.Errorf("after widening, Lines() has %d lines, want 2 (unwrapped)", n)
	}
}
