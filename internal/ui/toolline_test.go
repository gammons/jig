package ui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

func toolBlock(name, input, output string, isErr bool) transcript.Block {
	call := &core.ToolCall{ID: "c1", Name: name, Input: json.RawMessage(input)}
	b := transcript.Block{Kind: transcript.KindTool, Call: call}
	if output != "" || isErr {
		b.Result = &core.ToolResult{CallID: "c1", Name: name, Output: output, IsError: isErr}
	}
	return b
}

func TestToolLine_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		block       transcript.Block
		dur         time.Duration
		wantIcon    string
		wantName    string
		wantSummary string
	}{
		{
			name:        "read",
			block:       toolBlock("read", `{"path":"x.go"}`, "1: package x\n2: foo", false),
			wantIcon:    "▸",
			wantName:    "read",
			wantSummary: "x.go · 2 lines",
		},
		{
			name:        "read image",
			block:       toolBlock("read", `{"path":"x.png"}`, "image 800x600 (120 KB)", false),
			wantIcon:    "▸",
			wantName:    "read",
			wantSummary: "x.png · image 800x600",
		},
		{
			name:        "write",
			block:       toolBlock("write", `{"path":"a.go","content":"a\nb\nc"}`, "wrote 5 bytes to a.go", false),
			wantIcon:    "▸",
			wantName:    "write",
			wantSummary: "a.go +3",
		},
		{
			name:        "edit",
			block:       toolBlock("edit", `{"path":"a.go","old_string":"a\nb","new_string":"x\ny\nz"}`, "replaced 1 occurrence in a.go", false),
			wantIcon:    "▸",
			wantName:    "edit",
			wantSummary: "a.go +3 -2",
		},
		{
			name:        "bash exit 0 with duration",
			block:       toolBlock("bash", `{"command":"ls -la"}`, "file1\nfile2", false),
			dur:         1200 * time.Millisecond,
			wantIcon:    "▸",
			wantName:    "bash",
			wantSummary: "ls -la ✓ exit 0 · 1.2s",
		},
		{
			name:        "bash exit 3, no duration",
			block:       toolBlock("bash", `{"command":"false"}`, "boom\n[exit code 3]", true),
			wantIcon:    "▸",
			wantName:    "bash",
			wantSummary: "false ✗ exit 3",
		},
		{
			name:        "bash timed out",
			block:       toolBlock("bash", `{"command":"sleep 99"}`, "\n[timed out after 5s]", true),
			dur:         2500 * time.Millisecond,
			wantIcon:    "▸",
			wantName:    "bash",
			wantSummary: "sleep 99 ✗ timed out · 2.5s",
		},
		{
			name: "bash first line truncated to 60 runes",
			block: toolBlock("bash",
				`{"command":"echo 01234567890123456789012345678901234567890123456789012345678901234567890123"}`,
				"done", false),
			wantIcon: "▸",
			wantName: "bash",
			wantSummary: "echo 0123456789012345678901234567890123456789012345678901234" +
				"… ✓ exit 0",
		},
		{
			name:        "agent-browser with global flags",
			block:       toolBlock("bash", `{"command":"agent-browser --session s1 open x"}`, "", false),
			wantIcon:    "🌐",
			wantName:    "",
			wantSummary: "open x",
		},
		{
			name:        "agent-browser no flags",
			block:       toolBlock("bash", `{"command":"agent-browser screenshot"}`, "", false),
			wantIcon:    "🌐",
			wantName:    "",
			wantSummary: "screenshot",
		},
		{
			name:        "glob matches",
			block:       toolBlock("glob", `{"pattern":"*.go"}`, "a.go\nb.go", false),
			wantIcon:    "▸",
			wantName:    "glob",
			wantSummary: `"*.go" · 2 matches`,
		},
		{
			name:        "grep no matches",
			block:       toolBlock("grep", `{"pattern":"foo"}`, "no matches", false),
			wantIcon:    "▸",
			wantName:    "grep",
			wantSummary: `"foo" · 0 matches`,
		},
		{
			name:        "grep truncated matches not double counted",
			block:       toolBlock("grep", `{"pattern":"foo"}`, "a.go:1: foo\n[truncated at 100 results]", false),
			wantIcon:    "▸",
			wantName:    "grep",
			wantSummary: `"foo" · 1 matches`,
		},
		{
			name:        "todo",
			block:       toolBlock("todo", `{"todos":[{"content":"a","status":"pending"},{"content":"b","status":"in_progress"}]}`, "[ ] a\n[~] b", false),
			wantIcon:    "▸",
			wantName:    "todo",
			wantSummary: "2 items (1 in progress)",
		},
		{
			name:        "skill",
			block:       toolBlock("skill", `{"id":"brainstorming"}`, "<skill_content...>", false),
			wantIcon:    "▸",
			wantName:    "skill",
			wantSummary: "brainstorming",
		},
		{
			name:        "unknown tool",
			block:       toolBlock("custom_tool", `{"foo": "bar", "baz": 1}`, "ok", false),
			wantIcon:    "▸",
			wantName:    "custom_tool",
			wantSummary: `{"foo":"bar","baz":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			icon, name, summary := toolLine(tt.block, tt.dur)
			if icon != tt.wantIcon || name != tt.wantName || summary != tt.wantSummary {
				t.Errorf("toolLine = (%q, %q, %q), want (%q, %q, %q)",
					icon, name, summary, tt.wantIcon, tt.wantName, tt.wantSummary)
			}
		})
	}
}
