package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// groupMessages is a stored session whose exploration folds into one
// group: a read, then (next step) reasoning and a grep, then the reply.
// Its blocks are u/u1, t/c1, m/a2/0, t/c2, m/a3/0; the group is g/c1.
func groupMessages() []core.Message {
	msg := func(id string, role core.Role, parts ...core.Part) core.Message {
		return core.Message{ID: core.MessageID(id), SessionID: "ses_1", Role: role, Status: core.StatusComplete, Parts: parts}
	}
	call := func(id, name, input string) core.Part {
		return core.Part{Kind: core.PartToolCall, Call: &core.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}}
	}
	result := func(id, name, out string) core.Part {
		return core.Part{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: id, Name: name, Output: out}}
	}
	return []core.Message{
		msg("u1", core.RoleUser, core.Part{Kind: core.PartText, Text: "look around"}),
		msg("a1", core.RoleAssistant, call("c1", "read", `{"path":"a.go"}`), result("c1", "read", "1: package a")),
		msg("a2", core.RoleAssistant, core.Part{Kind: core.PartReasoning, Text: "now search"},
			call("c2", "grep", `{"pattern":"TODO"}`), result("c2", "grep", "b.go:3: // TODO")),
		msg("a3", core.RoleAssistant, core.Part{Kind: core.PartText, Text: "Found one TODO in b.go."}),
	}
}

// resumeGroups resumes ses_1 with groupMessages.
func resumeGroups() testOpt {
	return withResume(core.Session{ID: "ses_1", Agent: "build"}, groupMessages(), nil)
}

// listIDs returns the transcript list's item IDs in order, walking a copy
// of the list, so the App's own selection is untouched.
func (ta *testApp) listIDs() []string {
	l := ta.app.w.list
	l.Top()
	ids := make([]string, 0, l.Len())
	for range l.Len() {
		it, _ := l.Selected()
		ids = append(ids, it.ID)
		l, _ = l.Update(keyPress("j"))
	}
	return ids
}

// startTool delivers a root ToolCallStarted for call id of tool name.
func (ta *testApp) startTool(msg core.MessageID, id, name, input string) {
	ta.t.Helper()
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: msg,
		Call: core.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}})
}

// finishTool delivers a root ToolCallFinished for call id.
func (ta *testApp) finishTool(msg core.MessageID, id, name, out string, isErr bool) {
	ta.t.Helper()
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: msg,
		Result: core.ToolResult{CallID: id, Name: name, Output: out, IsError: isErr}})
}

func TestFold_SecondExplorationCallFormsGroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"t/c1"}) {
		t.Fatalf("after one read, list = %v, want [<user> t/c1]", got)
	}
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		t.Fatalf("after two reads, list = %v, want [<user> g/c1]", got)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the following view on g/c1", got)
	}
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "exploring · 2 reads · read b.go") {
		t.Errorf("no live header in the view:\n%s", view)
	}
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.finishTool("m1", "c2", "read", "1: package b", false)
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "▸ explored · 2 reads") {
		t.Errorf("no settled header in the view:\n%s", view)
	}
}

func TestFold_HiddenMembersAreNotLive(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	live := ta.app.sess.track.live
	if !live["g/c1"] || live["t/c1"] || live["t/c2"] {
		t.Errorf("live = %v, want the header g/c1 live and its hidden members not", live)
	}
}

func TestFold_LoneCallStaysPlain(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "done"})
	ta.fire()
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"t/c1", "m/m1/0"}) {
		t.Errorf("list = %v, want [<user> t/c1 m/m1/0]", got)
	}
}

func TestFold_StoredSessionLoadsCollapsed(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "▸ explored · 1 read, 1 grep") {
		t.Errorf("no collapsed header in the view:\n%s", view)
	}
}

func TestFold_CancelSettlesLiveGroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	ta.event(event.RunFailed{Base: rootBase(), Err: "cancelled"})
	if view := xansi.Strip(ta.view()); !strings.Contains(view, "▸ explored · 2 reads · 2 cancelled ⊘") {
		t.Errorf("no cancelled header in the view:\n%s", view)
	}
	if ta.app.sess.track.live["g/c1"] {
		t.Error("the cancelled group is still live (its spinner would keep ticking)")
	}
}

func TestFold_ThemeChangeRerendersHeader(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	before := ta.app.sess.track.versions["g/c1"]
	if before == 0 {
		t.Fatal("the header g/c1 was never issued")
	}
	pushTheme(ta.app)
	if got := ta.app.sess.track.versions["g/c1"]; got <= before {
		t.Errorf("header version after a theme change = %d, want > %d", got, before)
	}
}
