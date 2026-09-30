package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// streamChunk is one paragraph of a streamed reply: prose with inline
// styling, a fenced block, and a list, ending in a blank line.
const streamChunk = "Some prose with `code` and **bold** words that wraps across the line.\n\n" +
	"```go\nfunc f() int { return 42 }\n```\n\n- a list item\n\n"

// BenchmarkApp_StreamTick2000 measures one streaming tick (a delta, the
// tick that renders it, and the next View) on a resumed 2,000-block
// session, with the reply already ~16 KB long: a tick's cost must not grow
// with the reply. Each reply is ended at 32 KB and a new one started, so
// the result doesn't depend on how many iterations run.
func BenchmarkApp_StreamTick2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_1", Agent: "build"}, benchHistory(2000), nil))
	_ = ta.view()
	ta.typeText("go")
	ta.key("enter")
	reply, size := 0, 0
	start := func() {
		reply++
		size = 0
		ta.event(event.MessageStarted{Base: rootBase(), MessageID: core.MessageID(fmt.Sprintf("live%d", reply))})
		for size < 16<<10 {
			ta.event(event.TextDelta{Base: rootBase(), MessageID: core.MessageID(fmt.Sprintf("live%d", reply)), Text: streamChunk})
			size += len(streamChunk)
		}
		ta.fire()
		_ = ta.view()
	}
	start()
	b.ReportAllocs()
	for b.Loop() {
		if size >= 32<<10 {
			ta.event(event.StepFinished{Base: rootBase(), MessageID: core.MessageID(fmt.Sprintf("live%d", reply))})
			start()
		}
		ta.event(event.TextDelta{Base: rootBase(), MessageID: core.MessageID(fmt.Sprintf("live%d", reply)), Text: "word "})
		size += 5
		ta.fire()
		_ = ta.view()
	}
}

// benchHistory is a stored session of n messages alternating user and
// assistant, the assistant ones 1–12 lines of Markdown, so every text
// block goes through the real renderer (mdrender/glamour).
func benchHistory(n int) []core.Message {
	msgs := make([]core.Message, n)
	for i := range msgs {
		id := core.MessageID(fmt.Sprintf("m%05d", i))
		if i%2 == 0 {
			msgs[i] = core.Message{ID: id, SessionID: "ses_big", Role: core.RoleUser,
				Parts: []core.Part{{Kind: core.PartText, Text: fmt.Sprintf("question %d about the build", i)}}}
			continue
		}
		var b strings.Builder
		b.WriteString("## Answer\n\n")
		for l := range i % 12 {
			fmt.Fprintf(&b, "- item %d with `code` and **bold** text\n", l)
		}
		msgs[i] = core.Message{ID: id, SessionID: "ses_big", Role: core.RoleAssistant,
			Parts: []core.Part{{Kind: core.PartText, Text: b.String()}}}
	}
	return msgs
}

// BenchmarkApp_Resize2000 measures one terminal width change on a
// resumed 2,000-block session: a burst of three WindowSizeMsgs (coalesced
// into one list resize by the debounce), the debounce ticks, and the
// next View. The list re-renders every block at the new width.
func BenchmarkApp_Resize2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistory(2000), nil))
	if n := ta.app.w.list.Len(); n != 2000 {
		b.Fatalf("list has %d items, want 2000", n)
	}
	_ = ta.view()
	// Three widths in turn: the list keeps two widths' renders, so each
	// step is a width it holds nothing for.
	widths := [3]int{150, 155, 160}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		w := widths[i%3]
		i++
		for _, burst := range []int{w - 2, w - 1, w} {
			ta.send(tea.WindowSizeMsg{Width: burst, Height: 40})
		}
		ta.fire()
		_ = ta.view()
	}
}

// BenchmarkApp_Keystroke2000 measures typing one character into the
// prompt on a resumed 2,000-block session: the Update and the next View.
// Nothing in the transcript changes, so the frame's cost is composing it.
// The prompt is emptied every 80 characters, so the result doesn't depend
// on how many iterations run.
func BenchmarkApp_Keystroke2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistory(2000), nil))
	_ = ta.view()
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		if n++; n%80 == 0 {
			ta.app.w.prompt.Reset()
		}
		ta.send(tea.KeyPressMsg{Code: 'x', Text: "x"})
		_ = ta.view()
	}
}

// BenchmarkApp_Wheel2000 measures one mouse-wheel notch over the
// transcript of a resumed 2,000-block session and the next View, scrolling
// up and down in sweeps of 200 notches (so warm and newly shown blocks
// both count).
func BenchmarkApp_Wheel2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistory(2000), nil))
	_ = ta.view()
	x, y := ta.app.lay.Transcript.X+2, ta.app.lay.Transcript.Y+2
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		btn := tea.MouseWheelUp
		if (n/200)%2 == 1 {
			btn = tea.MouseWheelDown
		}
		n++
		ta.mouse(tea.MouseWheelMsg{X: x, Y: y, Button: btn})
		_ = ta.view()
	}
}

// BenchmarkApp_DetailsToggle2000 measures opening and closing the details
// split (enter, then q, each followed by the width debounce and a View)
// on a resumed 2,000-block session, after one warm-up pair: both widths
// are then cached, so a toggle re-renders nothing in the list.
func BenchmarkApp_DetailsToggle2000(b *testing.B) {
	ta := newTestApp(b, withSize(150, 40), withResume(core.Session{ID: "ses_big", Agent: "build"}, benchHistory(2000), nil))
	if n := ta.app.w.list.Len(); n != 2000 {
		b.Fatalf("list has %d items, want 2000", n)
	}
	toggle := func() {
		for _, k := range []string{"enter", "q"} {
			ta.key(k)
			ta.fire()
			_ = ta.view()
		}
	}
	ta.key("esc")
	_ = ta.view()
	closedW := ta.app.view.list.w
	ta.key("enter")
	ta.fire()
	_ = ta.view()
	if !ta.app.view.detailsOpen || ta.app.view.list.w == closedW {
		b.Fatalf("enter did not open the split at a new list width (open %v, width %d)", ta.app.view.detailsOpen, closedW)
	}
	ta.key("q")
	ta.fire()
	_ = ta.view()
	b.ReportAllocs()
	for b.Loop() {
		toggle()
	}
}
