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

func TestToolLine_MCP(t *testing.T) {
	t.Parallel()

	// With Result.Metadata present: name comes from metadata, not the
	// call name.
	call := &core.ToolCall{ID: "c1", Name: "mcp__gh__get_issue", Input: json.RawMessage(`{"repo":"owner/repo#1","other":"x"}`)}
	b := transcript.Block{Kind: transcript.KindTool, Call: call, Result: &core.ToolResult{
		CallID: "c1", Name: "mcp__gh__get_issue", Output: "ok",
		Metadata: map[string]string{"mcp.server": "gh", "mcp.tool": "get_issue"},
	}}
	icon, name, summary, hasStatus := toolLine(b, 0)
	if name != "gh get_issue" {
		t.Errorf("name = %q, want %q", name, "gh get_issue")
	}
	if summary != "owner/repo#1" {
		t.Errorf("summary = %q, want %q", summary, "owner/repo#1")
	}
	if hasStatus {
		t.Errorf("hasStatus = true, want false")
	}
	_ = icon

	// Running (no result): name is split from the call name.
	call2 := &core.ToolCall{ID: "c2", Name: "mcp__fake__echo", Input: json.RawMessage(`{"msg":"hi"}`)}
	b2 := transcript.Block{Kind: transcript.KindTool, Call: call2}
	_, name2, summary2, _ := toolLine(b2, 0)
	if name2 != "fake echo" {
		t.Errorf("name2 = %q, want %q", name2, "fake echo")
	}
	if summary2 != "hi" {
		t.Errorf("summary2 = %q, want %q", summary2, "hi")
	}
}

func TestToolLine_MCPSanitizes(t *testing.T) {
	t.Parallel()

	call := &core.ToolCall{ID: "c1", Name: "mcp__fake__echo", Input: json.RawMessage(`{"msg":"hi\u001b[2Jthere"}`)}
	b := transcript.Block{Kind: transcript.KindTool, Call: call}
	_, _, summary, _ := toolLine(b, 0)
	if summary != "hithere" {
		t.Errorf("summary = %q, want no escape sequence", summary)
	}
}

func TestToolLine_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		block         transcript.Block
		dur           time.Duration
		wantIcon      string
		wantName      string
		wantSummary   string
		wantHasStatus bool
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
			name:          "bash exit 0 with duration",
			block:         toolBlock("bash", `{"command":"ls -la"}`, "file1\nfile2", false),
			dur:           1200 * time.Millisecond,
			wantIcon:      "▸",
			wantName:      "bash",
			wantSummary:   "ls -la ✓ exit 0 · 1.2s",
			wantHasStatus: true,
		},
		{
			name:          "bash exit 3, no duration",
			block:         toolBlock("bash", `{"command":"false"}`, "boom\n[exit code 3]", true),
			wantIcon:      "▸",
			wantName:      "bash",
			wantSummary:   "false ✗ exit 3",
			wantHasStatus: true,
		},
		{
			name:          "bash timed out",
			block:         toolBlock("bash", `{"command":"sleep 99"}`, "\n[timed out after 5s]", true),
			dur:           2500 * time.Millisecond,
			wantIcon:      "▸",
			wantName:      "bash",
			wantSummary:   "sleep 99 ✗ timed out · 2.5s",
			wantHasStatus: true,
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
			wantHasStatus: true,
		},
		{
			name:        "bash error with no result yet (settled by RunFailed) has no embedded status",
			block:       transcript.Block{Kind: transcript.KindTool, State: transcript.StateError, Call: &core.ToolCall{ID: "c1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)}},
			wantIcon:    "▸",
			wantName:    "bash",
			wantSummary: "ls",
		},
		{
			name:          "bash first line is sanitized before the 60-rune cut",
			block:         toolBlock("bash", `{"command":"echo hi\u001b[2Jpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpad"}`, "done", false),
			wantIcon:      "▸",
			wantName:      "bash",
			wantSummary:   "echo hipadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpadpa… ✓ exit 0",
			wantHasStatus: true,
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
			name:        "agent-browser error has no embedded status either",
			block:       toolBlock("bash", `{"command":"agent-browser open evil.test"}`, "boom", true),
			wantIcon:    "🌐",
			wantName:    "",
			wantSummary: "open evil.test",
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
			name:        "grep error does not count its error message as matches",
			block:       toolBlock("grep", `{"pattern":"("}`, "error parsing regexp: missing closing ): `(`", true),
			wantIcon:    "▸",
			wantName:    "grep",
			wantSummary: `"("`,
		},
		{
			name:        "glob error does not count its error message as matches",
			block:       toolBlock("glob", `{"pattern":"[bad"}`, "syntax error in pattern", true),
			wantIcon:    "▸",
			wantName:    "glob",
			wantSummary: `"[bad"`,
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
			icon, name, summary, hasStatus := toolLine(tt.block, tt.dur)
			if icon != tt.wantIcon || name != tt.wantName || summary != tt.wantSummary || hasStatus != tt.wantHasStatus {
				t.Errorf("toolLine = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					icon, name, summary, hasStatus, tt.wantIcon, tt.wantName, tt.wantSummary, tt.wantHasStatus)
			}
		})
	}
}
