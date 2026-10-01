package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/transcript"
)

func TestKids_SpawnCreatesLivePane(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "hi"})
	ta.event(event.ToolCallStarted{Base: childBase(), MessageID: "k1", Call: core.ToolCall{ID: "c1", Name: "bash"}})

	p, ok := ta.app.sess.kids["ses_c"]
	if !ok {
		t.Fatal("kids[ses_c] not created on spawn")
	}
	if p.title != "↳ explore: find the config" {
		t.Errorf("title = %q, want %q", p.title, "↳ explore: find the config")
	}
	blocks := p.proj.Blocks()
	if len(blocks) != 2 || blocks[0].Kind != transcript.KindText || blocks[1].Kind != transcript.KindTool {
		t.Fatalf("kid blocks = %+v, want [KindText, KindTool]", blocks)
	}
	for _, b := range ta.app.sess.main.proj.Blocks() {
		if b.Kind == transcript.KindText && b.Text == "hi" {
			t.Error("the root projection gained the child's text block")
		}
	}
}

func TestKids_PaneTitleShowsShortModel(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	call := core.ToolCall{ID: "c9", Name: "task", Input: []byte(`{"agent":"explore","description":"find the config"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.event(event.SubagentSpawned{Base: rootBase(), Child: "ses_c", Agent: "explore",
		Model: "openrouter/anthropic/claude-sonnet-4", Description: "find the config", CallID: "c9"})

	p, ok := ta.app.sess.kids["ses_c"]
	if !ok {
		t.Fatal("kids[ses_c] not created on spawn")
	}
	if want := "↳ explore (claude-sonnet-4): find the config"; p.title != want {
		t.Errorf("title = %q, want %q", p.title, want)
	}
}

func TestShortModel(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                                     "",
		"anthropic/haiku":                      "haiku",
		"openrouter/anthropic/claude-sonnet-4": "claude-sonnet-4",
		"bare":                                 "bare",
	} {
		if got := shortModel(in); got != want {
			t.Errorf("shortModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKids_LoadThenReplayNoDuplicates(t *testing.T) {
	t.Parallel()
	stored := []core.Message{
		{ID: "k1", SessionID: "ses_c", Role: core.RoleAssistant, Status: core.StatusComplete,
			Parts: []core.Part{{Kind: core.PartText, Text: "old"}}},
	}
	ta := newTestApp(t, withSessions(nil, map[core.SessionID][]core.Message{"ses_c": stored}))
	ta.sendAndAdopt("find it")

	p, cmd := kidsCtl{ta.app}.kid("ses_c", "explore", "", "find the config")
	if !p.load.loading {
		t.Fatal("test setup: pane should be loading")
	}
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "old"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "new"})
	ta.run(cmd)

	if p.load.loading {
		t.Error("pane still loading after childLoaded")
	}
	var texts []string
	for _, b := range p.proj.Blocks() {
		texts = append(texts, b.Text)
	}
	if len(texts) != 2 || texts[0] != "old" || texts[1] != "new" {
		t.Fatalf("blocks text = %v, want [old new]", texts)
	}
}

func TestKids_RunFailedReplayedOnlyIfNotLoaded(t *testing.T) {
	t.Parallel()
	t.Run("last stored msg failed: RunFailed dropped", func(t *testing.T) {
		t.Parallel()
		stored := []core.Message{
			{ID: "k1", SessionID: "ses_c", Role: core.RoleAssistant, Status: core.StatusFailed,
				Parts: []core.Part{{Kind: core.PartText, Text: "old"}}},
		}
		ta := newTestApp(t, withSessions(nil, map[core.SessionID][]core.Message{"ses_c": stored}))
		ta.sendAndAdopt("find it")
		p, cmd := kidsCtl{ta.app}.kid("ses_c", "explore", "", "find the config")
		ta.event(event.RunFailed{Base: childBase(), Err: "boom"})
		ta.run(cmd)
		if n := countNotices(p, "run failed"); n != 1 {
			t.Fatalf("run failed notices = %d, want 1", n)
		}
	})
	t.Run("last stored msg complete: RunFailed replayed", func(t *testing.T) {
		t.Parallel()
		stored := []core.Message{
			{ID: "k1", SessionID: "ses_c", Role: core.RoleAssistant, Status: core.StatusComplete,
				Parts: []core.Part{{Kind: core.PartText, Text: "old"}}},
		}
		ta := newTestApp(t, withSessions(nil, map[core.SessionID][]core.Message{"ses_c": stored}))
		ta.sendAndAdopt("find it")
		p, cmd := kidsCtl{ta.app}.kid("ses_c", "explore", "", "find the config")
		ta.event(event.RunFailed{Base: childBase(), Err: "boom"})
		ta.run(cmd)
		if n := countNotices(p, "run failed"); n != 1 {
			t.Fatalf("run failed notices = %d, want 1", n)
		}
	})
}

// countNotices counts p's notice blocks whose text contains substr.
func countNotices(p *pane, substr string) int {
	n := 0
	for _, b := range p.proj.Blocks() {
		if b.Kind == transcript.KindNotice && strings.Contains(b.Text, substr) {
			n++
		}
	}
	return n
}

func TestKids_LoadErrorShowsNotice(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	p, _ := kidsCtl{ta.app}.kid("ses_c", "explore", "", "find the config")
	ta.run(nil)
	msg := childLoadedMsg{session: "ses_c", gen: p.load.gen, err: errors.New("boom")}
	kidsCtl{ta.app}.childLoaded(msg)
	var found bool
	for _, b := range p.proj.Blocks() {
		if b.Kind == transcript.KindNotice {
			if !strings.HasPrefix(b.Text, "could not load subagent: ") {
				t.Errorf("notice text = %q, want prefix %q", b.Text, "could not load subagent: ")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no notice on load error")
	}
}

func TestKids_GrandchildGetsOwnPane(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.ToolCallStarted{Base: childBase(), MessageID: "k1",
		Call: core.ToolCall{ID: "c2", Name: "task", Input: []byte(`{"agent":"general","description":"deeper"}`)}})
	ta.event(event.SubagentSpawned{Base: childBase(), Child: "ses_g", Agent: "general", Description: "deeper", CallID: "c2"})

	gp, ok := ta.app.sess.kids["ses_g"]
	if !ok {
		t.Fatal("kids[ses_g] not created")
	}
	ta.event(event.ToolCallStarted{Base: event.Base{SessionID: "ses_g", RootID: "ses_1"}, MessageID: "g1",
		Call: core.ToolCall{ID: "c3", Name: "bash"}})

	cp := ta.app.sess.kids["ses_c"]
	var sub *transcript.Block
	for i, b := range cp.proj.Blocks() {
		if b.ID == "t/c2" {
			sub = &cp.proj.Blocks()[i]
		}
	}
	if sub == nil || sub.Sub == nil || sub.Sub.Tools != 1 {
		t.Fatalf("kids[ses_c]'s t/c2 = %+v, want Sub.Tools 1", sub)
	}
	if len(gp.proj.Blocks()) == 0 {
		t.Error("kids[ses_g] got no block for its own tool call")
	}
}

func TestKids_ClearedOnResumeAndSwitch(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	if len(ta.app.sess.kids) == 0 {
		t.Fatal("test setup: expected a kid pane")
	}
	// Settle the run so the resume isn't held (sessionState.load only
	// runs while idle; a busy resume defers via view.resumeHeld).
	ta.event(event.RunFinished{Base: rootBase(), MessageID: "m1"})
	ta.returnSend()
	ta.send(resumeMsg{info: core.Session{ID: "ses_other", Agent: "build"}})
	if n := len(ta.app.sess.kids); n != 0 {
		t.Fatalf("len(kids) after resuming a different session = %d, want 0", n)
	}
}

func TestReplayable(t *testing.T) {
	t.Parallel()
	loaded := map[core.MessageID]bool{"k1": true}
	tests := []struct {
		name       string
		ev         event.Event
		lastStatus core.MessageStatus
		want       bool
	}{
		{"loaded message dropped", event.TextDelta{MessageID: "k1"}, core.StatusComplete, false},
		{"unloaded message replayed", event.TextDelta{MessageID: "k2"}, core.StatusComplete, true},
		{"no message id replayed", event.SubagentSpawned{}, core.StatusComplete, true},
		{"RunFailed dropped after failed status", event.RunFailed{}, core.StatusFailed, false},
		{"RunFailed dropped after interrupted status", event.RunFailed{}, core.StatusInterrupted, false},
		{"RunFailed replayed after complete status", event.RunFailed{}, core.StatusComplete, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := replayable(tt.ev, loaded, tt.lastStatus); got != tt.want {
				t.Errorf("replayable(%T) = %v, want %v", tt.ev, got, tt.want)
			}
		})
	}
}

// TestKids_ChildGroupsRegroup checks that three consecutive read calls in
// a kid pane group into one fold entry, same as the root's track.
func TestKids_ChildGroupsRegroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	for i, name := range []string{"c1", "c2", "c3"} {
		ta.event(event.ToolCallStarted{Base: childBase(), MessageID: "k1",
			Call: core.ToolCall{ID: name, Name: "read", Input: []byte(`{"path":"a.go"}`)}})
		ta.event(event.ToolCallFinished{Base: childBase(),
			Result: core.ToolResult{CallID: name, Name: "read", Output: "ok"}})
		_ = i
	}
	p := ta.app.sess.kids["ses_c"]
	p.track.fold.regroup(p.proj.Blocks())
	if n := len(p.track.fold.groups); n != 1 {
		t.Fatalf("groups = %d, want 1", n)
	}
}
