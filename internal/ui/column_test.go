package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
)

// TestColumn_EnterOpensDetailsEntry: enter on a selected bash block opens
// a details entry, shown with its header in the breadcrumb row.
func TestColumn_EnterOpensDetailsEntry(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "echo hi")
	ta.key("esc")
	ta.key("enter")

	if n := len(ta.app.view.col); n != 1 {
		t.Fatalf("len(col) = %d, want 1", n)
	}
	if columnTop(ta.app).kind != paneDetails {
		t.Fatalf("top().kind = %v, want paneDetails", columnTop(ta.app).kind)
	}
	if got := xansi.Strip(ta.view()); !strings.Contains(got, "main › bash · ") {
		t.Errorf("view = %q, want a breadcrumb %q", got, "main › bash · ")
	}
}

// TestColumn_EnterSameBlockCloses: enter twice on the same block opens
// then closes the column.
func TestColumn_EnterSameBlockCloses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "echo hi")
	ta.key("esc")
	ta.key("enter")
	ta.key("enter")
	if n := len(ta.app.view.col); n != 0 {
		t.Fatalf("len(col) = %d, want 0", n)
	}
}

// TestColumn_FollowsMainCursor: with one details entry and main focused,
// k re-points top().forBlock to the new block.
func TestColumn_FollowsMainCursor(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	before := columnTop(ta.app).forBlock
	ta.key("k")
	after := columnTop(ta.app).forBlock
	if after == before {
		t.Fatalf("top().forBlock unchanged after k: %q", after)
	}
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || string(after) != sel.ID {
		t.Errorf("top().forBlock = %q, want the new selection %+v", after, sel)
	}
}

// TestColumn_EscFromMainCloses: esc from main closes the whole column.
func TestColumn_EscFromMainCloses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	if !columnOpen(ta.app) {
		t.Fatal("test setup: column not open")
	}
	ta.key("esc")
	if columnOpen(ta.app) {
		t.Fatal("esc from main did not close the column")
	}
}

// TestColumn_QPopsWhenColumnFocused: q from main closes the column
// (drilling into the column pops one entry, a later task).
func TestColumn_QPopsWhenColumnFocused(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	if !columnOpen(ta.app) {
		t.Fatal("test setup: column not open")
	}
	ta.key("q")
	if columnOpen(ta.app) {
		t.Fatal("q from main did not close the column")
	}
}

// TestColumn_StaleDetailsMsgDropped: open A, open B, then deliver A's
// detailsMsg. The content is still B's (replaces
// TestNormal_StaleDetailsMsgIgnored).
func TestColumn_StaleDetailsMsgDropped(t *testing.T) {
	t.Parallel()
	proj := fakeProject{files: map[string][]byte{"a.go": []byte("before old text after\n")}}
	ta := newTestApp(t)
	ta.app.ports.Project = proj
	ta.sendAndAdopt("go")
	call := core.ToolCall{ID: "c1", Name: "edit", Input: []byte(`{"path":"a.go","old_string":"old","new_string":"new"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: "c1", Output: "ok"}})
	ta.key("esc")

	if sel, ok := ta.app.sess.main.list.Selected(); !ok || !strings.HasPrefix(sel.ID, "t/") {
		t.Fatalf("selection = %+v, want the tool block", sel)
	}
	// Open A (the edit block) directly, bypassing auto-delivery, so its
	// async Cmd can be resolved after the selection (and column) moved on.
	_, cmd := ta.app.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("want an async Cmd for the edit's file context")
	}

	ta.key("k") // move to the user block: B replaces A synchronously
	before := columnTop(ta.app).body.View()
	if !strings.Contains(xansi.Strip(before), "go") {
		t.Fatalf("details after k = %q, want the user block's text", before)
	}

	ta.send(cmd()) // deliver the stale detailsMsg for A
	if got := columnTop(ta.app).body.View(); got != before {
		t.Errorf("a stale detailsMsg changed the details pane: %q", got)
	}
}

// TestColumn_ClosedOnSessionSwitch: resuming a different session closes
// the column and any kid panes, and returns focus to main.
func TestColumn_ClosedOnSessionSwitch(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	if !columnOpen(ta.app) {
		t.Fatal("test setup: column not open")
	}

	ta.send(resumeMsg{info: core.Session{ID: "ses_other", Agent: "build"}})
	if n := len(ta.app.view.col); n != 0 {
		t.Errorf("len(col) = %d, want 0", n)
	}
	if n := len(ta.app.sess.kids); n != 0 {
		t.Errorf("len(kids) = %d, want 0", n)
	}
	if ta.app.view.focus != focusMain {
		t.Errorf("focus = %v, want focusMain", ta.app.view.focus)
	}
	if strings.Contains(xansi.Strip(ta.view()), "main ›") {
		t.Error("view still shows the column's breadcrumb")
	}
}

// TestColumn_GoldenDetailsColumn: 120×30 with a details entry open, wide
// layout (breadcrumb + rule + body beside main).
func TestColumn_GoldenDetailsColumn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(120, 30), withResume(core.Session{ID: "ses_1", Agent: "build"}, detailsGoldenMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	golden.Assert(t, "app_details_column", ta.view())
}

// TestColumn_PopsWhenBlockDropped (spec §6): a details entry on a
// pending user block that fails before its run starts is popped when
// dropUser removes the block.
func TestColumn_PopsWhenBlockDropped(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.chat.errs = []error{core.ErrBusy}
	ta.typeText("go")
	ta.key("enter")
	ta.key("esc")
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || !strings.HasPrefix(sel.ID, "u/pending/") {
		t.Fatalf("selection = %+v, want the pending user block", sel)
	}
	ta.key("enter")
	if !columnOpen(ta.app) {
		t.Fatal("test setup: column not open")
	}
	ta.returnSend() // Send fails (ErrBusy): dropUser removes the pending block
	if n := len(ta.app.view.col); n != 0 {
		t.Fatalf("len(col) = %d, want 0 (the pending block's entry was popped)", n)
	}
}

// TestColumn_DetailsFocusedJKScrollsBody (spec §5.2): with the column
// focused on a details pane, j/k scroll its body and leave main's
// selection untouched; gg/G do nothing to either.
func TestColumn_DetailsFocusedJKScrollsBody(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	ta.startBash("c1", "echo hi")
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: "c1", Output: strings.Join(lines, "\n")}})
	ta.key("esc")
	ta.key("enter") // open the details entry for the bash block
	ta.key("l")     // focus the column

	sel, ok := ta.app.sess.main.list.Selected()
	if !ok {
		t.Fatal("test setup: no main selection")
	}

	ta.key("j")
	ta.key("j")
	ta.key("j")
	if got, want := columnTop(ta.app).body.View(), ""; got == want {
		t.Fatal("test setup: body view is empty")
	}
	if !strings.Contains(xansi.Strip(columnTop(ta.app).body.View()), "line 3") {
		t.Errorf("body after 3×j = %q, want to show from line 3", xansi.Strip(columnTop(ta.app).body.View()))
	}
	if after, ok := ta.app.sess.main.list.Selected(); !ok || after.ID != sel.ID {
		t.Errorf("main selection changed to %+v, want unchanged %+v", after, sel)
	}

	ta.key("k")
	if !strings.Contains(xansi.Strip(columnTop(ta.app).body.View()), "line 2") {
		t.Errorf("body after k = %q, want back to line 2", xansi.Strip(columnTop(ta.app).body.View()))
	}

	ta.key("G")
	if !strings.Contains(xansi.Strip(columnTop(ta.app).body.View()), "line 2") {
		t.Errorf("G on a focused details pane scrolled the body: %q", xansi.Strip(columnTop(ta.app).body.View()))
	}
	if after, ok := ta.app.sess.main.list.Selected(); !ok || after.ID != sel.ID {
		t.Errorf("G changed main selection to %+v, want unchanged %+v", after, sel)
	}
}

// TestColumn_EnterOnSubagentPushesLivePane: enter on a subagent block
// with a spawned child pushes its live transcript pane, focused.
func TestColumn_EnterOnSubagentPushesLivePane(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "**bold** child"})
	ta.key("esc")
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "t/c9" {
		t.Fatalf("selection = %+v, want the subagent block t/c9", sel)
	}
	ta.key("enter")

	top := columnTop(ta.app)
	if top == nil || top.kind != paneTranscript {
		t.Fatalf("top().kind = %v, want paneTranscript", top)
	}
	if top.session != "ses_c" {
		t.Errorf("top().session = %q, want ses_c", top.session)
	}
	if ta.app.view.focus != focusColumn {
		t.Errorf("focus = %v, want focusColumn", ta.app.view.focus)
	}
	view := xansi.Strip(ta.view())
	if !strings.Contains(view, "main › ↳ explore: find the config") {
		t.Errorf("view has no breadcrumb:\n%s", view)
	}
	if strings.Contains(view, "**") || !strings.Contains(view, "bold child") {
		t.Errorf("view = %q, want markdown-rendered %q with no **", view, "bold child")
	}
}

// TestColumn_ChildStreamsPerTick: with the child pane open, deltas mark
// it dirty but don't upsert until fire(); only the load's Messages call
// is ever made for ses_c.
func TestColumn_ChildStreamsPerTick(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	// Settle the parent's subagent block (else its live spinner also
	// upserts main on every tick, which would conflate the count this
	// test cares about — the child pane's).
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: "c9", Name: "task", Output: "done"}})
	ta.key("esc")
	ta.key("enter")

	before := ta.app.w.upserts
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "one"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: " two"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: " three"})
	if n := ta.app.w.upserts - before; n != 0 {
		t.Fatalf("upserts before fire = %d, want 0", n)
	}
	ta.fire()
	if n := ta.app.w.upserts - before; n != 1 {
		t.Fatalf("upserts after fire = %d, want exactly 1", n)
	}
	if got := xansi.Strip(columnTop(ta.app).list.View()); !strings.Contains(got, "one two three") {
		t.Errorf("child list view = %q, want the streamed text", got)
	}
	calls := ta.sessions.msgCalls["ses_c"]
	if calls != 1 {
		t.Errorf("Messages(ses_c) called %d times, want exactly 1 (the load on spawn)", calls)
	}
}

// TestColumn_ChildKeepsScroll: scrolling up in the column, then a new
// child delta plus a tick, leaves the first visible row unchanged.
func TestColumn_ChildKeepsScroll(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: strings.Join(lines, "\n")})
	ta.key("esc")
	ta.key("enter")

	ta.key("ctrl+u")
	ta.key("k")
	before := columnTop(ta.app).list.View()

	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "more"})
	ta.fire()
	after := columnTop(ta.app).list.View()
	firstLine := func(s string) string {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			i = len(s)
		}
		line := xansi.Strip(s[:i])
		if j := strings.Index(line, "line 0"); j >= 0 {
			return strings.TrimSpace(line[j:])
		}
		return strings.TrimSpace(line)
	}
	if firstLine(after) != firstLine(before) {
		t.Errorf("first visible row changed:\nbefore %q\nafter  %q", firstLine(before), firstLine(after))
	}
}

// TestColumn_TabHLFocus: tab/h/l move focus between main and the column
// while it is open; with the column closed, tab still cycles the agent.
func TestColumn_TabHLFocus(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.key("esc")
	ta.key("enter")
	if ta.app.view.focus != focusColumn {
		t.Fatalf("focus after enter = %v, want focusColumn", ta.app.view.focus)
	}
	ta.key("tab")
	if ta.app.view.focus != focusMain {
		t.Fatalf("focus after tab = %v, want focusMain", ta.app.view.focus)
	}
	ta.key("tab")
	if ta.app.view.focus != focusColumn {
		t.Fatalf("focus after 2nd tab = %v, want focusColumn", ta.app.view.focus)
	}
	ta.key("h")
	if ta.app.view.focus != focusMain {
		t.Fatalf("focus after h = %v, want focusMain", ta.app.view.focus)
	}
	ta.key("l")
	if ta.app.view.focus != focusColumn {
		t.Fatalf("focus after l = %v, want focusColumn", ta.app.view.focus)
	}

	ta.key("esc") // pop; the column is now empty, focus back to main
	before := ta.app.sess.info.Agent
	ta.key("tab")
	if ta.app.sess.info.Agent == before {
		t.Errorf("tab with the column closed did not cycle the agent (still %q)", before)
	}
}

// TestColumn_NestedDrill: a grandchild spawned inside ses_c can be
// drilled into from the child's own pane, deepening the column.
func TestColumn_NestedDrill(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.ToolCallStarted{Base: childBase(), MessageID: "k1",
		Call: core.ToolCall{ID: "c2", Name: "task", Input: []byte(`{"agent":"general","description":"deeper"}`)}})
	ta.event(event.SubagentSpawned{Base: childBase(), Child: "ses_g", Agent: "general", Description: "deeper", CallID: "c2"})
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	if sel, ok := columnTop(ta.app).list.Selected(); !ok || sel.ID != "t/c2" {
		t.Fatalf("child selection = %+v, want t/c2", sel)
	}
	ta.key("enter") // child -> grandchild pane

	if n := len(ta.app.view.col); n != 2 {
		t.Fatalf("len(col) = %d, want 2", n)
	}
	if got := xansi.Strip(ta.view()); !strings.Contains(got, "main › ↳ explore: find the config › ↳ general: deeper") {
		t.Errorf("breadcrumb missing from view:\n%s", got)
	}

	ta.key("esc")
	if n := len(ta.app.view.col); n != 1 {
		t.Fatalf("len(col) after esc = %d, want 1", n)
	}
	if ta.app.view.focus != focusColumn {
		t.Errorf("focus after esc = %v, want focusColumn (still one entry)", ta.app.view.focus)
	}

	ta.key("esc")
	if n := len(ta.app.view.col); n != 0 {
		t.Fatalf("len(col) after 2nd esc = %d, want 0", n)
	}
	if ta.app.view.focus != focusMain {
		t.Errorf("focus after 2nd esc = %v, want focusMain", ta.app.view.focus)
	}
}

// TestColumn_EnterChildToolPushesDetails: selecting a plain tool block in
// the child pane and pressing enter pushes a details entry; esc returns
// to the child pane with its selection intact.
func TestColumn_EnterChildToolPushesDetails(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.ToolCallStarted{Base: childBase(), MessageID: "k1",
		Call: core.ToolCall{ID: "k1", Name: "bash", Input: []byte(`{"command":"ls"}`)}})
	ta.key("esc")
	ta.key("enter") // main -> child pane

	childPane := columnTop(ta.app)
	if !childPane.list.Select("t/k1") {
		t.Fatal("test setup: could not select the bash block")
	}
	ta.key("enter") // child -> details

	top := columnTop(ta.app)
	if top.kind != paneDetails {
		t.Fatalf("top().kind = %v, want paneDetails", top.kind)
	}
	if got := xansi.Strip(ta.view()); !strings.Contains(got, "bash · ls") {
		t.Errorf("breadcrumb = %q, want it to end \"bash · ls\"", got)
	}
	if ta.app.view.focus != focusColumn {
		t.Errorf("focus = %v, want focusColumn (pushed from inside the column)", ta.app.view.focus)
	}

	ta.key("esc")
	if columnTop(ta.app) != childPane {
		t.Fatalf("top() after esc = %v, want back to the child pane", columnTop(ta.app))
	}
	if sel, ok := childPane.list.Selected(); !ok || sel.ID != "t/k1" {
		t.Errorf("child selection after esc = %+v, want t/k1 kept", sel)
	}
}

// TestColumn_FocusedPaneYankSearch: y and / act on the focused pane,
// leaving main untouched.
func TestColumn_FocusedPaneYankSearch(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "child text"})
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	childPane := columnTop(ta.app)
	if !childPane.list.Select("m/k1/0") {
		t.Fatal("test setup: could not select the child text block")
	}
	_, cmd := ta.app.Update(keyPress("y"))
	if cmd == nil {
		t.Fatal("y produced no Cmd")
	}
	msg := cmd()
	var clipMsg tea.Msg
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if m := c(); fmt.Sprintf("%T", m) == "tea.setClipboardMsg" {
				clipMsg = m
			}
		}
	} else if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
		clipMsg = msg
	}
	if clipMsg == nil {
		t.Fatalf("no clipboard Cmd from y: %#v", msg)
	}
	if got := fmt.Sprint(clipMsg); !strings.Contains(got, "child text") {
		t.Errorf("clipboard = %q, want the child block's text", got)
	}

	ta.key("/")
	ta.typeText("child")
	ta.key("enter")
	if got := columnTop(ta.app).list.View(); !strings.Contains(xansi.Strip(got), "child text") {
		t.Errorf("child list has no visible text after search: %q", xansi.Strip(got))
	}
	// main's search must not have been touched.
	if n := ta.app.sess.main.list.SetSearch(""); n != 0 {
		t.Errorf("main had a search query applied")
	}
}

// TestColumn_SubagentWithoutChildShowsHeaderOnly: enter on a subagent
// block with no spawned child yet opens a header-only details pane, and
// makes no Messages call.
func TestColumn_SubagentWithoutChildShowsHeaderOnly(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	call := core.ToolCall{ID: "c9", Name: "task", Input: []byte(`{"agent":"explore","description":"find the config"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.key("esc")
	ta.key("enter")

	top := columnTop(ta.app)
	if top == nil || top.kind != paneDetails {
		t.Fatalf("top().kind = %v, want paneDetails", top)
	}
	if got := xansi.Strip(ta.view()); !strings.Contains(got, "subagent · explore · find the config") {
		t.Errorf("breadcrumb = %q, want it to end \"subagent · explore · find the config\"", got)
	}
	if n := len(ta.app.sess.kids); n != 0 {
		t.Errorf("len(kids) = %d, want 0 (no Messages call for a childless subagent)", n)
	}
}

// TestColumn_DetailsFocusedYSlashGpDoNothing: while the column is
// focused on a details pane, y, /, and gp do nothing to main.
func TestColumn_DetailsFocusedYSlashGpDoNothing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "one")
	ta.startBash("c2", "two")
	ta.request("p1", "c1", "one")
	ta.key("esc")
	ta.key("enter") // open the details pane for the selected block
	ta.key("l")     // focus the column

	sel, ok := ta.app.sess.main.list.Selected()
	if !ok {
		t.Fatal("test setup: no main selection")
	}
	before := columnTop(ta.app).body.View()

	_, cmd := ta.app.Update(keyPress("y"))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
				t.Error("y copied something while a details pane is focused")
			}
		}
	}
	ta.key("/")
	if ta.app.view.searching {
		t.Error("/ opened search while a details pane is focused")
	}
	ta.key("g")
	ta.key("p")
	if after, ok := ta.app.sess.main.list.Selected(); !ok || after.ID != sel.ID {
		t.Errorf("gp changed main selection to %+v, want unchanged %+v", after, sel)
	}
	if got := columnTop(ta.app).body.View(); got != before {
		t.Errorf("the details pane's body changed: %q", got)
	}
}

// TestColumn_ChildGroupsFold: three consecutive child read calls show
// as one "explored" header in the column, o expands it, and main is
// unaffected.
func TestColumn_ChildGroupsFold(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	read := func(id, path string) {
		ta.event(event.ToolCallStarted{Base: childBase(), MessageID: "k1",
			Call: core.ToolCall{ID: id, Name: "read", Input: json.RawMessage(fmt.Sprintf(`{"path":%q}`, path))}})
		ta.event(event.ToolCallFinished{Base: childBase(), MessageID: "k1",
			Result: core.ToolResult{CallID: id, Name: "read", Output: "1: package a"}})
	}
	read("r1", "a.go")
	read("r2", "b.go")
	read("r3", "c.go")
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	child := columnTop(ta.app)
	view := xansi.Strip(child.list.View())
	if !strings.Contains(view, "explored") {
		t.Fatalf("child list view = %q, want an \"explored\" header", view)
	}
	if strings.Contains(view, "a.go") {
		t.Errorf("child view shows a member's detail before expanding: %q", view)
	}
	if !child.list.Select("g/r1") {
		t.Fatal("test setup: could not select the child's group header")
	}
	ta.key("o")
	view = xansi.Strip(columnTop(ta.app).list.View())
	if !strings.Contains(view, "a.go") {
		t.Errorf("child view after o = %q, want the expanded member's path", view)
	}
	if got := xansi.Strip(ta.app.sess.main.list.View()); strings.Contains(got, "explored") {
		t.Errorf("main's list shows an \"explored\" header, want none: %q", got)
	}
}

// TestMouse_SelectCopyInSubagentPane: dragging across two child blocks
// in the column copies their text.
// TestMouse_SubagentPaneDragPastEdgeClamps: a drag past a subagent pane's
// right edge clamps to the pane's last content column, like main's
// transcript, rather than pinning to a HitTest-rejected cell (which would
// silently stop the selection from extending).
func TestMouse_SubagentPaneDragPastEdgeClamps(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(160, 30))
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "first child line"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "second child line"})
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	side := ta.app.lay.Side
	startY := side.Y + columnHeaderRows
	endY := startY + 2

	ta.mouse(tea.MouseClickMsg{X: side.X + 1, Y: startY, Button: tea.MouseLeft})
	// Drag far past the column's right edge, on the second block's row.
	ta.mouse(tea.MouseMotionMsg{X: side.X + side.W + 5, Y: endY, Button: tea.MouseLeft})
	_, cmd := ta.app.Update(tea.MouseReleaseMsg{X: side.X + side.W + 5, Y: endY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("release produced no Cmd")
	}
	msg := cmd()
	var clipMsg tea.Msg
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if m := c(); fmt.Sprintf("%T", m) == "tea.setClipboardMsg" {
				clipMsg = m
			}
		}
	} else if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
		clipMsg = msg
	}
	if clipMsg == nil {
		t.Fatalf("no clipboard Cmd from the drag: %#v", msg)
	}
	got := fmt.Sprint(clipMsg)
	if !strings.Contains(got, "second child line") {
		t.Errorf("copied text = %q, want it to include the second line (clamped, not stopped)", got)
	}
}

// TestMouse_SubagentPaneDragPastLeftEdgeStaysInColumn: the mirror case,
// dragging past the column's left edge into main's region — the drag
// stays pinned to the column and still extends.
func TestMouse_SubagentPaneDragPastLeftEdgeStaysInColumn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(160, 30))
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "first child line"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "second child line"})
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	side := ta.app.lay.Side
	startY := side.Y + columnHeaderRows
	endY := startY + 2

	ta.mouse(tea.MouseClickMsg{X: side.X + 1, Y: startY, Button: tea.MouseLeft})
	// Drag far past the column's left edge, into main's region.
	ta.mouse(tea.MouseMotionMsg{X: side.X - 20, Y: endY, Button: tea.MouseLeft})

	sel := ta.app.view.mouse.sel
	if !sel.Active {
		t.Fatal("motion past the left edge did not extend the selection")
	}
	if a := ta.app.view.mouse.pane; a != regionDetails {
		t.Errorf("pane = %v, want regionDetails (pinned to the column)", a)
	}
}

// TestMouse_ColumnPoppedMidDrag: popping the column mid-drag (esc) leaves
// motion and release as no-ops, with no panic and nothing copied from the
// now-gone pane.
func TestMouse_ColumnPoppedMidDrag(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(160, 30))
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "first child line"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "second child line"})
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	side := ta.app.lay.Side
	startY := side.Y + columnHeaderRows
	ta.mouse(tea.MouseClickMsg{X: side.X + 1, Y: startY, Button: tea.MouseLeft})

	ta.key("esc") // pop the column mid-drag

	ta.mouse(tea.MouseMotionMsg{X: side.X + 5, Y: startY + 2, Button: tea.MouseLeft})
	_, cmd := ta.app.Update(tea.MouseReleaseMsg{X: side.X + 5, Y: startY + 2, Button: tea.MouseLeft})
	if cmd != nil {
		msg := cmd()
		got := fmt.Sprint(msg)
		if strings.Contains(got, "child line") {
			t.Errorf("release after column pop copied %q, want nothing from the popped pane", got)
		}
	}
}

func TestMouse_SelectCopyInSubagentPane(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(160, 30))
	ta.sendAndAdopt("find it")
	ta.startSubagent()
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k1", Text: "first child line"})
	ta.event(event.TextDelta{Base: childBase(), MessageID: "k2", Text: "second child line"})
	ta.key("esc")
	ta.key("enter") // main -> child pane, focused

	side := ta.app.lay.Side
	ids := paneIDs(columnTop(ta.app))
	if len(ids) < 2 {
		t.Fatalf("test setup: child pane has %d blocks, want >= 2", len(ids))
	}
	startY := side.Y + columnHeaderRows
	endY := startY + 2
	ta.mouse(tea.MouseClickMsg{X: side.X + 1, Y: startY, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: side.X + 30, Y: endY, Button: tea.MouseLeft})
	_, cmd := ta.app.Update(tea.MouseReleaseMsg{X: side.X + 30, Y: endY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("release produced no Cmd")
	}
	msg := cmd()
	var clipMsg tea.Msg
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if m := c(); fmt.Sprintf("%T", m) == "tea.setClipboardMsg" {
				clipMsg = m
			}
		}
	} else if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
		clipMsg = msg
	}
	if clipMsg == nil {
		t.Fatalf("no clipboard Cmd from the drag: %#v", msg)
	}
	got := fmt.Sprint(clipMsg)
	if !strings.Contains(got, "first child line") || !strings.Contains(got, "second child line") {
		t.Errorf("clipboard = %q, want both child lines", got)
	}
}
