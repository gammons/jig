package permcard

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPermcard_SetStylesRestylesAndBumpsVersion(t *testing.T) {
	t.Parallel()
	reply := func(string, Reply) tea.Cmd { return nil }
	req := &Request{ID: "p1", Tool: "bash", Subject: "rm -rf /tmp/x"}
	m := New(reply)
	m.SetWidth(80)
	m.Set(req)
	want := New(reply, WithStyles(pinnedStyles()))
	want.SetWidth(80)
	want.Set(req)

	v := m.Version()
	m.SetStyles(pinnedStyles())
	if m.Version() != v+1 {
		t.Fatalf("Version() = %d after SetStyles, want %d", m.Version(), v+1)
	}
	if got := m.View(); got != want.View() {
		t.Fatalf("View() after SetStyles = %q, want %q", got, want.View())
	}
}

func TestPermcard_DenyInputDoesNotBlink(t *testing.T) {
	t.Parallel()
	m := New(func(string, Reply) tea.Cmd { return nil })
	m.Set(&Request{ID: "p1", Tool: "bash", Subject: "ls"})
	m, _ = m.Update(keyMsg("D"))
	if !m.Typing() {
		t.Fatal("D did not open the deny input")
	}
	if m.input.Styles().Cursor.Blink {
		t.Fatal("the deny input's cursor blinks (a real-time timer Cmd); want it static")
	}
}
