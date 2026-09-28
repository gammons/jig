package sidebar

import "testing"

func TestSidebar_SetStyles(t *testing.T) {
	t.Parallel()
	secs := []Section{{Title: "Session", Rows: []Row{{Text: "t"}, {Icon: "✓", Text: "done", Tone: Success}}}}
	m := New()
	m.SetSize(30, 6)
	m.SetSections(secs)
	want := New(WithStyles(pinnedStyles()))
	want.SetSize(30, 6)
	want.SetSections(secs)
	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}
