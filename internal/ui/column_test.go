package ui

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

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
