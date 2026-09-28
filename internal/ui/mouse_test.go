package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// twentyTextMessages returns a resumable history of 20 one-line assistant
// text blocks, for wheel-scrolling tests.
func twentyTextMessages() []core.Message {
	msgs := make([]core.Message, 20)
	for i := range 20 {
		msgs[i] = core.Message{
			ID: core.MessageID(fmt.Sprintf("a%d", i+1)), SessionID: "ses_1", Role: core.RoleAssistant,
			Parts: []core.Part{{Kind: core.PartText, Text: fmt.Sprintf("message %d", i+1)}},
		}
	}
	return msgs
}

func TestMouse_ViewEnablesCellMotion(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	if got := ta.app.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want MouseModeCellMotion", got)
	}
}

func TestMouse_WheelScrollsTranscriptAndMovesHighlight(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(120, 30), withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))

	// Independently compute what ScrollBy(-wheelLines) does to a copy of
	// the same list, to check the wiring without duplicating blocklist's
	// own tested behavior.
	before := ta.app.w.list
	before.ScrollBy(-wheelLines)
	wantSel, ok := before.Selected()
	if !ok {
		t.Fatal("test setup: no selection after ScrollBy")
	}
	beforeLine := strings.Split(ta.app.w.list.View(), "\n")[0]

	ta.mouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})

	afterLine := strings.Split(ta.app.w.list.View(), "\n")[0]
	if afterLine == beforeLine {
		t.Error("wheel up did not change the transcript's first line")
	}
	gotSel, ok := ta.app.w.list.Selected()
	if !ok || gotSel.ID != wantSel.ID {
		t.Errorf("selection after wheel = %+v, want %+v", gotSel, wantSel)
	}
}

func TestMouse_WheelInInsertKeepsDraft(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))
	ta.typeText("half a thought")
	ta.mouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	ta.mouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	if ta.app.mode != modeInsert {
		t.Errorf("mode = %v, want INSERT", ta.app.mode)
	}
	if v := ta.app.w.prompt.Value(); v != "half a thought" {
		t.Errorf("prompt = %q, want unchanged", v)
	}
}

func TestMouse_WheelIgnoredOverPromptAndSidebar(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(150, 40), withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))
	if !ta.app.lay.SideVisible {
		t.Fatal("test setup: sidebar not visible at 150 cols")
	}
	before := ta.view()

	ta.mouse(tea.MouseWheelMsg{X: ta.app.lay.Prompt.X + 2, Y: ta.app.lay.Prompt.Y + 1, Button: tea.MouseWheelUp})
	if got := ta.view(); got != before {
		t.Error("wheel over the prompt changed the frame")
	}

	ta.mouse(tea.MouseWheelMsg{X: ta.app.lay.Side.X + 2, Y: ta.app.lay.Side.Y + 2, Button: tea.MouseWheelUp})
	if got := ta.view(); got != before {
		t.Error("wheel over the sidebar changed the frame")
	}
}

func TestMouse_WheelScrollsDetails(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(150, 40))
	ta.sendAndAdopt("go")
	var rows []string
	for i := range 60 {
		rows = append(rows, fmt.Sprintf("row%02d", i))
	}
	call := core.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"cat file"}`)}
	ta.event(event.ToolCallStarted{Base: rootBase(), MessageID: "m1", Call: call})
	ta.event(event.ToolCallFinished{Base: rootBase(), MessageID: "m1", Result: core.ToolResult{
		CallID: "c1", Name: "bash", Output: strings.Join(rows, "\n"),
	}})
	ta.key("esc")
	ta.key("enter")
	if !ta.app.view.detailsOpen {
		t.Fatal("enter did not open the details split")
	}

	before := ta.app.w.details
	before.ScrollBy(wheelLines)
	wantRow := strings.Split(before.View(), "\n")[2]

	ta.mouse(tea.MouseWheelMsg{X: ta.app.lay.Side.X + 2, Y: ta.app.lay.Side.Y + 2, Button: tea.MouseWheelDown})

	gotRow := strings.Split(ta.app.w.details.View(), "\n")[2]
	if gotRow != wantRow {
		t.Errorf("details body row after wheel down = %q, want %q", gotRow, wantRow)
	}
}

func TestMouse_IgnoredWhilePickerOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))
	ta.key("ctrl+p")
	if ta.app.mode != modePicker {
		t.Fatal("ctrl+p did not open the picker")
	}
	before := ta.view()
	ta.mouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	if got := ta.view(); got != before {
		t.Error("wheel while the picker is open changed the frame")
	}
}
