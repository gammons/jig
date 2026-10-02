package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/transcript"
)

// testFiles are a.go … e.go; b.go and d.go are git-modified.
func testFiles() testOpt {
	return withFiles(
		core.ProjectFile{Path: "a.go"},
		core.ProjectFile{Path: "b.go", Modified: true},
		core.ProjectFile{Path: "c.go"},
		core.ProjectFile{Path: "d.go", Modified: true},
		core.ProjectFile{Path: "e.go"},
	)
}

// toolCall delivers a root tool call name(path) that succeeds.
func (ta *testApp) toolCall(id, name, path string) {
	ta.t.Helper()
	call := core.ToolCall{ID: id, Name: name, Input: fmt.Appendf(nil, `{"path":%q,"content":"x"}`, path)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{CallID: id, Name: name, Output: "ok"}})
}

func itemIDs(items []picker.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestFiles_RankingTouchedModifiedRest(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	ta.sendAndAdopt("go")
	ta.toolCall("c1", "read", "c.go")
	ta.toolCall("c2", "write", testWorkDir+"/e.go")
	ta.toolCall("c3", "read", "./c.go") // c.go touched again, last
	ta.toolCall("c4", "bash", "a.go")   // not a file touch
	got := itemIDs(loadItems(ta, picker.Level{ID: levelFiles}))
	want := []string{"c.go", "e.go", "b.go", "d.go", "a.go"}
	if !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestFiles_UnsafePathsSkipped(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withFiles(core.ProjectFile{Path: "ok.go"}, core.ProjectFile{Path: "bad\x1b[31m.go"}))
	if got := itemIDs(loadItems(ta, picker.Level{ID: levelFiles})); !slices.Equal(got, []string{"ok.go"}) {
		t.Errorf("files = %q, want the unsafe path skipped", got)
	}
}

func TestFiles_AtEscInsertsLiteral(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	ta.typeText("see @")
	if !ta.app.w.picker.IsOpen() || ta.app.mode != modePicker {
		t.Fatalf("@: open=%v mode=%v; want the file picker", ta.app.w.picker.IsOpen(), ta.app.mode)
	}
	ta.key("esc")
	if v := ta.app.w.prompt.Value(); v != "see @" {
		t.Errorf("prompt = %q, want a literal @", v)
	}
	if ta.app.mode != modeInsert {
		t.Errorf("mode = %v, want INSERT", ta.app.mode)
	}
	ta.typeText("x")
	if v := ta.app.w.prompt.Value(); v != "see @x" {
		t.Errorf("prompt = %q, want typing to resume", v)
	}

	// Attach files… from the root, then esc: no @.
	ta.key("ctrl+p")
	ta.typeText("attach")
	ta.key("enter")
	ta.key("esc")
	if v := ta.app.w.prompt.Value(); v != "see @x" {
		t.Errorf("prompt = %q, want no @ from a root-opened files level", v)
	}
}

func TestFiles_ChosenInsertsMentions(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	ta.typeText("see @")
	ta.typeText("e.go")
	ta.key("enter")
	if v := ta.app.w.prompt.Value(); v != "see @e.go " {
		t.Errorf("prompt = %q, want the mention", v)
	}
	ta.key("ctrl+p")
	ta.typeText("attach")
	ta.key("enter")
	ta.key("tab")
	ta.key("down")
	ta.key("tab")
	ta.key("enter")
	if v := ta.app.w.prompt.Value(); v != "see @e.go @b.go @d.go " {
		t.Errorf("prompt = %q, want both marked files", v)
	}
}

func TestSend_AttachmentsOnlyForSurvivingTokens(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	ta.key("@")
	ta.key("tab") // b.go (modified, first)
	ta.key("down")
	ta.key("tab") // d.go
	ta.key("enter")
	if v := ta.app.w.prompt.Value(); v != "@b.go @d.go " {
		t.Fatalf("prompt = %q", v)
	}
	for range len("@d.go ") {
		ta.key("backspace")
	}
	ta.send(tea.PasteMsg{Content: "and @b.goo and look"}) // typing @ would open the picker
	ta.key("enter")
	if len(ta.chat.sends) != 1 {
		t.Fatalf("%d sends, want 1", len(ta.chat.sends))
	}
	s := ta.chat.sends[0]
	if s.Text != "@b.go and @b.goo and look" || !slices.Equal(s.Attachments, []string{"b.go"}) {
		t.Errorf("send = %q %v, want only b.go attached", s.Text, s.Attachments)
	}
	blocks := ta.app.sess.main.proj.Blocks()
	if len(blocks) != 1 || !slices.Equal(blocks[0].Attachments, []string{"b.go"}) {
		t.Errorf("user block attachments = %+v, want b.go", blocks)
	}
}

func TestSend_AttachmentsClearedAfterSend(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	ta.key("@")
	ta.key("tab") // b.go
	ta.key("enter")
	if v := ta.app.w.prompt.Value(); v != "@b.go " {
		t.Fatalf("prompt = %q, want the mention", v)
	}
	ta.sendAndAdopt("") // sends the prefilled "@b.go "
	ta.event(event.RunFinished{Base: rootBase(), MessageID: "m1"})
	ta.returnSend()
	if len(ta.chat.sends) != 1 || !slices.Equal(ta.chat.sends[0].Attachments, []string{"b.go"}) {
		t.Fatalf("first send = %+v, want b.go attached", ta.chat.sends[0])
	}

	// A later message that happens to still contain the "@b.go" token
	// must not re-attach it: a picked path is consumed by the send it
	// was picked for, not remembered indefinitely.
	ta.send(tea.PasteMsg{Content: "see @b.go again"})
	ta.key("enter")
	if len(ta.chat.sends) != 2 {
		t.Fatalf("%d sends, want 2", len(ta.chat.sends))
	}
	if got := ta.chat.sends[1].Attachments; got != nil {
		t.Errorf("second send attachments = %v, want none", got)
	}
	var users []transcript.Block
	for _, b := range ta.app.sess.main.proj.Blocks() {
		if b.Kind == transcript.KindUser {
			users = append(users, b)
		}
	}
	if len(users) != 2 || users[1].Attachments != nil {
		t.Errorf("user blocks = %+v, want the second with no attachments", users)
	}
}

func TestPicker_GoldenFiles(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	ta.key("@")
	golden.Assert(t, "picker_files", ta.view())
}

// TestFiles_ChosenMentionHighlighted: a file chosen in the picker shows in
// the prompt in the theme's mention color; once sent, the next prompt's
// matching text is plain again.
func TestFiles_ChosenMentionHighlighted(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, testFiles())
	on := xansi.Style{}.ForegroundColor(ta.app.theme.set.Prompt.Mention.GetForeground()).String()
	ta.typeText("see @")
	ta.typeText("e.go")
	ta.key("enter")
	if !strings.Contains(ta.view(), on+"@e.go") {
		t.Fatalf("the chosen mention is not highlighted:\n%q", ta.view())
	}
	ta.key("enter") // send: the attachment is consumed
	ta.send(tea.PasteMsg{Content: "@e.go again"})
	if strings.Contains(ta.view(), on+"@e.go") {
		t.Errorf("a sent mention stays highlighted in the next prompt")
	}
}
