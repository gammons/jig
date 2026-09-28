package permcard

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
		Text: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaf00")),
		Hint: lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
	}
}

// fakeReply records every call, returning nil Cmds.
type fakeReply struct {
	calls []call
}

type call struct {
	id string
	r  Reply
}

func (f *fakeReply) fn() ReplyFunc {
	return func(id string, r Reply) tea.Cmd {
		f.calls = append(f.calls, call{id, r})
		return func() tea.Msg { return nil }
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
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

func TestPermcard_KeysReply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want ReplyKind
	}{
		{"a", ReplyOnce},
		{"A", ReplyAlways},
		{"d", ReplyDeny},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			f := &fakeReply{}
			m := New(f.fn())
			m.SetWidth(60)
			m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "ls"})

			m, cmd := m.Update(keyMsg(tt.key))
			if cmd == nil {
				t.Fatalf("key %q produced no Cmd", tt.key)
			}
			if len(f.calls) != 1 {
				t.Fatalf("key %q produced %d calls, want 1", tt.key, len(f.calls))
			}
			got := f.calls[0]
			if got.id != "req-1" || got.r.Kind != tt.want {
				t.Fatalf("key %q call = %+v, want id req-1, Kind %s", tt.key, got, tt.want)
			}
			if m.Typing() {
				t.Fatalf("Typing() = true after %q, want false", tt.key)
			}
		})
	}
}

func TestPermcard_DenyWithMessage(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(60)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "rm -rf /"})

	m, _ = m.Update(keyMsg("D"))
	if !m.Typing() {
		t.Fatalf("Typing() = false after D, want true")
	}

	m = typeText(m, "not now")

	var cmd tea.Cmd
	m, cmd = m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatalf("enter while typing produced no Cmd")
	}
	if m.Typing() {
		t.Fatalf("Typing() = true after enter, want false")
	}
	if len(f.calls) != 1 {
		t.Fatalf("enter produced %d calls, want 1", len(f.calls))
	}
	got := f.calls[0]
	if got.id != "req-1" || got.r.Kind != ReplyDeny || got.r.Message != "not now" {
		t.Fatalf("call = %+v, want id req-1, Kind deny, Message %q", got, "not now")
	}
}

func TestPermcard_DenyMessageEscCancels(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(60)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "ls"})

	m, _ = m.Update(keyMsg("D"))
	m = typeText(m, "no")
	m, cmd := m.Update(keyMsg("esc"))
	if cmd != nil {
		t.Fatalf("esc while typing produced a Cmd, want nil")
	}
	if m.Typing() {
		t.Fatalf("Typing() = true after esc, want false")
	}
	if len(f.calls) != 0 {
		t.Fatalf("esc while typing sent a reply: %+v", f.calls)
	}
}

func TestPermcard_SubagentCopy(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(80)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "git push origin main", Subagent: "general"})

	got := xansi.Strip(m.View())
	want := "⚠ general (subagent) wants to run bash: git push origin main"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("View() first line = %q, want prefix %q", got, want)
	}
}

func TestPermcard_RootCopy(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(80)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "git push origin main"})

	lines := strings.Split(xansi.Strip(m.View()), "\n")
	if len(lines) != 2 {
		t.Fatalf("View() produced %d lines, want 2", len(lines))
	}
	if want := "⚠ bash wants to run:  git push origin main"; lines[0] != want {
		t.Fatalf("line 1 = %q, want %q", lines[0], want)
	}
	if want := "  a allow · A always (this exact command) · d deny · D deny with message"; lines[1] != want {
		t.Fatalf("line 2 = %q, want %q", lines[1], want)
	}
}

func TestPermcard_SubjectTruncated(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(30)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: strings.Repeat("x", 100)})

	lines := strings.Split(m.View(), "\n")
	if w := lipgloss.Width(lines[0]); w > 30 {
		t.Fatalf("line 1 width = %d, want <= 30", w)
	}
	if got := xansi.Strip(lines[0]); !strings.HasSuffix(got, "…") {
		t.Fatalf("line 1 = %q, want a truncated subject ending in …", got)
	}
}

func TestPermcard_EmptyViewWithoutRequest(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(60)
	if got := m.View(); got != "" {
		t.Fatalf("View() without a request = %q, want \"\"", got)
	}
}

func TestPermcard_ClearsOnSetNil(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(60)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "ls"})
	m.Set(nil)
	if m.Request() != nil {
		t.Fatalf("Request() after Set(nil) = %+v, want nil", m.Request())
	}
	if got := m.View(); got != "" {
		t.Fatalf("View() after Set(nil) = %q, want \"\"", got)
	}
}

func TestPermcard_VersionBumps(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn())
	m.SetWidth(60)
	v0 := m.Version()

	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "ls"})
	v1 := m.Version()
	if v1 <= v0 {
		t.Fatalf("version after Set = %d, want > %d", v1, v0)
	}

	// Setting an equal request is not a visible change.
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "ls"})
	if got := m.Version(); got != v1 {
		t.Fatalf("version after equal Set = %d, want unchanged %d", got, v1)
	}

	m, _ = m.Update(keyMsg("a")) // plain allow: no card-visible change
	if got := m.Version(); got != v1 {
		t.Fatalf("version after 'a' = %d, want unchanged %d", got, v1)
	}

	m, _ = m.Update(keyMsg("D"))
	v2 := m.Version()
	if v2 <= v1 {
		t.Fatalf("version after D = %d, want > %d", v2, v1)
	}

	m = typeText(m, "x")
	v3 := m.Version()
	if v3 <= v2 {
		t.Fatalf("version after typing = %d, want > %d", v3, v2)
	}

	m, _ = m.Update(keyMsg("esc"))
	v4 := m.Version()
	if v4 <= v3 {
		t.Fatalf("version after esc = %d, want > %d", v4, v3)
	}

	m.Set(nil)
	v5 := m.Version()
	if v5 <= v4 {
		t.Fatalf("version after Set(nil) = %d, want > %d", v5, v4)
	}
}

func TestGolden_CardRoot(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn(), WithStyles(pinnedStyles()))
	m.SetWidth(60)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "git push origin main"})
	golden.Assert(t, "card_root", m.View())
}

func TestGolden_CardTyping(t *testing.T) {
	t.Parallel()
	f := &fakeReply{}
	m := New(f.fn(), WithStyles(pinnedStyles()))
	m.SetWidth(60)
	m.Set(&Request{ID: "req-1", Tool: "bash", Subject: "git push origin main"})

	m, _ = m.Update(keyMsg("D"))
	m = typeText(m, "not needed")
	golden.Assert(t, "card_typing", m.View())
}
