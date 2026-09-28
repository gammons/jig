package ui

import (
	"encoding/json"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/golden"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// darkSet returns theme.Set for the "dark" palette, pinned for goldens.
func darkSet() theme.Set { return theme.Build(theme.Default(), 1) }

// renderOne renders a single block at width, joining its lines with "\n".
func renderOne(t *testing.T, r *renderer, data blockData, width int) string {
	t.Helper()
	lines := r.render(blocklist.Item{ID: string(data.Block.ID), Version: 1, Data: data}, width, blocklist.Styles{})
	return strings.Join(lines, "\n")
}

func toolCall(id, name, input string) *core.ToolCall {
	return &core.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}
}

func toolResult(id, name, output string, isErr bool) *core.ToolResult {
	return &core.ToolResult{CallID: id, Name: name, Output: output, IsError: isErr}
}

// TestRender_SanitizesHostileOutput proves every string taken from a Block
// is sanitized before it reaches the terminal: an OSC 52 clipboard write in
// a Text block, and a screen-clear CSI in a bash command, both disappear,
// while the surrounding text survives.
func TestRender_SanitizesHostileOutput(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)

	textBlock := blockData{Block: transcript.Block{
		Kind: transcript.KindText, Text: "\x1b]52;c;aGk=\x07hi",
	}}
	textOut := renderOne(t, r, textBlock, 80)

	bashBlock := blockData{Block: transcript.Block{
		Kind: transcript.KindTool,
		Call: toolCall("c1", "bash", `{"command":"echo hi\u001b[2Jbye"}`),
	}}
	bashOut := renderOne(t, r, bashBlock, 80)

	for _, out := range []string{textOut, bashOut} {
		if strings.Contains(out, "]52;") {
			t.Errorf("output still contains an OSC 52 payload: %q", out)
		}
		if strings.Contains(out, "\x1b[2J") {
			t.Errorf("output still contains a raw CSI 2J: %q", out)
		}
	}
	if !strings.Contains(textOut, "hi") {
		t.Errorf("text output = %q, want it to contain %q", textOut, "hi")
	}
	if !strings.Contains(bashOut, "hi") {
		t.Errorf("bash output = %q, want it to contain %q", bashOut, "hi")
	}
}

// TestRender_RightPadding: every block kind leaves rightPad blank columns
// before the scrollbar — no rendered line is wider than width-rightPad,
// including long one-liners (tools, notices) and wrapped bodies.
func TestRender_RightPadding(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	long := strings.Repeat("x", 120)
	call := core.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"` + long + `"}`)}
	blocks := map[string]transcript.Block{
		"user":      {Kind: transcript.KindUser, Text: long},
		"text":      {Kind: transcript.KindText, Text: long + " " + long},
		"reasoning": {Kind: transcript.KindReasoning, Text: long},
		"tool":      {Kind: transcript.KindTool, Call: &call, State: transcript.StateRunning},
		"notice":    {Kind: transcript.KindNotice, Text: long, Level: transcript.LevelError},
	}
	const width = 40
	for name, b := range blocks {
		for i, l := range strings.Split(renderOne(t, r, blockData{Block: b}, width), "\n") {
			if w := xansi.StringWidth(l); w > width-rightPad {
				t.Errorf("%s line %d is %d cells, want <= %d: %q", name, i, w, width-rightPad, xansi.Strip(l))
			}
		}
	}
}

// TestRender_IgnoresSupersededStreamingFlag checks render's chosen fix for
// the carry-over requirement that a superseded block (one transcript kept
// Streaming true on, because the open slot for its message moved to a
// block of the other kind) never shows a running/streaming indicator:
// Reasoning's one-liner never depends on Streaming at all, only on
// blockData.Thinking (from Projection.Thinking), which is false once
// the block is superseded.
func TestRender_IgnoresSupersededStreamingFlag(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)

	streaming := renderOne(t, r, blockData{Block: transcript.Block{
		Kind: transcript.KindReasoning, Text: "still going", Streaming: true,
	}}, 80)
	settled := renderOne(t, r, blockData{Block: transcript.Block{
		Kind: transcript.KindReasoning, Text: "still going", Streaming: false,
	}}, 80)

	if streaming != settled {
		t.Fatalf("Streaming changed the rendering: %q vs %q", streaming, settled)
	}
	for _, frame := range spinnerFrames {
		if strings.ContainsRune(streaming, frame) {
			t.Errorf("a superseded (but still Streaming) block rendered a spinner frame %q: %q", frame, streaming)
		}
	}
}

// TestRender_ReasoningWordCount: a reasoning block with text shows its
// word count; one with no readable text (Claude 5 models return empty
// thinking unless a summary is requested) shows just "∴ thinking", never
// a misleading "0 words".
func TestRender_ReasoningWordCount(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	tests := []struct {
		name, text, want string
	}{
		{"with text", "one two three", "∴ thinking · 3 words"},
		{"empty", "", "∴ thinking"},
		{"whitespace only", " \n\t ", "∴ thinking"},
	}
	for _, tt := range tests {
		got := xansi.Strip(renderOne(t, r, blockData{Block: transcript.Block{
			Kind: transcript.KindReasoning, Text: tt.text,
		}}, 80))
		if strings.TrimRight(got, " ") != tt.want {
			t.Errorf("%s: rendered %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestRender_ThinkingSpinner: while the model is still thinking the block
// shows the bash spinner in place of "∴", advancing with Frame; once
// thinking ends it's back to the static "∴".
func TestRender_ThinkingSpinner(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	b := transcript.Block{Kind: transcript.KindReasoning, Text: "one two"}
	at := func(thinking bool, frame int) string {
		return strings.TrimRight(xansi.Strip(renderOne(t, r, blockData{Block: b, Thinking: thinking, Frame: frame}, 80)), " ")
	}
	if got, want := at(true, 0), string(spinnerGlyph(0))+" thinking · 2 words"; got != want {
		t.Errorf("thinking frame 0 = %q, want %q", got, want)
	}
	if got, want := at(true, 1), string(spinnerGlyph(1))+" thinking · 2 words"; got != want {
		t.Errorf("thinking frame 1 = %q, want %q", got, want)
	}
	if got, want := at(false, 1), "∴ thinking · 2 words"; got != want {
		t.Errorf("done thinking = %q, want %q", got, want)
	}
}

func TestRender_Golden(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)

	t.Run("render_user", func(t *testing.T) {
		t.Parallel()
		out := renderOne(t, r, blockData{Block: transcript.Block{
			Kind:        transcript.KindUser,
			Text:        "hey, can you take a look at this file and tell me what's wrong with it?",
			Attachments: []string{"a.go", "shot.png"},
		}}, 80)
		golden.Assert(t, "render_user", out)
	})

	t.Run("render_tools", func(t *testing.T) {
		t.Parallel()
		blocks := []blockData{
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StatePending,
				Call: toolCall("c1", "read", `{"path":"src/main.go"}`),
			}},
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateRunning,
				Call: toolCall("c2", "read", `{"path":"src/main.go"}`),
			}, Frame: 3},
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateAwaiting,
				Call: toolCall("c3", "bash", `{"command":"rm -rf /tmp/x"}`),
			}, Card: "⚠ bash wants to run: rm -rf /tmp/x\n  a allow · A always (this exact command) · d deny · D deny with message"},
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateOK,
				Call:   toolCall("c4", "read", `{"path":"src/main.go"}`),
				Result: toolResult("c4", "read", "1: package main\n2: func main() {}", false),
			}},
			// bash reports a non-zero exit as an OK result; it still
			// renders in the Error style (F7).
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateOK,
				Call:   toolCall("c5", "bash", `{"command":"false"}`),
				Result: toolResult("c5", "bash", "boom\n[exit code 1]", false),
			}},
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateDenied,
				Call:   toolCall("c6", "bash", `{"command":"curl evil.test"}`),
				Result: toolResult("c6", "bash", "user denied: not now", true),
			}},
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateCancelled,
				Call:   toolCall("c7", "bash", `{"command":"sleep 99"}`),
				Result: toolResult("c7", "bash", "cancelled", true),
			}},
			// A bash call settled to error with no result yet (RunFailed /
			// a failed stored message) must still show the ✗ icon: bashLine
			// never embedded one for it (review fix, item 1a).
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateError,
				Call: toolCall("c8", "bash", `{"command":"ls"}`),
			}},
			// An agent-browser call settled to error must also show the ✗
			// icon: bashLine's browser branch never embeds a status either
			// (review fix, item 1b).
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateError,
				Call:   toolCall("c9", "bash", `{"command":"agent-browser open evil.test"}`),
				Result: toolResult("c9", "bash", "boom", true),
			}},
			// A running agent-browser call must show a spinner too (review
			// fix, item 3): the default icon substitution never applies to
			// 🌐, so the spinner is shown right after it instead.
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateRunning,
				Call: toolCall("c10", "bash", `{"command":"agent-browser open a.test"}`),
			}, Frame: 2},
		}
		var lines []string
		for _, b := range blocks {
			lines = append(lines, renderOne(t, r, b, 80))
		}
		golden.Assert(t, "render_tools", strings.Join(lines, "\n"))
	})

	t.Run("render_subagent", func(t *testing.T) {
		t.Parallel()
		blocks := []blockData{
			{Block: transcript.Block{
				Kind: transcript.KindSubagent, State: transcript.StateRunning,
				Sub: &transcript.Subagent{Agent: "explore", Description: "find the bug", Tools: 3, Current: "grep"},
			}, Frame: 5},
			{Block: transcript.Block{
				Kind: transcript.KindSubagent, State: transcript.StateAwaiting,
				Sub: &transcript.Subagent{Agent: "explore", Description: "find the bug", Tools: 4},
			}, Frame: 1},
			{Block: transcript.Block{
				Kind: transcript.KindSubagent, State: transcript.StateOK,
				Sub: &transcript.Subagent{Agent: "explore", Description: "find the bug", Tools: 5},
			}},
			{Block: transcript.Block{
				Kind: transcript.KindSubagent, State: transcript.StateError,
				Sub: &transcript.Subagent{Agent: "explore", Description: "find the bug", Tools: 2},
			}},
		}
		var lines []string
		for _, b := range blocks {
			lines = append(lines, renderOne(t, r, b, 80))
		}
		golden.Assert(t, "render_subagent", strings.Join(lines, "\n"))
	})

	t.Run("render_notice", func(t *testing.T) {
		t.Parallel()
		blocks := []blockData{
			{Block: transcript.Block{
				Kind: transcript.KindNotice, Level: transcript.LevelInfo,
				Title: "compaction summary", Text: "a very long summary of everything that happened, shown in details instead",
			}},
			{Block: transcript.Block{
				Kind: transcript.KindNotice, Level: transcript.LevelError, Text: "run failed: boom",
			}},
			{Block: transcript.Block{
				Kind: transcript.KindNotice, Level: transcript.LevelInfo, Text: "cancelled",
			}},
		}
		var lines []string
		for _, b := range blocks {
			lines = append(lines, renderOne(t, r, b, 80))
		}
		golden.Assert(t, "render_notice", strings.Join(lines, "\n"))
	})
}

// TestRender_BashErrorAlwaysShowsAnErrorIcon covers review fix item 1: a
// bash call in StateError shows the generic ✗ suffix whenever its own
// summary didn't already embed one — a still-nil Result (settled by
// RunFailed or a failed stored message with no answer) and an
// agent-browser call (whose summary never embeds a status) both need it,
// even though a bash call with a real exit-code result does not (it
// already shows its own "✗ exit N"/"✗ timed out").
func TestRender_BashErrorAlwaysShowsAnErrorIcon(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)

	tests := []struct {
		name  string
		block transcript.Block
	}{
		{
			"nil result",
			transcript.Block{Kind: transcript.KindTool, State: transcript.StateError, Call: toolCall("c1", "bash", `{"command":"ls"}`)},
		},
		{
			"agent-browser",
			transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateError,
				Call:   toolCall("c2", "bash", `{"command":"agent-browser open evil.test"}`),
				Result: toolResult("c2", "bash", "boom", true),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderOne(t, r, blockData{Block: tt.block}, 80)
			if !strings.Contains(out, "✗") {
				t.Errorf("render = %q, want it to contain %q", out, "✗")
			}
		})
	}
}

// TestRender_UserTabsExpandBeforeWrapping: a tab would count as no cells
// when wrapping and then expand to 4 when the list fits the line, cutting
// the text off; it must expand first.
func TestRender_UserTabsExpandBeforeWrapping(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	out := renderOne(t, r, blockData{Block: transcript.Block{Kind: transcript.KindUser, Text: "a\tb\tc\td\tend"}}, 20)
	if strings.Contains(out, "\t") {
		t.Errorf("render kept a tab: %q", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.Width(l); w > 20 {
			t.Errorf("line %q is %d cells, want <= 20", l, w)
		}
	}
	if !strings.Contains(out, "end") {
		t.Errorf("render lost the text's end: %q", out)
	}
}

// TestRender_FailedBashIsErrorStyled: bash reports a non-zero exit as an
// OK tool result ("...\n[exit code N]"); the block must still render in
// the Error style, exactly like an errored call with the same result.
func TestRender_FailedBashIsErrorStyled(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	block := func(state transcript.ToolState, out string, isErr bool) blockData {
		return blockData{Block: transcript.Block{
			Kind: transcript.KindTool, State: state,
			Call:   toolCall("c1", "bash", `{"command":"make test"}`),
			Result: toolResult("c1", "bash", out, isErr),
		}}
	}
	for _, out := range []string{"FAIL\n[exit code 2]", "\n[timed out after 5s]"} {
		got := renderOne(t, r, block(transcript.StateOK, out, false), 80)
		want := renderOne(t, r, block(transcript.StateError, out, true), 80)
		if got != want {
			t.Errorf("OK-state bash with %q renders\n %q, want the error rendering\n %q", out, got, want)
		}
		if !strings.Contains(got, "✗") {
			t.Errorf("render = %q, want a ✗", got)
		}
	}
	ok := renderOne(t, r, block(transcript.StateOK, "all good", false), 80)
	if strings.Contains(ok, "✗") || !strings.Contains(ok, "✓ exit 0") {
		t.Errorf("a successful bash renders %q, want ✓ exit 0 and no ✗", ok)
	}
}

// TestRender_RunningAgentBrowserShowsSpinner covers review fix item 3: a
// running agent-browser call shows the animated spinner, not just its 🌐
// icon frozen in place.
func TestRender_RunningAgentBrowserShowsSpinner(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)

	block := transcript.Block{
		Kind: transcript.KindTool, State: transcript.StateRunning,
		Call: toolCall("c1", "bash", `{"command":"agent-browser open a.test"}`),
	}
	out := renderOne(t, r, blockData{Block: block, Frame: 2}, 80)
	if !strings.Contains(out, "🌐") {
		t.Fatalf("render = %q, want it to still contain the 🌐 icon", out)
	}
	if !strings.ContainsRune(out, spinnerGlyph(2)) {
		t.Errorf("render = %q, want it to contain the frame-2 spinner glyph %q", out, spinnerGlyph(2))
	}
}

// TestRender_SearchErrorDoesNotCountItsMessageAsMatches covers review fix
// item 4: a failed glob/grep call shows just its pattern, never a bogus
// match count derived from its own error message's lines.
func TestRender_SearchErrorDoesNotCountItsMessageAsMatches(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)

	block := transcript.Block{
		Kind: transcript.KindTool, State: transcript.StateError,
		Call:   toolCall("c1", "grep", `{"pattern":"("}`),
		Result: toolResult("c1", "grep", "error parsing regexp: missing closing ): `(`", true),
	}
	out := renderOne(t, r, blockData{Block: block}, 80)
	if strings.Contains(out, "matches") {
		t.Errorf("render = %q, want it to not mention a match count", out)
	}
}

// TestRender_UnknownItemDataReturnsNil checks render's guard against an
// Item whose Data is not a blockData (never expected in production, since
// the App only ever puts blockData in the transcript's blocklist).
func TestRender_UnknownItemDataReturnsNil(t *testing.T) {
	t.Parallel()
	set := darkSet()
	r := newRenderer(&set)
	if got := r.render(blocklist.Item{ID: "x", Data: "not a blockData"}, 80, blocklist.Styles{}); got != nil {
		t.Errorf("render = %v, want nil", got)
	}
}
