package ui

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
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

func TestMouse_DragCopiesTextAndHints(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")

	r := ta.app.lay.Transcript
	// Row 0 is block "m/a1/0"'s only line ("first message"); x=1 is its
	// first content column (x=0 is the selection prefix). Columns 1..7
	// give "irst m" (6 chars; trailing whitespace is trimmed per line, so
	// starting at column 0 would trim "first " down to 5).
	ta.mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 8, Y: r.Y, Button: tea.MouseLeft})
	ta.mouse(tea.MouseReleaseMsg{X: r.X + 8, Y: r.Y, Button: tea.MouseLeft})

	if got, want := ta.app.view.hint, "copied 6 chars"; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
	view := xansi.Strip(ta.view())
	styled := ta.view()
	if !strings.Contains(styled, ta.app.theme.set.Selection.On) {
		t.Errorf("frame has no selection On escape:\n%s", view)
	}
}

func TestMouse_DragAcrossBlocksCopiesInOrder(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")

	r := ta.app.lay.Transcript
	// Row 2 is block 2's line ("second message"); row 0 is block 1's
	// ("first message"). Press low, drag up: the copied text is still in
	// document order (block 1 then block 2), not press-to-release order.
	ta.mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y + 2, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
	cmd := mouseCtl{ta.app}.release(tea.MouseReleaseMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("release: want a Cmd copying the selection")
	}
	msg := cmd()
	if name := fmt.Sprintf("%T", msg); !strings.Contains(name, "ClipboardMsg") {
		t.Fatalf("release Cmd produced %s, want tea's clipboard message", name)
	}
	got := fmt.Sprint(msg)
	want := "irst message\ns"
	if got != want {
		t.Errorf("copied text = %q, want %q", got, want)
	}
}

func TestMouse_DragPinnedToStartPane(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(150, 40), withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))
	if !ta.app.lay.SideVisible {
		t.Fatal("test setup: sidebar not visible at 150 cols")
	}
	ta.key("esc")
	r := ta.app.lay.Transcript

	ta.mouse(tea.MouseClickMsg{X: r.X + 1, Y: r.Y + 2, Button: tea.MouseLeft})
	// Move past the transcript's right edge (into the sidebar) and past
	// its bottom edge (into the prompt row).
	ta.mouse(tea.MouseMotionMsg{X: r.X + r.W + 20, Y: r.Y + r.H + 5, Button: tea.MouseLeft})

	sel := ta.app.view.mouse.sel
	if !sel.Active {
		t.Fatal("motion outside the pane did not extend the selection")
	}
	wantID, wantLine, wantCol, ok := blocklist.HitTest(ta.app.w.list, r.W-2, r.H-1)
	if !ok {
		t.Fatal("test setup: pinned cell HitTest failed")
	}
	if sel.End.ID != wantID || sel.End.Line != wantLine || sel.End.Col != wantCol {
		t.Errorf("End = %+v, want {%q %d %d} (pinned to the transcript's last cell)", sel.End, wantID, wantLine, wantCol)
	}
}

func TestMouse_ClickSelectsBlock(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))
	ta.key("esc")
	if !ta.app.w.list.Select("m/a1/0") {
		t.Fatal("test setup: block 1 not found")
	}
	if !ta.app.w.list.Select("m/a3/0") {
		t.Fatal("test setup: block 3 not found")
	}

	r := ta.app.lay.Transcript
	id1, line1, _, ok := blocklist.HitTest(ta.app.w.list, 1, r.Y)
	if !ok || id1 != "m/a1/0" {
		t.Fatalf("test setup: row 0 is %q line %d, want block 1", id1, line1)
	}

	ta.mouse(tea.MouseClickMsg{X: r.X + 1, Y: r.Y, Button: tea.MouseLeft})
	ta.mouse(tea.MouseReleaseMsg{X: r.X + 1, Y: r.Y, Button: tea.MouseLeft})

	got, ok := ta.app.w.list.Selected()
	if !ok || got.ID != "m/a1/0" {
		t.Errorf("selected = %+v, want block 1", got)
	}
	if ta.app.view.hint != "" {
		t.Errorf("hint = %q, want none (a click copies nothing)", ta.app.view.hint)
	}
}

func TestMouse_ClickOnCardDisarms(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.startBash("c1", "make")
	ta.startBash("c2", "ls")
	ta.request("p1", "c1", "make")
	ta.arm()
	if !ta.app.w.card.Armed() {
		t.Fatal("test setup: card not armed")
	}
	ta.app.w.list.Select("t/c2")

	r := ta.app.lay.Transcript
	row := -1
	for y := 0; y < r.H; y++ {
		if id, _, _, ok := blocklist.HitTest(ta.app.w.list, 1, y); ok && id == "t/c1" {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("test setup: t/c1 is not visible in the transcript")
	}

	ta.mouse(tea.MouseClickMsg{X: r.X + 1, Y: r.Y + row, Button: tea.MouseLeft})
	ta.mouse(tea.MouseReleaseMsg{X: r.X + 1, Y: r.Y + row, Button: tea.MouseLeft})

	if got, ok := ta.app.w.list.Selected(); !ok || got.ID != "t/c1" {
		t.Fatalf("selected = %+v, want t/c1", got)
	}
	if ta.app.w.card.Armed() {
		t.Fatal("card still armed right after the click landed on it")
	}
	ta.key("a")
	if len(ta.perms.replies) != 0 {
		t.Fatalf("a right after the click replied: %+v", ta.perms.replies)
	}
	ta.arm()
	ta.key("a")
	if len(ta.perms.replies) != 1 {
		t.Errorf("replies = %+v, want one reply once armed", ta.perms.replies)
	}
}

func TestMouse_AutoScrollAtEdge(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withSize(120, 30), withResume(core.Session{ID: "ses_1", Agent: "build"}, twentyTextMessages(), nil))
	ta.key("esc")

	r := ta.app.lay.Transcript
	if r.H < 8 {
		t.Fatalf("test setup: transcript too short (%d rows)", r.H)
	}
	before := xansi.Strip(ta.app.w.list.View())

	// Press mid-transcript, then drag onto row 0 (the top edge).
	ta.mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y + 5, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})

	ticks := deferredOf[mouseScrollMsg](ta)
	if len(ticks) != 1 {
		t.Fatalf("auto-scroll ticks scheduled = %d, want 1", len(ticks))
	}
	if got, want := ticks[0].d, autoScrollEvery; got != want {
		t.Errorf("tick delay = %v, want %v", got, want)
	}
	selBefore := ta.app.view.mouse.sel

	// Each tick scrolls up one line and extends the range. (Items are one
	// line with a one-line gap between them, so a single tick's End can
	// land on the same block via the gap-row snap; two ticks always
	// cover a full item and its gap, and so always move it.)
	ta.clk.Advance(autoScrollEvery)
	ta.fire()
	ta.clk.Advance(autoScrollEvery)
	ta.fire()

	after := xansi.Strip(ta.app.w.list.View())
	if after == before {
		t.Error("auto-scroll ticks did not scroll the transcript")
	}
	selAfter := ta.app.view.mouse.sel
	if selAfter.End == selBefore.End {
		t.Errorf("auto-scroll ticks did not extend the range: End unchanged at %+v", selAfter.End)
	}

	// After release, a further fire scrolls nothing.
	ta.mouse(tea.MouseReleaseMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
	before3 := ta.app.w.list.View()
	ta.clk.Advance(autoScrollEvery)
	ta.fire()
	after3 := ta.app.w.list.View()
	if after3 != before3 {
		t.Error("auto-scroll continued after release")
	}
}

func TestMouse_EscClearsSelection(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")

	r := ta.app.lay.Transcript
	drag := func() {
		ta.mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
		ta.mouse(tea.MouseMotionMsg{X: r.X + 6, Y: r.Y, Button: tea.MouseLeft})
		ta.mouse(tea.MouseReleaseMsg{X: r.X + 6, Y: r.Y, Button: tea.MouseLeft})
	}

	drag()
	if !ta.app.view.mouse.sel.Active {
		t.Fatal("test setup: no active selection after the drag")
	}
	if !strings.Contains(ta.view(), ta.app.theme.set.Selection.On) {
		t.Fatal("test setup: selection not painted")
	}

	ta.key("esc")
	if ta.app.view.mouse.sel.Active {
		t.Error("esc in NORMAL did not clear the selection")
	}
	if strings.Contains(ta.view(), ta.app.theme.set.Selection.On) {
		t.Error("frame still shows the selection escape after esc")
	}
	if ta.app.mode != modeNormal {
		t.Errorf("mode after esc with no selection left = %v, want unchanged NORMAL", ta.app.mode)
	}

	// esc in INSERT clears the selection and still switches to NORMAL.
	drag()
	ta.key("i")
	if ta.app.mode != modeInsert {
		t.Fatal("test setup: i did not switch to INSERT")
	}
	if !ta.app.view.mouse.sel.Active {
		t.Fatal("test setup: switching to INSERT cleared the selection")
	}
	ta.key("esc")
	if ta.app.view.mouse.sel.Active {
		t.Error("esc in INSERT did not clear the selection")
	}
	if ta.app.mode != modeNormal {
		t.Error("esc in INSERT did not still switch to NORMAL")
	}
}

func TestMouse_PickerCancelsDrag(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")

	r := ta.app.lay.Transcript
	ta.mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 6, Y: r.Y, Button: tea.MouseLeft})
	if !ta.app.view.mouse.sel.Active {
		t.Fatal("test setup: no active selection before opening the picker")
	}

	ta.key("ctrl+p")
	if ta.app.mode != modePicker {
		t.Fatal("ctrl+p did not open the picker")
	}
	if ta.app.view.mouse.sel.Active {
		t.Error("opening the picker did not clear the selection")
	}

	ta.mouse(tea.MouseReleaseMsg{X: r.X + 6, Y: r.Y, Button: tea.MouseLeft})
	if ta.app.view.hint != "" {
		t.Errorf("hint = %q, want none: a release after the picker cancelled the drag copies nothing", ta.app.view.hint)
	}
}

func TestMouse_DetailsSelection(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t, withResume(core.Session{ID: "ses_1", Agent: "build"}, twoTextMessages(), nil))
	ta.key("esc")
	ta.key("enter") // opens details for the selected (last) block: "second message"
	if !ta.app.view.detailsOpen {
		t.Fatal("enter did not open the details split")
	}
	got := xansi.Strip(ta.app.w.details.View())
	if !strings.Contains(got, "second message") {
		t.Fatalf("details = %q, want the selected block's text", got)
	}
	if line, _, ok := ta.app.w.details.HitTest(0, 2); !ok || line != 0 {
		t.Fatalf("test setup: HitTest(0,2) = line %d ok %v, want line 0", line, ok)
	}

	r := ta.app.lay.Side
	ta.mouse(tea.MouseClickMsg{X: r.X, Y: r.Y + 2, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 6, Y: r.Y + 2, Button: tea.MouseLeft})
	ta.mouse(tea.MouseReleaseMsg{X: r.X + 6, Y: r.Y + 2, Button: tea.MouseLeft})

	if got, want := ta.app.view.hint, "copied 6 chars"; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
	if !ta.app.view.mouse.sel.Active {
		t.Fatal("test setup: selection not left active after release")
	}

	// Moving the selection to another block rebuilds the details
	// (SetContent), clearing the selection.
	ta.key("k")
	if ta.app.view.mouse.sel.Active {
		t.Error("opening another block's details did not clear the selection")
	}
}

func TestMouse_SelectionSurvivesStreaming(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.sendAndAdopt("go")
	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: "hello world"})
	ta.key("esc")

	r := ta.app.lay.Transcript
	if r.H < 4 {
		t.Fatalf("test setup: transcript too short (%d rows)", r.H)
	}
	// Row 2 (not an edge row) maps back to the only block's only line
	// (HitTest snaps a row past its content to the block's last line).
	ta.mouse(tea.MouseClickMsg{X: r.X + 1, Y: r.Y + 2, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 5, Y: r.Y + 2, Button: tea.MouseLeft})
	if !ta.app.view.mouse.sel.Active {
		t.Fatal("test setup: no active selection")
	}
	before := ta.app.view.mouse.sel

	ta.event(event.TextDelta{Base: rootBase(), MessageID: "m1", Text: " more"})
	ta.fire()

	after := ta.app.view.mouse.sel
	if before != after {
		t.Errorf("selection changed after streaming: before %+v, after %+v", before, after)
	}
	if !strings.Contains(ta.view(), ta.app.theme.set.Selection.On) {
		t.Error("highlight missing after the streaming delta")
	}
}

func TestMouse_SelectionClearsWhenBlockGone(t *testing.T) {
	t.Parallel()
	sess := core.Session{ID: "ses_1", Agent: "build"}
	ta := newTestApp(t, withResume(sess, twoTextMessages(), nil))
	ta.key("esc")

	r := ta.app.lay.Transcript
	// Row 0 is block "m/a1/0"'s only line ("first message").
	ta.mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y, Button: tea.MouseLeft})
	ta.mouse(tea.MouseMotionMsg{X: r.X + 8, Y: r.Y, Button: tea.MouseLeft})
	ta.mouse(tea.MouseReleaseMsg{X: r.X + 8, Y: r.Y, Button: tea.MouseLeft})

	if !ta.app.view.mouse.sel.Active {
		t.Fatal("test setup: no active selection after the drag")
	}
	if !strings.Contains(ta.view(), ta.app.theme.set.Selection.On) {
		t.Fatal("test setup: selection not painted")
	}

	// A resume (the same path a real re-read of the session takes:
	// onPortResult's resumeMsg case, idle here so it applies at once)
	// whose stored history no longer includes the selected block ("a1"
	// dropped, only "a2" remains) is a Load that drops it from the
	// projection.
	ta.sessions.msgs = twoTextMessages()[1:]
	ta.send(resumeMsg{info: sess, msgs: ta.sessions.msgs})

	if strings.Contains(xansi.Strip(ta.view()), "first message") {
		t.Fatal("test setup: the dropped block is still shown")
	}
	if ta.app.view.mouse.sel.Active {
		t.Error("selection still active after its block's Load dropped it")
	}
	if strings.Contains(ta.view(), ta.app.theme.set.Selection.On) {
		t.Error("frame still shows the selection escape after its block is gone")
	}
}
