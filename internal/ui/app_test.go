package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/transcript"
)

func TestApp_TinySizesDoNotPanic(t *testing.T) {
	t.Parallel()
	for _, sz := range [][2]int{{80, 24}, {40, 10}, {20, 5}, {1, 1}, {0, 0}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			t.Parallel()
			ta := newTestApp(t)
			ta.sendAndAdopt("hello there, a message long enough to wrap at small sizes")
			ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "# Title\n\nsome **markdown** text"})
			ta.fire()
			ta.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			ta.fire()
			assertFits(t, ta.view(), sz[0], sz[1])
			// A width change mid-stream, before the debounced list resize lands.
			ta.send(tea.WindowSizeMsg{Width: sz[0] + 7, Height: sz[1]})
			ta.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: " more"})
			assertFits(t, ta.view(), sz[0], sz[1])
			ta.fire()
			assertFits(t, ta.view(), sz[0], sz[1])
		})
	}
}

// assertFits checks that view has at most h lines, none wider than w.
func assertFits(t *testing.T, view string, w, h int) {
	t.Helper()
	if view == "" {
		return
	}
	lines := strings.Split(view, "\n")
	if len(lines) > h {
		t.Errorf("%d lines, want <= %d", len(lines), h)
	}
	for i, l := range lines {
		if lw := ansi.Width(l); lw > w {
			t.Errorf("line %d is %d wide, want <= %d: %q", i, lw, w, l)
		}
	}
}

func TestApp_SendNewSessionAdoptsRoot(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.typeText("hello")
	ta.key("enter")

	if len(ta.chat.sends) != 1 {
		t.Fatalf("sends = %d, want 1", len(ta.chat.sends))
	}
	got := ta.chat.sends[0]
	want := core.SendRequest{Agent: "build", Text: "hello"}
	if got.SessionID != want.SessionID || got.Agent != want.Agent || got.Text != want.Text {
		t.Errorf("send = %+v, want %+v", got, want)
	}
	if v := ta.app.w.prompt.Value(); v != "" {
		t.Errorf("prompt = %q, want reset", v)
	}
	if n := ta.app.w.list.Len(); n != 1 {
		t.Errorf("list has %d items, want the pending user block", n)
	}

	// Neither a descendant's session nor a session from another run is
	// adopted.
	ta.event(event.SessionCreated{Base: event.Base{SessionID: "ses_kid", RootID: "ses_other"}, Info: core.Session{ID: "ses_kid", ParentID: "ses_other"}})
	if id := ta.app.sess.info.ID; id != "" {
		t.Fatalf("adopted %q from a descendant SessionCreated", id)
	}

	ta.event(event.SessionCreated{Base: rootBase(), Info: core.Session{ID: "ses_1", Agent: "build"}})
	if id := ta.app.sess.info.ID; id != "ses_1" {
		t.Fatalf("root = %q, want ses_1", id)
	}

	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "hi back"})
	ta.fire()
	if n := ta.app.w.list.Len(); n != 2 {
		t.Errorf("list has %d items after a root delta, want 2", n)
	}
	if !strings.Contains(xansi.Strip(ta.view()), "hi back") {
		t.Error("streamed text not shown after adoption")
	}

	// A second SessionCreated never replaces the adopted root.
	ta.event(event.SessionCreated{Base: event.Base{SessionID: "ses_2", RootID: "ses_2"}, Info: core.Session{ID: "ses_2"}})
	if id := ta.app.sess.info.ID; id != "ses_1" {
		t.Errorf("root changed to %q", id)
	}
}

func TestApp_IdleSessionCreatedNotAdopted(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.event(event.SessionCreated{Base: rootBase(), Info: core.Session{ID: "ses_1"}})
	if id := ta.app.sess.info.ID; id != "" {
		t.Errorf("adopted %q while no send was in flight", id)
	}
}

func TestApp_QueueSendsAfterRun(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("first")

	ta.typeText("second")
	ta.key("enter")
	if len(ta.chat.sends) != 1 {
		t.Fatalf("sends = %d while running, want 1", len(ta.chat.sends))
	}
	if !ta.app.sess.queued || ta.app.w.prompt.Value() != "second" {
		t.Fatalf("queued = %v, prompt = %q; want queued with the text kept", ta.app.sess.queued, ta.app.w.prompt.Value())
	}
	if !strings.Contains(ta.view(), "⏳ queued") {
		t.Error("prompt border lacks ⏳ queued")
	}

	// Another session's run ending doesn't release the queue.
	ta.event(event.RunFinished{Base: event.Base{SessionID: "ses_9", RootID: "ses_9"}})
	if len(ta.chat.sends) != 1 {
		t.Fatal("queue sent on another session's RunFinished")
	}

	ta.event(event.RunFinished{Base: rootBase(), MessageID: "m1"})
	if len(ta.chat.sends) != 2 {
		t.Fatalf("sends = %d after RunFinished, want 2", len(ta.chat.sends))
	}
	if got := ta.chat.sends[1]; got.SessionID != "ses_1" || got.Text != "second" || got.Agent != "" {
		t.Errorf("queued send = %+v, want ses_1/second (no agent on an existing session)", got)
	}
	if ta.app.sess.queued || ta.app.w.prompt.Value() != "" || !ta.app.sess.run.running {
		t.Errorf("after the queued send: queued=%v prompt=%q running=%v", ta.app.sess.queued, ta.app.w.prompt.Value(), ta.app.sess.run.running)
	}
}

func TestApp_CtrlCLadder(t *testing.T) {
	t.Parallel()
	t.Run("idle empty quits", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.key("ctrl+c")
		if !ta.quit {
			t.Error("did not quit")
		}
	})
	t.Run("idle with text clears the prompt", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.typeText("draft")
		ta.key("ctrl+c")
		if ta.quit || ta.app.w.prompt.Value() != "" {
			t.Errorf("quit=%v prompt=%q; want the prompt cleared", ta.quit, ta.app.w.prompt.Value())
		}
		ta.key("ctrl+c")
		if !ta.quit {
			t.Error("second ctrl+c on an empty prompt did not quit")
		}
	})
	t.Run("running cancels", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.sendAndAdopt("go")
		ta.typeText("draft")
		ta.key("ctrl+c")
		if !slices.Equal(ta.chat.cancels, []core.SessionID{"ses_1"}) || ta.quit {
			t.Errorf("cancels = %v quit=%v; want one Cancel(ses_1)", ta.chat.cancels, ta.quit)
		}
		if ta.app.w.prompt.Value() != "draft" {
			t.Errorf("prompt = %q; cancelling must not clear it", ta.app.w.prompt.Value())
		}
	})
	t.Run("queued and running", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.sendAndAdopt("go")
		ta.typeText("later")
		ta.key("enter")
		ta.key("ctrl+c")
		if ta.app.sess.queued || !slices.Equal(ta.chat.cancels, []core.SessionID{"ses_1"}) {
			t.Fatalf("queued=%v cancels=%v; want the queue cleared and Cancel in one press", ta.app.sess.queued, ta.chat.cancels)
		}
		ta.event(event.RunFailed{Base: rootBase(), Err: "cancelled"})
		if len(ta.chat.sends) != 1 {
			t.Errorf("sends = %d after RunFailed, want nothing more sent", len(ta.chat.sends))
		}
		if ta.app.sess.run.running {
			t.Error("still running after RunFailed")
		}
	})
}

func TestApp_CtrlDQuitsOnlyOnEmptyPrompt(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.typeText("x")
	ta.key("ctrl+d")
	if ta.quit {
		t.Fatal("ctrl+d quit with text in the prompt")
	}
	ta.key("ctrl+c")
	ta.key("ctrl+d")
	if !ta.quit {
		t.Error("ctrl+d on an empty prompt did not quit")
	}
}

func TestApp_StreamingCoalescesTo80ms(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.fire() // drain the tick the send started
	ta.app.w.upserts = 0

	for i := range 10 {
		ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: fmt.Sprintf("w%d ", i)})
	}
	if n := ta.app.w.upserts; n != 0 {
		t.Fatalf("upserts = %d before the tick, want 0", n)
	}
	if len(ta.deferred) != 1 || ta.deferred[0].d != 80*time.Millisecond {
		t.Fatalf("deferred = %+v, want one 80ms streamTick", ta.deferred)
	}
	ta.fire()
	if n := ta.app.w.upserts; n != 1 {
		t.Errorf("upserts = %d after one tick, want exactly 1", n)
	}
	if !strings.Contains(xansi.Strip(ta.view()), "w9") {
		t.Error("the tick did not render the streamed text")
	}

	// StepFinished gives the message's text blocks their final render at
	// once, without waiting for a tick.
	ta.app.w.upserts = 0
	ta.event(event.StepFinished{Base: rootBase(), MessageID: "m1"})
	if n := ta.app.w.upserts; n != 1 {
		t.Errorf("upserts = %d on StepFinished, want 1", n)
	}

	// The tick stops once the run ends.
	ta.event(event.RunFinished{Base: rootBase(), MessageID: "m1"})
	ta.fire()
	if len(ta.deferred) != 0 {
		t.Errorf("tick rescheduled after the run ended: %+v", ta.deferred)
	}
}

func TestApp_ToolDurationsFromClock(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	call := core.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"make test"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.clk.Advance(1200 * time.Millisecond)
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: "c1", Name: "bash", Output: "ok"}})
	if d := ta.app.sess.tools.durs["t/c1"]; d != 1200*time.Millisecond {
		t.Errorf("duration = %v, want 1.2s", d)
	}
}

func TestApp_TabCyclesAgentsAndConfigures(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	if a := ta.app.sess.info.Agent; a != "build" {
		t.Fatalf("initial agent = %q, want build", a)
	}
	ta.key("tab")
	if a := ta.app.sess.info.Agent; a != "plan" {
		t.Fatalf("after tab agent = %q, want plan", a)
	}
	if !strings.Contains(xansi.Strip(ta.view()), "Message plan…") {
		t.Error("placeholder does not name the new agent")
	}
	if len(ta.sessions.configure) != 0 {
		t.Fatalf("Configure called without a session: %+v", ta.sessions.configure)
	}
	ta.key("tab")
	if a := ta.app.sess.info.Agent; a != "build" {
		t.Fatalf("tab wraps to %q, want build", a)
	}

	ta.sendAndAdopt("hi")
	ta.key("shift+tab")
	if a := ta.app.sess.info.Agent; a != "plan" {
		t.Fatalf("shift+tab agent = %q, want plan", a)
	}
	want := []configureCall{{ID: "ses_1", Agent: "plan"}}
	if !slices.Equal(ta.sessions.configure, want) {
		t.Errorf("configure = %+v, want %+v", ta.sessions.configure, want)
	}
}

func TestApp_StatusContextAndCost(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.app.opts.Aliases = map[string]string{"sonnet": "anthropic/claude-sonnet-5"}
	ta.sendAndAdopt("go")
	ta.event(event.StepFinished{Base: rootBase(), MessageID: "m1", Usage: core.Usage{Input: 1000, CacheRead: 500, Output: 10}, CostUSD: 0.25})
	ta.event(event.StepFinished{Base: rootBase(), MessageID: "m2", Usage: core.Usage{Input: 2000, CacheRead: 300}, CostUSD: 0.5})
	// A subagent's step is its own session's usage, not the root's.
	ta.event(event.StepFinished{Base: event.Base{SessionID: "ses_kid", RootID: "ses_1"}, MessageID: "k1", Usage: core.Usage{Input: 99999}, CostUSD: 9})
	ta.clk.Advance(12 * time.Second)

	st := ta.app.statusState()
	if st.CtxUsed != 2300 || st.CtxLimit != 200000 {
		t.Errorf("ctx = %d/%d, want 2300/200000", st.CtxUsed, st.CtxLimit)
	}
	if st.CostUSD != 0.75 {
		t.Errorf("cost = %v, want 0.75", st.CostUSD)
	}
	if st.Model != "sonnet" || st.Agent != "build" || st.Mode != "INSERT" {
		t.Errorf("mode/agent/model = %q/%q/%q, want INSERT/build/sonnet", st.Mode, st.Agent, st.Model)
	}
	if !st.Running || st.Elapsed != 12*time.Second {
		t.Errorf("running=%v elapsed=%v, want running 12s", st.Running, st.Elapsed)
	}

	// Without an alias, the model shows as its short ID.
	ta.app.opts.Aliases = nil
	if st := ta.app.statusState(); st.Model != "claude-sonnet-5" {
		t.Errorf("model = %q, want claude-sonnet-5", st.Model)
	}
}

func TestApp_StatusSanitizesSessionStrings(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.event(event.SessionUpdated{Base: rootBase(), Info: core.Session{ID: "ses_1", Agent: "bu\x1b[31mild", Model: "x/evil\x1b]0;t\x07"}})
	st := ta.app.statusState()
	for _, s := range []string{st.Agent, st.Model} {
		if strings.ContainsRune(s, '\x1b') {
			t.Errorf("unsanitized status string %q", s)
		}
	}
}

func TestApp_ResumeLoadsSession(t *testing.T) {
	t.Parallel()
	sess := core.Session{ID: "ses_old", Title: "Old work", Agent: "plan"}
	msgs := []core.Message{
		{ID: "u1", SessionID: "ses_old", Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "earlier"}}},
		{ID: "a1", SessionID: "ses_old", Role: core.RoleAssistant, Model: "anthropic/claude-sonnet-5", CostUSD: 0.1,
			Usage: core.Usage{Input: 400, CacheRead: 100}, Parts: []core.Part{{Kind: core.PartText, Text: "answer"}}},
	}
	todos := []core.Todo{{Content: "write tests", Status: "in_progress"}}
	ta := newTestApp(t, withResume(sess, msgs, todos))

	if n := ta.app.w.list.Len(); n != 2 {
		t.Fatalf("list has %d items after resume, want 2", n)
	}
	s := ta.app.sess
	if s.info.Title != "Old work" || s.info.Agent != "plan" || s.cost != 0.1 || len(s.todos) != 1 {
		t.Errorf("resumed state: info=%+v cost=%v todos=%v", s.info, s.cost, s.todos)
	}
	if st := ta.app.statusState(); st.CtxUsed != 500 {
		t.Errorf("ctx used = %d, want the last step's 500", st.CtxUsed)
	}
	if !strings.Contains(xansi.Strip(ta.view()), "answer") {
		t.Error("resumed transcript not shown")
	}
}

func TestApp_HistoryAppendedAndCapped(t *testing.T) {
	t.Parallel()
	old := make([]string, 100)
	for i := range old {
		old[i] = fmt.Sprintf("p%d", i)
	}
	ta := newTestApp(t, withPrefs(core.Prefs{History: map[string][]string{testWorkDir: old}}))
	ta.typeText("newest")
	ta.key("enter")

	h := ta.prefs.Get().History[testWorkDir]
	if len(h) != 100 || h[99] != "newest" || h[0] != "p1" {
		t.Errorf("history len %d, first %q, last %q; want 100 ending in newest", len(h), h[0], h[len(h)-1])
	}
	// The prompt walks the updated history at once.
	ta.key("up")
	if v := ta.app.w.prompt.Value(); v != "newest" {
		t.Errorf("↑ gives %q, want newest", v)
	}
}

func TestApp_QuitCancelsBaseContext(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.typeText("go")
	ta.key("enter")
	ctx := ta.chat.ctxs[0]
	if ctx.Err() != nil {
		t.Fatal("send context cancelled before quit")
	}
	ta.run(ta.app.quit())
	if ctx.Err() == nil {
		t.Error("quitting did not cancel the base context the send ran under")
	}
}

func TestApp_ResizeDebouncesListWidth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	w0 := ta.app.view.listW
	ta.send(tea.WindowSizeMsg{Width: 130, Height: 30})
	ta.send(tea.WindowSizeMsg{Width: 140, Height: 30})
	ta.send(tea.WindowSizeMsg{Width: 150, Height: 32})
	if ta.app.view.listW != w0 {
		t.Fatalf("list width changed to %d before the debounce tick", ta.app.view.listW)
	}
	if ta.app.lay.Transcript.W != 102 {
		t.Fatalf("layout not applied at once: %+v", ta.app.lay.Transcript)
	}
	if ta.app.view.listH != ta.app.lay.Transcript.H {
		t.Errorf("list height %d, want %d applied at once", ta.app.view.listH, ta.app.lay.Transcript.H)
	}
	for _, d := range ta.deferred {
		if d.d != 50*time.Millisecond {
			t.Errorf("debounce delay %v, want 50ms", d.d)
		}
	}
	ta.fire()
	if ta.app.view.listW != 102 {
		t.Errorf("list width = %d after the debounce, want 102", ta.app.view.listW)
	}
}

func TestApp_ChangedFilesNormalized(t *testing.T) {
	t.Parallel()
	in := []transcript.FileChange{
		{Path: "a.go", Kind: transcript.ChangeAdded},
		{Path: "./a.go", Kind: transcript.ChangeModified},
		{Path: testWorkDir + "/a.go", Kind: transcript.ChangeModified},
		{Path: "sub/../b.go", Kind: transcript.ChangeModified},
		{Path: "/elsewhere/c.go", Kind: transcript.ChangeAdded},
	}
	got := normalizeChanges(testWorkDir, in)
	want := []transcript.FileChange{
		{Path: "a.go", Kind: transcript.ChangeAdded},
		{Path: "b.go", Kind: transcript.ChangeModified},
		{Path: "/elsewhere/c.go", Kind: transcript.ChangeAdded},
	}
	if !slices.Equal(got, want) {
		t.Errorf("normalizeChanges = %+v, want %+v", got, want)
	}
}

func TestApp_SidebarListsNormalizedFiles(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(150, 40))
	ta.sendAndAdopt("go")
	for i, p := range []string{"a.go", "./a.go", testWorkDir + "/a.go"} {
		id := fmt.Sprintf("c%d", i)
		call := core.ToolCall{ID: id, Name: "write", Input: []byte(fmt.Sprintf(`{"path":%q,"content":"x"}`, p))}
		ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
		ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: id, Name: "write", Output: "wrote"}})
	}
	side := xansi.Strip(ta.app.w.side.View())
	if n := strings.Count(side, "a.go"); n != 1 {
		t.Errorf("a.go listed %d times in the sidebar, want once:\n%s", n, side)
	}
}

func TestApp_EscEntersNormalAndIReturns(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.key("esc")
	if ta.app.mode != modeNormal {
		t.Fatalf("mode = %v after esc, want NORMAL", ta.app.mode)
	}
	ta.typeText("x") // not typed into the prompt in NORMAL
	ta.key("i")
	if ta.app.mode != modeInsert || ta.app.w.prompt.Value() != "" {
		t.Errorf("mode = %v prompt = %q; want INSERT with an empty prompt", ta.app.mode, ta.app.w.prompt.Value())
	}
}

func TestApp_ViewIsAltScreen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	if !ta.app.View().AltScreen {
		t.Error("View is not on the alt screen")
	}
}

func TestApp_GoldenIdle(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	golden.Assert(t, "app_idle", ta.view())
}

func TestApp_GoldenStreaming(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("Explain the build")
	ta.event(event.MessageStarted{Base: rootBase(), MessageID: "m1", Agent: "build", Model: "anthropic/claude-sonnet-5"})
	ta.event(event.ReasoningDelta{Base: rootBase(), MessageID: "m1", Text: "Let me look at the Makefile first."})
	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "The build runs **make check**:\n\n- `go test`\n- `golangci-lint`"})
	call := core.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"make check"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.clk.Advance(3 * time.Second)
	ta.fire()
	golden.Assert(t, "app_streaming", ta.view())
}
