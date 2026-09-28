package details

import "testing"

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
