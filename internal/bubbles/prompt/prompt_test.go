package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/jig/internal/golden"
)

// pinnedStyles are fixed styles for goldens and exact-output assertions.
func pinnedStyles() Styles {
	return Styles{
		Border:      lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaf00")),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "shift+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
	case "alt+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	case "@":
		return tea.KeyPressMsg{Code: '@', Text: "@"}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// mustMsg runs cmd (which must be non-nil) and asserts its message has
// type T.
func mustMsg[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected a cmd, got nil")
	}
	msg := cmd()
	v, ok := msg.(T)
	if !ok {
		t.Fatalf("cmd yielded %T, want %T", msg, v)
	}
	return v
}

func TestPrompt_SubmitAndBlank(t *testing.T) {
	t.Parallel()

	blank := New(nil)
	blank.Focus()
	blank = typeText(blank, "   ")
	if _, cmd := blank.Update(keyMsg("enter")); cmd != nil {
		t.Errorf("blank submit: got a cmd, want none")
	}

	m := New(nil)
	m.Focus()
	m = typeText(m, "hello")
	_, cmd := m.Update(keyMsg("enter"))
	msg := mustMsg[SubmitMsg](t, cmd)
	if msg.Text != "hello" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello")
	}
}

func TestPrompt_NewlineKeys(t *testing.T) {
	t.Parallel()

	for _, k := range []string{"shift+enter", "alt+enter"} {
		m := New(nil)
		m.Focus()
		m = typeText(m, "a")
		m, cmd := m.Update(keyMsg(k))
		if cmd != nil {
			t.Errorf("%s: got a cmd, want none", k)
		}
		m = typeText(m, "b")
		if got, want := m.Value(), "a\nb"; got != want {
			t.Errorf("%s: Value() = %q, want %q", k, got, want)
		}
	}
}

func TestPrompt_GrowsToEightLines(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetWidth(40)

	check := func(step string, want int) {
		t.Helper()
		if got := m.Height(); got != want {
			t.Errorf("%s: Height() = %d, want %d", step, got, want)
		}
	}
	check("1 line", 3) // 1 content line + 2 border

	for range 4 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}
	check("5 lines", 7)

	for range 7 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}
	check("12 lines requested", 10) // clamped to 8 content lines + 2 border
}

func TestPrompt_EditorRoundTrip(t *testing.T) {
	t.Parallel()

	edit := func(text string) tea.Cmd {
		if text != "old" {
			t.Errorf("edit called with %q, want %q", text, "old")
		}
		return func() tea.Msg { return EditedMsg{Text: "new"} }
	}
	m := New(edit)
	m.Focus()
	m = typeText(m, "old")
	_, cmd := m.Update(keyMsg("ctrl+e"))
	edited := mustMsg[EditedMsg](t, cmd)
	if edited.Text != "new" {
		t.Fatalf("EditedMsg.Text = %q, want %q", edited.Text, "new")
	}
	m, _ = m.Update(edited)
	if got := m.Value(); got != "new" {
		t.Errorf("Value() = %q, want %q", got, "new")
	}
}

func TestPrompt_AtEmitsMention(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	_, cmd := m.Update(keyMsg("@"))
	mustMsg[MentionMsg](t, cmd)
	if got := m.Value(); got != "" {
		t.Errorf("Value() = %q, want empty (nothing inserted)", got)
	}
}

func TestPrompt_GoldenPlaceholder(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(40)
	golden.Assert(t, "prompt_placeholder", m.View())
}

func TestPrompt_GoldenQueued(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(40)
	m.SetQueued(true)
	golden.Assert(t, "prompt_queued", m.View())
}

func TestPrompt_GoldenChip(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetWidth(40)
	m.Focus()
	m, _ = m.Update(tea.PasteMsg{Content: strings.Repeat("z", 600)})
	golden.Assert(t, "prompt_chip", m.View())
}
