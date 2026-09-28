package ui

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestInsert_FixedKeysBeatKeymap(t *testing.T) {
	t.Parallel()
	// A config binding a fixed key must never override it (R21).
	ta := newTestApp(t, withKeybinds(map[string]string{
		"insert.enter":  "app.quit",
		"insert.ctrl+c": "view.sidebar",
		"insert.tab":    "app.quit",
		"insert.esc":    "app.quit",
	}))
	ta.typeText("hello")
	ta.key("enter")
	if ta.quit || len(ta.chat.sends) != 1 {
		t.Fatalf("enter: quit=%v sends=%d; want a send", ta.quit, len(ta.chat.sends))
	}
	ta.key("tab")
	if ta.quit || ta.app.sess.info.Agent != "plan" {
		t.Fatalf("tab: quit=%v agent=%q; want the agent cycled", ta.quit, ta.app.sess.info.Agent)
	}
	ta.key("esc")
	if ta.quit || ta.app.mode != modeNormal {
		t.Fatalf("esc: quit=%v mode=%v; want NORMAL", ta.quit, ta.app.mode)
	}
}

func TestInsert_RemappableKeyRunsAction(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withKeybinds(map[string]string{"insert.ctrl+q": "app.quit"}))
	ta.key("ctrl+q")
	if !ta.quit {
		t.Error("a key bound to app.quit did not quit")
	}
}

func TestInsert_SidebarToggleSavesPref(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	if !ta.app.lay.SideVisible {
		t.Fatal("sidebar hidden at 120 cols by default")
	}
	ta.key("ctrl+b")
	if ta.app.lay.SideVisible {
		t.Error("ctrl+b did not hide the sidebar")
	}
	if p := ta.prefs.Get().Sidebar; p == nil || *p {
		t.Errorf("prefs.Sidebar = %v, want false", p)
	}
	ta.key("ctrl+b")
	if !ta.app.lay.SideVisible {
		t.Error("second ctrl+b did not show the sidebar")
	}
}

func TestInsert_SidebarPrefLoadedAtStart(t *testing.T) {
	t.Parallel()
	off := false
	ta := newTestApp(t, withPrefs(core.Prefs{Sidebar: &off}))
	if ta.app.lay.SideVisible {
		t.Error("a saved sidebar=false pref was ignored")
	}
}

func TestInsert_MentionInsertsLiteralAt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.typeText("see @x")
	if v := ta.app.w.prompt.Value(); v != "see @x" {
		t.Errorf("prompt = %q, want the @ kept", v)
	}
}

// fakeExec is a core.ExecCommand that does nothing.
type fakeExec struct{}

func (fakeExec) Run() error          { return nil }
func (fakeExec) SetStdin(io.Reader)  {}
func (fakeExec) SetStdout(io.Writer) {}
func (fakeExec) SetStderr(io.Writer) {}

// fakeEditor implements core.EditorService, returning edited text.
type fakeEditor struct {
	got    string
	edited string
	err    error
}

func (f *fakeEditor) Edit(text string) (core.ExecCommand, func() (string, error), error) {
	f.got = text
	if f.err != nil {
		return nil, nil, f.err
	}
	return fakeExec{}, func() (string, error) { return f.edited, nil }, nil
}

func TestInsert_EditorRoundTrip(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ed := &fakeEditor{edited: "from the editor"}
	ta.app.ports.Editor = ed
	ta.typeText("draft")
	ta.key("ctrl+e")
	if ed.got != "draft" {
		t.Fatalf("editor got %q, want draft", ed.got)
	}
	// tea.Exec runs the command in the real program; deliver its exit.
	ta.send(editorExitedMsg{result: func() (string, error) { return ed.edited, nil }})
	if v := ta.app.w.prompt.Value(); v != "from the editor" {
		t.Errorf("prompt = %q, want the edited text", v)
	}
}

func TestInsert_EditorErrorBecomesHint(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.app.ports.Editor = &fakeEditor{err: errors.New("no \x1b[31meditor")}
	ta.typeText("draft")
	ta.key("ctrl+e")
	st := ta.app.statusState()
	if !strings.Contains(st.Hint, "no") || strings.ContainsRune(st.Hint, '\x1b') {
		t.Errorf("hint = %q, want the sanitized error", st.Hint)
	}
	if v := ta.app.w.prompt.Value(); v != "draft" {
		t.Errorf("prompt = %q, want it untouched", v)
	}
}
