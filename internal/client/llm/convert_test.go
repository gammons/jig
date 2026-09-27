package llm

import (
	"encoding/json"
	"testing"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

// textOf returns the text of a fantasy.MessagePart known to be a TextPart,
// failing the test otherwise.
func textOf(t *testing.T, p fantasy.MessagePart) string {
	t.Helper()
	tp, ok := p.(fantasy.TextPart)
	if !ok {
		t.Fatalf("part %#v is not a TextPart", p)
	}
	return tp.Text
}

func TestToFantasy_UserAssistantToolRoundTrip(t *testing.T) {
	req := core.LLMRequest{
		System: []string{"sys1", "sys2"},
		Messages: []core.Message{
			{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}},
			{Role: core.RoleAssistant, Parts: []core.Part{
				{Kind: core.PartText, Text: "checking"},
				{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "weather", Input: json.RawMessage(`{"loc":"nyc"}`)}},
				{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c1", Name: "weather", Output: "sunny"}},
			}},
		},
		Tools:           []core.ToolSpec{{Name: "weather", Description: "get weather", Schema: map[string]any{"type": "object"}}},
		MaxOutputTokens: 500,
	}

	call := ToFantasy(req)

	if len(call.Prompt) != 4 {
		t.Fatalf("got %d messages, want 4 (system, user, assistant, tool): %+v", len(call.Prompt), call.Prompt)
	}

	sys := call.Prompt[0]
	if sys.Role != fantasy.MessageRoleSystem || len(sys.Content) != 2 {
		t.Fatalf("system message: got %+v, want role system with 2 parts", sys)
	}
	if got := textOf(t, sys.Content[0]); got != "sys1" {
		t.Errorf("system part 0: got %q, want %q", got, "sys1")
	}
	if got := textOf(t, sys.Content[1]); got != "sys2" {
		t.Errorf("system part 1: got %q, want %q", got, "sys2")
	}

	user := call.Prompt[1]
	if user.Role != fantasy.MessageRoleUser || len(user.Content) != 1 {
		t.Fatalf("user message: got %+v, want role user with 1 part", user)
	}
	if got := textOf(t, user.Content[0]); got != "hi" {
		t.Errorf("user text: got %q, want %q", got, "hi")
	}

	asst := call.Prompt[2]
	if asst.Role != fantasy.MessageRoleAssistant || len(asst.Content) != 2 {
		t.Fatalf("assistant message: got %+v, want role assistant with 2 parts", asst)
	}
	if got := textOf(t, asst.Content[0]); got != "checking" {
		t.Errorf("assistant text: got %q, want %q", got, "checking")
	}
	tc, ok := asst.Content[1].(fantasy.ToolCallPart)
	if !ok {
		t.Fatalf("assistant part 1: got %#v, want ToolCallPart", asst.Content[1])
	}
	if tc.ToolCallID != "c1" || tc.ToolName != "weather" || tc.Input != `{"loc":"nyc"}` {
		t.Errorf("tool call part: got %+v, want {c1 weather {\"loc\":\"nyc\"}}", tc)
	}

	toolMsg := call.Prompt[3]
	if toolMsg.Role != fantasy.MessageRoleTool || len(toolMsg.Content) != 1 {
		t.Fatalf("tool message: got %+v, want role tool with 1 part", toolMsg)
	}
	tr, ok := toolMsg.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("tool part: got %#v, want ToolResultPart", toolMsg.Content[0])
	}
	text, ok := tr.Output.(fantasy.ToolResultOutputContentText)
	if !ok || text.Text != "sunny" || tr.ToolCallID != "c1" {
		t.Errorf("tool result: got %+v, want {c1 text:sunny}", tr)
	}

	if len(call.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(call.Tools))
	}
	ft, ok := call.Tools[0].(fantasy.FunctionTool)
	if !ok || ft.Name != "weather" || ft.Description != "get weather" {
		t.Errorf("tool: got %+v, want FunctionTool weather", call.Tools[0])
	}

	if call.MaxOutputTokens == nil || *call.MaxOutputTokens != 500 {
		t.Errorf("MaxOutputTokens: got %v, want 500", call.MaxOutputTokens)
	}
}

func TestToFantasy_SynthesizesMissingToolResults(t *testing.T) {
	req := core.LLMRequest{
		Messages: []core.Message{
			{Role: core.RoleAssistant, Parts: []core.Part{
				{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "bash", Input: json.RawMessage(`{}`)}},
			}},
		},
	}

	call := ToFantasy(req)

	if len(call.Prompt) != 2 {
		t.Fatalf("got %d messages, want 2 (assistant, tool): %+v", len(call.Prompt), call.Prompt)
	}
	toolMsg := call.Prompt[1]
	if toolMsg.Role != fantasy.MessageRoleTool || len(toolMsg.Content) != 1 {
		t.Fatalf("tool message: got %+v, want role tool with 1 synthesized result", toolMsg)
	}
	tr, ok := toolMsg.Content[0].(fantasy.ToolResultPart)
	if !ok || tr.ToolCallID != "c1" {
		t.Fatalf("tool result: got %#v, want ToolResultPart for c1", toolMsg.Content[0])
	}
	errOut, ok := tr.Output.(fantasy.ToolResultOutputContentError)
	if !ok || errOut.Error == nil || errOut.Error.Error() != "interrupted" {
		t.Errorf("synthesized result output: got %+v, want error %q", tr.Output, "interrupted")
	}
}

func TestToFantasy_ErrorResultUsesErrorContent(t *testing.T) {
	req := core.LLMRequest{
		Messages: []core.Message{
			{Role: core.RoleAssistant, Parts: []core.Part{
				{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "bash", Input: json.RawMessage(`{}`)}},
				{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c1", Name: "bash", Output: "boom", IsError: true}},
			}},
		},
	}

	call := ToFantasy(req)

	toolMsg := call.Prompt[1]
	tr, ok := toolMsg.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("tool result: got %#v, want ToolResultPart", toolMsg.Content[0])
	}
	errOut, ok := tr.Output.(fantasy.ToolResultOutputContentError)
	if !ok || errOut.Error == nil || errOut.Error.Error() != "boom" {
		t.Errorf("error result output: got %+v, want error %q", tr.Output, "boom")
	}
}

func TestToFantasy_CompactionBecomesSummaryUserMessage(t *testing.T) {
	req := core.LLMRequest{
		Messages: []core.Message{
			{Role: core.RoleAssistant, Agent: "compaction", Parts: []core.Part{
				{Kind: core.PartCompaction, Text: "prior summary"},
			}},
		},
	}

	call := ToFantasy(req)

	if len(call.Prompt) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(call.Prompt), call.Prompt)
	}
	msg := call.Prompt[0]
	if msg.Role != fantasy.MessageRoleUser || len(msg.Content) != 1 {
		t.Fatalf("compaction message: got %+v, want role user with 1 part", msg)
	}
	want := "Summary of the conversation so far:\nprior summary"
	if got := textOf(t, msg.Content[0]); got != want {
		t.Errorf("compaction text: got %q, want %q", got, want)
	}
}

func TestToFantasy_DropsPartlessAssistantMessage(t *testing.T) {
	req := core.LLMRequest{
		Messages: []core.Message{
			{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}},
			{Role: core.RoleAssistant, Parts: nil}, // e.g. a run interrupted before producing anything
		},
	}

	call := ToFantasy(req)

	if len(call.Prompt) != 1 {
		t.Fatalf("got %d messages, want 1 (the empty assistant message dropped): %+v", len(call.Prompt), call.Prompt)
	}
	if call.Prompt[0].Role != fantasy.MessageRoleUser {
		t.Errorf("remaining message: got role %q, want user", call.Prompt[0].Role)
	}
}

// TestToFantasy_NeverAppliesAnthropicCacheItself guards against
// regressing to ID-based gating inside ToFantasy: caching is applied by
// the adapter (keyed on the factory's Type), not here, even when
// req.Model.Provider happens to be "anthropic".
func TestToFantasy_NeverAppliesAnthropicCacheItself(t *testing.T) {
	req := core.LLMRequest{
		Model:  core.ModelRef{Provider: "anthropic", Model: "claude-3-5-sonnet-20241022"},
		System: []string{"s1"},
		Messages: []core.Message{
			{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}},
		},
	}

	call := ToFantasy(req)

	for i, msg := range call.Prompt {
		if msg.ProviderOptions != nil {
			t.Errorf("message %d: got ProviderOptions %+v, want nil (ToFantasy must not apply provider-specific options)", i, msg.ProviderOptions)
		}
		for j, part := range msg.Content {
			if part.Options() != nil {
				t.Errorf("message %d part %d: got Options %+v, want nil", i, j, part.Options())
			}
		}
	}
}
