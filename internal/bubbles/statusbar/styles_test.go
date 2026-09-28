package statusbar

import "testing"

func TestStatus_SetStyles(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetWidth(120)
	m.Set(runningState())
	want := New(WithStyles(pinnedStyles()))
	want.SetWidth(120)
	want.Set(runningState())
	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}
