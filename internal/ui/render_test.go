package ui

import (
	"encoding/json"
	"strings"
	"testing"

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

// TestRender_IgnoresSupersededStreamingFlag checks render's chosen fix for
// the carry-over requirement that a superseded block (one transcript kept
// Streaming true on, because the open slot for its message moved to a
// block of the other kind) never shows a running/streaming indicator:
// Reasoning's one-liner never depends on Streaming at all.
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
			{Block: transcript.Block{
				Kind: transcript.KindTool, State: transcript.StateError,
				Call:   toolCall("c5", "bash", `{"command":"false"}`),
				Result: toolResult("c5", "bash", "boom\n[exit code 1]", true),
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
