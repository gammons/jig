package picker

import "testing"

func TestPicker_SetStyles(t *testing.T) {
	t.Parallel()
	items := map[string][]Item{"root": {{ID: "a", Title: "Alpha", Detail: "first"}, {ID: "b", Title: "Beta"}}}
	build := func(opts ...Option) Model {
		m := New(loadFunc(items), opts...)
		m.SetSize(100, 30)
		return open(t, m, Level{ID: "root", Title: "Actions"})
	}
	m := build()
	want := build(WithStyles(pinnedStyles()))
	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}
