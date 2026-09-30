package breadcrumb

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Muted:       lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Current:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5fff5f")),
		CurrentDim:  lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Hint:        lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Rule:        lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")),
		RuleFocused: lipgloss.NewStyle().Foreground(lipgloss.Color("#5fff5f")),
	}
}

func TestBreadcrumb_SetStyles(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSegments([]string{"main", "↳ explore: find the bug"})
	m.SetHint("esc back")
	m.SetFocused(true)
	m.SetWidth(80)

	want := New(WithStyles(pinnedStyles()))
	want.SetSegments([]string{"main", "↳ explore: find the bug"})
	want.SetHint("esc back")
	want.SetFocused(true)
	want.SetWidth(80)

	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}
