package ui

import (
	"encoding/json"
	"fmt"
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

// twoReadsThenReasoning forms g/c1 from two reads, then starts step m2
// with reasoning that a stream tick lists as a plain item.
func (ta *testApp) twoReadsThenReasoning() {
	ta.t.Helper()
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	ta.finishTool("m1", "c2", "read", "1: package b", false)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		ta.t.Fatalf("list = %v, want [<user> g/c1]", got)
	}
}

func (ta *testApp) reasonInStepTwo() {
	ta.t.Helper()
	ta.event(event.MessageStarted{Base: rootBase(), MessageID: "m2"})
	ta.event(event.ReasoningDelta{Base: rootBase(), MessageID: "m2", Text: "now grep"})
	ta.fire()
}

func TestFold_ReasoningAbsorbedAfterATickLeavesTheList(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.twoReadsThenReasoning()
	ta.reasonInStepTwo()
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1", "m/m2/0"}) {
		t.Fatalf("after the tick, list = %v, want [<user> g/c1 m/m2/0]", got)
	}
	ta.startTool("m2", "c3", "grep", `{"pattern":"TODO"}`)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		t.Fatalf("after the grep, list = %v, want [<user> g/c1]", got)
	}
	ta.event(event.RunFinished{Base: rootBase()})
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		t.Errorf("after the run, list = %v, want [<user> g/c1]", got)
	}
}

func TestFold_ReasoningAbsorbedIntoAnOpenGroupIsNested(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.twoReadsThenReasoning()
	f := &ta.app.sess.track.fold
	f.toggle("g/c1")
	f.regroup(ta.app.sess.proj.Blocks())
	foldCtl{ta.app}.relist(nil)
	ta.reasonInStepTwo()
	ta.startTool("m2", "c3", "grep", `{"pattern":"TODO"}`)
	want := []string{"g/c1", "t/c1", "t/c2", "m/m2/0", "t/c3"}
	if got := ta.listIDs(); !slices.Equal(got[1:], want) {
		t.Fatalf("list = %v, want [<user> %v]", got, want)
	}
	if !ta.app.sess.track.nested["m/m2/0"] {
		t.Error("the absorbed reasoning was never re-issued as nested")
	}
}

func TestFold_UpsertDoesNotDuplicateTheLayout(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	f := &ta.app.sess.track.fold
	seen := map[string]bool{}
	for _, e := range f.order {
		if seen[string(e.id)] {
			t.Fatalf("fold.order repeats %q: %v", e.id, f.order)
		}
		seen[string(e.id)] = true
	}
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	if _, changed := f.regroup(ta.app.sess.proj.Blocks()); changed {
		t.Errorf("regroup with nothing new reports a restructure; order = %v", f.order)
	}
}

func TestFold_OTogglesAGroup(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	want := []string{"u/u1", "g/c1", "t/c1", "m/a2/0", "t/c2", "m/a3/0"}
	if got := ta.listIDs(); !slices.Equal(got, want) {
		t.Fatalf("after o, list = %v, want %v", got, want)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the header g/c1", got)
	}
	view := xansi.Strip(ta.view())
	for _, s := range []string{"▾ explored · 1 read, 1 grep", "  ▸ read  a.go · 1 lines", `  ▸ grep  "TODO" · 1 matches`} {
		if !strings.Contains(view, s) {
			t.Errorf("view has no %q:\n%s", s, view)
		}
	}
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("after o again, list = %v, want %v", got, want)
	}
}

func TestFold_ToggleRerendersOnlyTheFlippedHeader(t *testing.T) {
	t.Parallel()
	msgs := groupMessages()
	msgs = append(msgs,
		core.Message{ID: "a4", SessionID: "ses_1", Role: core.RoleAssistant, Status: core.StatusComplete, Parts: []core.Part{
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "d1", Name: "read", Input: json.RawMessage(`{"path":"c.go"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "d1", Name: "read", Output: "1: package c"}},
			{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "d2", Name: "grep", Input: json.RawMessage(`{"pattern":"FIXME"}`)}},
			{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "d2", Name: "grep", Output: "c.go:1: // FIXME"}},
		}},
		core.Message{ID: "a5", SessionID: "ses_1", Role: core.RoleAssistant, Status: core.StatusComplete, Parts: []core.Part{
			{Kind: core.PartText, Text: "Done."},
		}})
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, msgs, nil))
	ta.key("esc")
	if !ta.app.w.list.Select("g/c1") {
		t.Fatal("no group g/c1 in the list")
	}
	v := ta.app.sess.track.versions
	first, other := v["g/c1"], v["g/d1"]
	ta.key("o")
	if got := v["g/c1"]; got <= first {
		t.Errorf("toggled header version %d → %d, want it re-rendered", first, got)
	}
	if got := v["g/d1"]; got != other {
		t.Errorf("other header version %d → %d, want nothing re-rendered", other, got)
	}
	first = v["g/c1"]
	ta.key("o")
	if got := v["g/c1"]; got <= first {
		t.Errorf("collapsed header version %d → %d, want it re-rendered", first, got)
	}
	if got := v["g/d1"]; got != other {
		t.Errorf("other header version %d → %d, want nothing re-rendered", other, got)
	}
}

func TestFold_OOnAFollowedHeaderKeepsTheSelection(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.startTool("m1", "c2", "read", `{"path":"b.go"}`)
	ta.finishTool("m1", "c2", "read", "1: package b", false)
	ta.key("esc")
	if got := ta.selectedID(); got != "g/c1" {
		t.Fatalf("selected = %q before o, want g/c1", got)
	}
	ta.key("o")
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1", "t/c1", "t/c2"}) {
		t.Fatalf("after o, list = %v, want the group expanded", got)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q after o, want the header g/c1", got)
	}
}

func TestFold_OOnAMemberCollapsesToTheHeader(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	ta.app.w.list.Select("t/c2")
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the header g/c1", got)
	}
}

func TestFold_OElsewhereDoesNothing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc") // the selection is on the reply, m/a3/0
	before := ta.app.sess.track.versions["g/c1"]
	ta.key("o")
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v unchanged", got, want)
	}
	if got := ta.app.sess.track.versions["g/c1"]; got != before {
		t.Errorf("header version %d → %d, want nothing re-rendered", before, got)
	}
}

func TestFold_KeyCanBeRemapped(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups(), withKeybinds(map[string]string{"normal.z": "transcript.fold"}))
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("z")
	if got := ta.listIDs(); len(got) != 6 {
		t.Errorf("after the remapped z, list = %v, want the group expanded", got)
	}
}

func TestFold_EnterOnAHeaderShowsItsMembers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("enter")
	if ta.app.view.detailsFor != "g/c1" {
		t.Fatalf("detailsFor = %q, want g/c1", ta.app.view.detailsFor)
	}
	body := xansi.Strip(ta.app.w.details.View())
	for _, s := range []string{"group · 1 read, 1 grep", "▸ read  a.go · 1 lines", "∴ thinking", `▸ grep  "TODO" · 1 matches`} {
		if !strings.Contains(body, s) {
			t.Errorf("details have no %q:\n%s", s, body)
		}
	}
}

func TestFold_YankOnAHeaderCopiesSubjects(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	cmd := normalKeys{ta.app}.yank()
	if cmd == nil {
		t.Fatal("yank on a header: want a Cmd")
	}
	if got := fmt.Sprint(cmd()); got != "a.go\nTODO" {
		t.Errorf("clipboard = %q, want %q", got, "a.go\nTODO")
	}
	if ta.app.view.hint != "yanked" {
		t.Errorf("hint = %q, want yanked", ta.app.view.hint)
	}
}

func TestFold_DetailsFollowCollapse(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	ta.app.w.list.Select("t/c1")
	ta.key("enter")
	if ta.app.view.detailsFor != "t/c1" {
		t.Fatalf("detailsFor = %q, want t/c1", ta.app.view.detailsFor)
	}
	ta.key("o")
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want g/c1", got)
	}
	if ta.app.view.detailsFor != "g/c1" {
		t.Errorf("detailsFor = %q, want the details to follow to g/c1", ta.app.view.detailsFor)
	}
	if body := xansi.Strip(ta.app.w.details.View()); !strings.Contains(body, "group · 1 read, 1 grep") {
		t.Errorf("details still show the hidden member:\n%s", body)
	}
}

func TestFold_SearchExpandsGroupsAndClearingRestores(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.key("/")
	ta.typeText("a.go")
	ta.key("enter")
	want := []string{"u/u1", "g/c1", "t/c1", "m/a2/0", "t/c2", "m/a3/0"}
	if got := ta.listIDs(); !slices.Equal(got, want) {
		t.Fatalf("with a search applied, list = %v, want %v", got, want)
	}
	ta.key("n")
	if got := ta.selectedID(); got != "t/c1" {
		t.Fatalf("n selected %q, want the match inside the group, t/c1", got)
	}
	ta.key("esc") // no split open: clears the search
	if got, want := ta.listIDs(), []string{"u/u1", "g/c1", "m/a3/0"}; !slices.Equal(got, want) {
		t.Errorf("after clearing the search, list = %v, want %v", got, want)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the hidden match's header g/c1", got)
	}
}

func TestFold_ClearingSearchKeepsAUserExpandedGroupOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, resumeGroups())
	ta.key("esc")
	ta.app.w.list.Select("g/c1")
	ta.key("o")
	ta.key("/")
	ta.typeText("nothing matches this")
	ta.key("enter")
	ta.key("esc")
	if got := ta.listIDs(); len(got) != 6 {
		t.Errorf("after clearing the search, list = %v, want g/c1 still expanded", got)
	}
}

func TestFold_SearchHoldSurvivesASessionSwitch(t *testing.T) {
	t.Parallel()
	other := core.Session{ID: "ses_2", Agent: "build"}
	ta := newTestApp(t, resumeGroups(),
		withSessions([]core.Session{other}, map[core.SessionID][]core.Message{"ses_2": groupMessages()}))
	ta.key("esc")
	ta.key("/")
	ta.typeText("TODO")
	ta.key("enter")
	if !ta.app.sess.track.fold.search {
		t.Fatal("no search hold after applying a search")
	}
	ta.send(resumeMsg{info: other, msgs: groupMessages()})
	if ta.app.sess.info.ID != "ses_2" {
		t.Fatalf("session = %q, want ses_2", ta.app.sess.info.ID)
	}
	if !ta.app.sess.track.fold.search {
		t.Error("the search is still applied but the groups are no longer held open")
	}
	found := false
	for range 3 {
		ta.key("n")
		if ta.selectedID() == "t/c2" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("n never selected the member match t/c2; list = %v, selected = %q", ta.listIDs(), ta.selectedID())
	}
}

func TestFold_PermissionForcesGroupOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("look around")
	ta.startTool("m1", "c1", "read", `{"path":"a.go"}`)
	ta.finishTool("m1", "c1", "read", "1: package a", false)
	ta.startTool("m1", "c2", "read", `{"path":"/etc/hosts"}`)
	ta.event(event.PermissionRequested{Base: rootBase(), RequestID: "p1", Tool: "read", Subject: "/etc/hosts",
		Call: core.ToolCall{ID: "c2", Name: "read"}})

	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1", "t/c1", "t/c2"}) {
		t.Fatalf("with a pending permission, list = %v, want [<user> g/c1 t/c1 t/c2]", got)
	}
	if got := ta.selectedID(); got != "t/c2" {
		t.Fatalf("selected = %q, want the requesting member t/c2 (the focus rule)", got)
	}
	ta.app.w.list.Top()
	ta.key("g")
	ta.key("p")
	if got := ta.selectedID(); got != "t/c2" {
		t.Fatalf("gp selected %q, want t/c2", got)
	}
	if req := ta.app.w.card.Request(); req == nil || req.ID != "p1" || !(permCtl{ta.app}).onCard() {
		t.Fatalf("card = %+v on %q, want p1 on the selected member", req, ta.app.w.cardAt.block)
	}
	ta.arm()
	ta.key("a")
	if len(ta.perms.replies) != 1 || ta.perms.replies[0].ID != "p1" {
		t.Fatalf("replies = %+v, want one for p1", ta.perms.replies)
	}

	ta.event(event.PermissionResolved{Base: rootBase(), RequestID: "p1", Reply: core.PermissionReply{Kind: core.ReplyOnce}})
	ta.finishTool("m1", "c2", "read", "1: 127.0.0.1 localhost", false)
	if got := ta.listIDs(); !slices.Equal(got[1:], []string{"g/c1"}) {
		t.Errorf("after the reply, list = %v, want the group collapsed again", got)
	}
	if got := ta.selectedID(); got != "g/c1" {
		t.Errorf("selected = %q, want the header g/c1", got)
	}
}
