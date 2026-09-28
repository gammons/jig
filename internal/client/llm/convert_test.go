package llm

import (
	"encoding/base64"
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

func TestToFantasy_UserAttachments(t *testing.T) {
	req := core.LLMRequest{
		Messages: []core.Message{
			{Role: core.RoleUser, Parts: []core.Part{
				{Kind: core.PartText, Text: "look"},
				{Kind: core.PartAttachment, Attachment: &core.Attachment{Path: "/w/notes.txt", Content: "hello notes"}},
				{Kind: core.PartAttachment, Attachment: &core.Attachment{
					Path:  "/w/shot.png",
					Media: &core.Media{MIME: "image/png", Data: []byte{1, 2, 3}},
				}},
			}},
		},
	}

	call := ToFantasy(req)
	if len(call.Prompt) != 1 {
		t.Fatalf("got %d messages, want 1", len(call.Prompt))
	}
	user := call.Prompt[0]
	if user.Role != fantasy.MessageRoleUser || len(user.Content) != 3 {
		t.Fatalf("user message = %+v, want role user with 3 parts", user)
	}
	if got := textOf(t, user.Content[0]); got != "look" {
		t.Errorf("text part = %q, want %q", got, "look")
	}
	want := "<attachment path=\"/w/notes.txt\">\nhello notes\n</attachment>"
	if got := textOf(t, user.Content[1]); got != want {
		t.Errorf("text attachment = %q, want %q", got, want)
	}
	fp, ok := user.Content[2].(fantasy.FilePart)
	if !ok {
		t.Fatalf("image attachment = %#v, want FilePart", user.Content[2])
	}
	if fp.Filename != "shot.png" || fp.MediaType != "image/png" || string(fp.Data) != "\x01\x02\x03" {
		t.Errorf("file part = %+v", fp)
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

func TestToFantasy_NonObjectToolInputReplaysAsEmptyObject(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"wrapped invalid", `"{\"path\": \"a.go\", \"con"`, `{}`},
		{"array", `[1,2]`, `{}`},
		{"null", `null`, `{}`},
		{"number", `42`, `{}`},
		{"invalid", `{not json`, `{}`},
		{"empty", ``, `{}`},
		{"object", `{"loc":"nyc"}`, `{"loc":"nyc"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := core.LLMRequest{Messages: []core.Message{{Role: core.RoleAssistant, Parts: []core.Part{
				{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "write", Input: json.RawMessage(c.input)}},
				{Kind: core.PartToolResult, Result: &core.ToolResult{CallID: "c1", Name: "write", Output: "bad", IsError: true}},
			}}}}
			call := ToFantasy(req)
			tc, ok := call.Prompt[0].Content[0].(fantasy.ToolCallPart)
			if !ok {
				t.Fatalf("part = %#v, want ToolCallPart", call.Prompt[0].Content[0])
			}
			if tc.Input != c.want {
				t.Errorf("Input = %q, want %q", tc.Input, c.want)
			}
		})
	}
}

// toolOutput returns the Output of the single tool result ToFantasy makes
// from one assistant message holding call c1 and result r.
func toolOutput(t *testing.T, r *core.ToolResult) fantasy.ToolResultOutputContent {
	t.Helper()
	call := ToFantasy(core.LLMRequest{Messages: []core.Message{{Role: core.RoleAssistant, Parts: []core.Part{
		{Kind: core.PartToolCall, Call: &core.ToolCall{ID: "c1", Name: "read", Input: json.RawMessage(`{}`)}},
		{Kind: core.PartToolResult, Result: r},
	}}}})
	if len(call.Prompt) != 2 || len(call.Prompt[1].Content) != 1 {
		t.Fatalf("prompt = %+v, want assistant + one tool result", call.Prompt)
	}
	tr, ok := call.Prompt[1].Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("part = %#v, want ToolResultPart", call.Prompt[1].Content[0])
	}
	return tr.Output
}

func TestToFantasy_ToolResultMedia(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', 0, 1, 2}
	out := toolOutput(t, &core.ToolResult{CallID: "c1", Output: "image 20x10 (7 B)", Media: []core.Media{{MIME: "image/png", Ref: "r", Data: data}}})
	m, ok := out.(fantasy.ToolResultOutputContentMedia)
	if !ok {
		t.Fatalf("Output = %#v, want ToolResultOutputContentMedia", out)
	}
	if m.Data != base64.StdEncoding.EncodeToString(data) {
		t.Errorf("Data = %q, want base64 of the bytes", m.Data)
	}
	if m.MediaType != "image/png" || m.Text != "image 20x10 (7 B)" {
		t.Errorf("MediaType/Text = %q/%q, want image/png and the Output", m.MediaType, m.Text)
	}
}

func TestToFantasy_ToolResultExtraMediaOmitted(t *testing.T) {
	media := []core.Media{{MIME: "image/png", Data: []byte("a")}, {MIME: "image/png", Data: []byte("b")}, {MIME: "image/png", Data: []byte("c")}}
	out := toolOutput(t, &core.ToolResult{CallID: "c1", Output: "shots", Media: media})
	m, ok := out.(fantasy.ToolResultOutputContentMedia)
	if !ok {
		t.Fatalf("Output = %#v, want ToolResultOutputContentMedia", out)
	}
	if m.Text != "shots [+2 more images omitted]" {
		t.Errorf("Text = %q, want the extra-media note", m.Text)
	}
}

func TestToFantasy_UnloadedMediaIsText(t *testing.T) {
	out := toolOutput(t, &core.ToolResult{CallID: "c1", Output: "img", Media: []core.Media{{MIME: "image/png", Ref: "r"}}})
	if tx, ok := out.(fantasy.ToolResultOutputContentText); !ok || tx.Text != "img" {
		t.Errorf("Output = %#v, want text %q (no Data loaded)", out, "img")
	}
}

func TestToFantasy_ErrorResultIgnoresMedia(t *testing.T) {
	out := toolOutput(t, &core.ToolResult{CallID: "c1", Output: "boom", IsError: true, Media: []core.Media{{MIME: "image/png", Data: []byte("x")}}})
	e, ok := out.(fantasy.ToolResultOutputContentError)
	if !ok || e.Error.Error() != "boom" {
		t.Errorf("Output = %#v, want error %q", out, "boom")
	}
}
