package confirm

import "testing"

func TestConfirm_SetStyles(t *testing.T) {
	t.Parallel()
	choices := []Choice{{Key: "t", Label: "Trust"}, {Key: "n", Label: "Don't trust"}}
	m := New()
	m.SetSize(100, 30)
	m.Set("Trust project?", trustEffects(), choices)
	want := New(WithStyles(pinnedStyles()))
	want.SetSize(100, 30)
	want.Set("Trust project?", trustEffects(), choices)
	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}
