package details

import (
	"strings"
	"testing"
)

func TestDetails_SetStyles(t *testing.T) {
	t.Parallel()
	c := Content{Header: "read · a.go", Lines: []string{"x"}}
	m := New()
	m.SetSize(20, 4)
	m.SetContent(c)
	want := New(WithStyles(pinnedStyles()))
	want.SetSize(20, 4)
	want.SetContent(c)
	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}

func TestDetails_ReplaceContentKeepsScroll(t *testing.T) {
	t.Parallel()
	numbered := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = string(rune('a' + i))
		}
		return out
	}
	m := New()
	m.SetSize(10, 5) // 3 body rows
	m.SetContent(Content{Header: "h", Lines: numbered(10)})
	m.ScrollBy(4)
	m.ReplaceContent(Content{Header: "h", Lines: numbered(12)})
	if got := strings.Split(m.View(), "\n")[2]; !strings.HasPrefix(got, "e") {
		t.Fatalf("first body row after ReplaceContent = %q, want the kept offset (e)", got)
	}
	m.ReplaceContent(Content{Header: "h", Lines: numbered(5)}) // max scroll 2
	if got := strings.Split(m.View(), "\n")[2]; !strings.HasPrefix(got, "c") {
		t.Fatalf("first body row after a shorter ReplaceContent = %q, want the clamped offset (c)", got)
	}
}
