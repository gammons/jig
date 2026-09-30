package ui

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/coderender"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/transcript"
)

// twoTextMessages returns a resumable history with two assistant text
// blocks ("m/a1/0" first, "m/a2/0" last), for navigation tests.
func twoTextMessages() []core.Message {
	return []core.Message{
		{ID: "a1", SessionID: "ses_1", Role: core.RoleAssistant, Parts: []core.Part{{Kind: core.PartText, Text: "first message"}}},
		{ID: "a2", SessionID: "ses_1", Role: core.RoleAssistant, Parts: []core.Part{{Kind: core.PartText, Text: "second message"}}},
	}
}

// detailsGoldenMessages returns a small, deterministic transcript (a user
// prompt, an assistant explanation, and a finished bash call) for the
// details-split goldens.
func detailsGoldenMessages() []core.Message {
	return []core.Message{
		{ID: "u1", SessionID: "ses_1", Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "Explain the build"}}},
		{
			ID: "a1", SessionID: "ses_1", Role: core.RoleAssistant, Model: "anthropic/claude-sonnet-5",
			Parts: []core.Part{
				{Kind: core.PartText, Text: "The build runs **make check**:\n\n- `go test`\n- `golangci-lint`"},
				{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"make check"}`)}},
				{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c1", Output: "ok"}},
			},
		},
	}
}

func TestNormal_GGAndG(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	if n := ta.app.sess.main.list.Len(); n != 2 {
		t.Fatalf("list has %d items, want 2", n)
	}
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a2/0" {
		t.Fatalf("initial selection = %+v, want the last block", sel)
	}

	ta.key("g")
	if ta.app.view.keyPrefix != "g" {
		t.Fatal("g did not set the key prefix")
	}
	ta.key("g")
	if ta.app.view.keyPrefix != "" {
		t.Error("gg did not clear the key prefix")
	}
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a1/0" {
		t.Errorf("selection after gg = %+v, want the first block", sel)
	}

	// A bare "g" followed by an unrelated key is swallowed, resetting the
	// prefix without moving the selection.
	ta.key("G")
	ta.key("g")
	ta.key("x")
	if ta.app.view.keyPrefix != "" {
		t.Error("prefix not cleared after an unknown g-command")
	}
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a2/0" {
		t.Errorf("selection = %+v, want unchanged (last block, from G)", sel)
	}
}

func TestNormal_DetailsToggleAndFollowsSelection(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")

	ta.key("enter")
	if !columnOpen(ta.app) {
		t.Fatal("enter did not open the details split")
	}
	if got := xansi.Strip(columnTop(ta.app).body.View()); !strings.Contains(got, "second message") {
		t.Errorf("details = %q, want the selected (last) block's text", got)
	}

	ta.key("k")
	got := xansi.Strip(columnTop(ta.app).body.View())
	if !strings.Contains(got, "first message") || strings.Contains(got, "second message") {
		t.Errorf("details after k = %q, want the new selection's text only", got)
	}

	ta.key("enter")
	if columnOpen(ta.app) {
		t.Error("enter on an open split did not close it")
	}
}

func TestNormal_StaleDetailsMsgIgnored(t *testing.T) {
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
	// Open details directly (bypassing the harness's auto-delivery) so the
	// async Cmd can be resolved after the selection has moved on.
	_, cmd := ta.app.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("want an async Cmd for the edit's file context")
	}

	ta.key("k") // move to the user block; details rebuild for it synchronously
	before := columnTop(ta.app).body.View()
	if !strings.Contains(xansi.Strip(before), "go") {
		t.Fatalf("details after k = %q, want the user block's text", before)
	}

	ta.send(cmd()) // deliver the stale detailsMsg for the (no longer selected) tool block
	if got := columnTop(ta.app).body.View(); got != before {
		t.Errorf("a stale detailsMsg changed the details pane: %q", got)
	}
}

func TestNormal_SearchInputAndClear(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("k") // start on the first block
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a1/0" {
		t.Fatalf("selection = %+v, want the first block", sel)
	}

	ta.key("/")
	if !ta.app.view.searching {
		t.Fatal("/ did not open the search input")
	}
	ta.typeText("second")
	ta.key("enter")
	if ta.app.view.searching {
		t.Error("search input still open after enter")
	}

	ta.key("n")
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a2/0" {
		t.Fatalf("selection after n = %+v, want the matching second block", sel)
	}

	ta.key("k") // back to the first block
	ta.key("esc")
	if columnOpen(ta.app) {
		t.Fatal("esc should not have anything to close here")
	}
	ta.key("n")
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a1/0" {
		t.Errorf("selection after clearing the search and n = %+v, want unchanged", sel)
	}
}

func TestNormal_EscClosesDetailsFirst(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("/")
	ta.typeText("second")
	ta.key("enter") // applies the search; NORMAL, not searching

	ta.key("k") // first block, does not match "second"
	ta.key("enter")
	if !columnOpen(ta.app) {
		t.Fatal("want the details split open")
	}

	ta.key("esc")
	if columnOpen(ta.app) {
		t.Fatal("esc did not close the details split first")
	}
	// The search is still applied: n still finds the second block.
	ta.key("n")
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a2/0" {
		t.Fatalf("selection after n = %+v, want the search still applied", sel)
	}

	ta.key("k")
	ta.key("esc") // no split open now: clears the search
	ta.key("n")
	if sel, ok := ta.app.sess.main.list.Selected(); !ok || sel.ID != "m/a1/0" {
		t.Errorf("selection after clearing the search = %+v, want unchanged", sel)
	}
}

func TestNormal_YankTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		b    transcript.Block
		want string
	}{
		{"text", transcript.Block{Kind: transcript.KindText, Text: "hello\nworld"}, "hello\nworld"},
		{"user", transcript.Block{Kind: transcript.KindUser, Text: "hi there"}, "hi there"},
		{"reasoning", transcript.Block{Kind: transcript.KindReasoning, Text: "thinking it through"}, "thinking it through"},
		{
			"bash",
			transcript.Block{Kind: transcript.KindTool, Call: &core.ToolCall{Name: "bash", Input: []byte(`{"command":"echo hi"}`)}},
			"echo hi",
		},
		{
			"multi-line bash keeps its newlines, sanitized",
			transcript.Block{Kind: transcript.KindTool, Call: &core.ToolCall{Name: "bash", Input: []byte(`{"command":"cd src &&\n  make \u001b[31mall\n"}`)}},
			"cd src &&\n  make all\n",
		},
		{
			"read",
			transcript.Block{Kind: transcript.KindTool, Call: &core.ToolCall{Name: "read", Input: []byte(`{"path":"a.go"}`)}},
			"a.go",
		},
		{
			"write",
			transcript.Block{Kind: transcript.KindTool, Call: &core.ToolCall{Name: "write", Input: []byte(`{"path":"b.go"}`)}},
			"b.go",
		},
		{
			"edit",
			transcript.Block{Kind: transcript.KindTool, Call: &core.ToolCall{Name: "edit", Input: []byte(`{"path":"c.go","old_string":"foo","new_string":"bar"}`)}},
			coderender.DiffText("foo", "bar", 3),
		},
		{
			"other tool",
			transcript.Block{Kind: transcript.KindTool, Call: &core.ToolCall{Name: "grep"}, Result: &core.ToolResult{Output: "match: 1"}},
			"match: 1",
		},
		{
			"subagent",
			transcript.Block{Kind: transcript.KindSubagent, Sub: &transcript.Subagent{Description: "explore code"}},
			"explore code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := yankText(tt.b); got != tt.want {
				t.Errorf("yankText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormal_YankCmdAndHint(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")

	cmd := normalKeys{ta.app}.yank()
	if cmd == nil {
		t.Fatal("yank: want a Cmd")
	}
	if name := fmt.Sprintf("%T", cmd()); !strings.Contains(name, "ClipboardMsg") {
		t.Errorf("yank Cmd produced %s, want tea's clipboard message", name)
	}

	ta.key("y")
	if ta.app.view.hint != "yanked" {
		t.Errorf("hint = %q, want yanked", ta.app.view.hint)
	}
}

func TestNormal_GoldenDetailsOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(150, 40), withResume(core.Session{ID: "ses_1", Agent: "build"}, detailsGoldenMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	ta.fire() // the resize debounce: record the settled list width
	golden.Assert(t, "app_details_open", ta.view())
}

func TestNormal_GoldenNarrowDetails(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(100, 30), withResume(core.Session{ID: "ses_1", Agent: "build"}, detailsGoldenMessages(), nil))
	ta.key("esc")
	ta.key("enter")
	ta.fire() // the resize debounce: record the settled list width
	golden.Assert(t, "narrow_details", ta.view())
}
