package prompt

import "testing"

func TestPrompt_SetStyles(t *testing.T) {
	t.Parallel()
	build := func(opts ...Option) Model {
		m := New(nil, opts...)
		m.SetWidth(40)
		m.SetAgent("build")
		m.SetQueued(true)
		m.Focus()
		return typeText(m, "hello")
	}
	m := build()
	want := build(WithStyles(pinnedStyles()))
	m.SetStyles(pinnedStyles())
	if m.View() != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", m.View(), want.View())
	}
}
