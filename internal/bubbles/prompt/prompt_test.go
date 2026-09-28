package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

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
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
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
	check("12 lines requested", 10) // the visible viewport clamps to 8 content lines + 2 border
}

// TestPrompt_NeverDropsPastHeightCap covers the review's critical #1: past
// the visible cap, content must keep growing (and stay fully present in
// Value/View, scrolled into view), never be dropped or merged.
func TestPrompt_NeverDropsPastHeightCap(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetWidth(40)

	for range 14 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}
	m = typeText(m, "last")

	lines := strings.Split(m.Value(), "\n")
	if len(lines) != 15 {
		t.Fatalf("Value() has %d lines, want 15 (no lines dropped or merged): %q", len(lines), m.Value())
	}
	for i, l := range lines {
		if i < 14 && l != "" {
			t.Errorf("line %d = %q, want empty (only the last line was typed into)", i, l)
		}
	}
	if lines[14] != "last" {
		t.Errorf("last line = %q, want %q", lines[14], "last")
	}

	if got := m.ta.Height(); got != maxContentLines {
		t.Errorf("ta.Height() = %d, want the visible cap %d", got, maxContentLines)
	}
	if got, want := m.Height(), maxContentLines+2; got != want {
		t.Errorf("Height() = %d, want %d (%d content lines + border)", got, want, maxContentLines)
	}
	if !strings.Contains(m.View(), "last") {
		t.Errorf("cursor line not scrolled into view:\n%s", m.View())
	}
}

// TestPrompt_PasteAtCapNeverDropped covers the review's critical #1 for a
// single paste (not incremental typing): content already at the visible
// cap must not cause a subsequent paste to be silently discarded.
func TestPrompt_PasteAtCapNeverDropped(t *testing.T) {
	t.Parallel()

	m := New(nil)
	m.Focus()
	m.SetWidth(80)
	for range 10 {
		m, _ = m.Update(keyMsg("shift+enter"))
	}

	paste := strings.Repeat("q", 70)
	m, _ = m.Update(tea.PasteMsg{Content: paste})
	if got := m.Value(); !strings.Contains(got, paste) {
		t.Errorf("paste at the height cap was dropped: Value() = %q", got)
	}
}

// TestPrompt_BlurredIgnoresInput covers the review's item 4: Paste and
// KeyPressMsg must be no-ops while the prompt is blurred.
func TestPrompt_BlurredIgnoresInput(t *testing.T) {
	t.Parallel()

	m := New(nil) // starts blurred; Focus is never called
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd != nil {
		t.Errorf("KeyPressMsg while blurred: got a cmd, want none")
	}
	if got := m.Value(); got != "" {
		t.Errorf("KeyPressMsg while blurred: Value() = %q, want empty", got)
	}

	m, cmd = m.Update(tea.PasteMsg{Content: "pasted text"})
	if cmd != nil {
		t.Errorf("PasteMsg while blurred: got a cmd, want none")
	}
	if got := m.Value(); got != "" {
		t.Errorf("PasteMsg while blurred: Value() = %q, want empty", got)
	}
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

// TestPrompt_GoldenPlaceholder renders at width 80 (a realistic terminal
// width) so the full placeholder — "Message coder…  (ctrl+p actions · @
// files)" — fits on one line untouched (review item 3).
func TestPrompt_GoldenPlaceholder(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(xansi.Strip(view), "Message coder…  (ctrl+p actions · @ files)") {
		t.Errorf("View() does not contain the full placeholder at width 80:\n%s", view)
	}
	golden.Assert(t, "prompt_placeholder", view)
}

func TestPrompt_GoldenQueued(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(80)
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

// TestPrompt_PlaceholderTruncatesNarrow covers review item 3's other half:
// a width too narrow for the full placeholder truncates gracefully to one
// line ending in "…", rather than bubbles/textarea's own multi-line
// wrapping silently cutting it off with no ellipsis.
func TestPrompt_PlaceholderTruncatesNarrow(t *testing.T) {
	t.Parallel()

	m := New(nil, WithStyles(pinnedStyles()))
	m.SetAgent("coder")
	m.SetWidth(20)
	view := m.View()

	lines := strings.Split(view, "\n")
	if len(lines) != 3 { // top border + 1 content line + bottom border
		t.Fatalf("View() has %d lines, want 3 (placeholder must stay on one line):\n%s", len(lines), view)
	}
	if !strings.Contains(xansi.Strip(lines[1]), "…") {
		t.Errorf("content line does not end in an ellipsis: %q", lines[1])
	}
	if strings.Contains(xansi.Strip(view), "files)") {
		t.Errorf("full placeholder text leaked through at a too-narrow width:\n%s", view)
	}
	golden.Assert(t, "prompt_placeholder_narrow", view)
}
