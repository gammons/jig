// Package llm adapts jig's core.LLM port onto charm.land/fantasy: it
// converts requests and streamed responses between the two shapes, maps
// fantasy's provider errors onto core.LLMError, and resolves a
// core.ModelRef into a credentialed core.LLM through Source.
package llm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

// compactionPrefix is prepended to a PartCompaction's summary text when it
// is converted into a user message.
const compactionPrefix = "Summary of the conversation so far:\n"

// interruptedResultText is the synthesized ToolResult output for a
// ToolCallPart that never received a real result, e.g. because the run was
// cancelled mid-turn.
const interruptedResultText = "interrupted"

// ToFantasy converts req into a fantasy.Call: system strings become one
// system message, each core.Message becomes zero or more fantasy.Message
// (an assistant message with no content parts is dropped, since Anthropic
// and friends reject empty message content), and tools/MaxOutputTokens
// carry across directly. It does not apply any provider-specific
// treatment (e.g. Anthropic's prompt caching): that is the adapter's job,
// since it is keyed on the provider's Type, not on req.Model.Provider (an
// arbitrary catalog ID).
func ToFantasy(req core.LLMRequest) fantasy.Call {
	var messages []fantasy.Message
	if len(req.System) > 0 {
		messages = append(messages, fantasy.NewSystemMessage(req.System...))
	}
	for _, m := range req.Messages {
		messages = append(messages, convertMessage(m)...)
	}

	call := fantasy.Call{
		Prompt: messages,
		Tools:  convertTools(req.Tools),
	}
	if req.MaxOutputTokens > 0 {
		tokens := req.MaxOutputTokens
		call.MaxOutputTokens = &tokens
	}
	return call
}

// convertMessage converts one core.Message into the fantasy.Message(s) it
// becomes: a compaction message becomes a single user message; otherwise a
// user message becomes one fantasy user message, and an assistant message
// becomes an assistant message optionally followed by a tool-result
// message.
func convertMessage(m core.Message) []fantasy.Message {
	if text, ok := compactionText(m); ok {
		return []fantasy.Message{fantasy.NewUserMessage(compactionPrefix + text)}
	}
	switch m.Role {
	case core.RoleUser:
		return []fantasy.Message{convertUserMessage(m)}
	case core.RoleAssistant:
		return convertAssistantMessage(m)
	default:
		return nil
	}
}

// compactionText returns m's compaction summary and true if m's Parts
// consist of a single PartCompaction part.
func compactionText(m core.Message) (string, bool) {
	if len(m.Parts) != 1 || m.Parts[0].Kind != core.PartCompaction {
		return "", false
	}
	return m.Parts[0].Text, true
}

// convertUserMessage converts m's text and attachment parts, in order,
// into a fantasy user message: each text part becomes a TextPart, and each
// attachment becomes attachmentPart's result.
func convertUserMessage(m core.Message) fantasy.Message {
	var content []fantasy.MessagePart
	for _, p := range m.Parts {
		switch {
		case p.Kind == core.PartText:
			content = append(content, fantasy.TextPart{Text: p.Text})
		case p.Kind == core.PartAttachment && p.Attachment != nil:
			content = append(content, attachmentPart(*p.Attachment))
		}
	}
	return fantasy.Message{Role: fantasy.MessageRoleUser, Content: content}
}

// attachmentPart converts a into the fantasy content it becomes: an image
// whose Data has been loaded becomes a FilePart; a text attachment, or an
// image whose Data could not be loaded (client/llm's media wrapper has
// already replaced Content with a placeholder and cleared Media), becomes
// a TextPart wrapping the content in an <attachment> tag.
func attachmentPart(a core.Attachment) fantasy.MessagePart {
	if a.Media != nil && a.Media.Data != nil {
		return fantasy.FilePart{Filename: filepath.Base(a.Path), Data: a.Media.Data, MediaType: a.Media.MIME}
	}
	return fantasy.TextPart{Text: fmt.Sprintf("<attachment path=%q>\n%s\n</attachment>", a.Path, a.Content)}
}

// convertAssistantMessage splits m's parts into an assistant message
// (text, reasoning, and tool-call parts) and, if it contains any tool
// calls, a following tool-role message carrying one ToolResultPart per
// call: the call's real result if any, else a synthesized "interrupted"
// error result. An assistant message with no content parts at all (e.g. a
// run interrupted before it produced anything) is dropped: Anthropic and
// friends reject empty message content.
// replayInput returns a stored tool call's input as sent back to a
// provider: unchanged when it is a JSON object, "{}" otherwise (invalid
// JSON, a string, array, number, null, or empty). Providers expect object
// arguments; Gemini, for one, drops a non-object FunctionCall and then
// rejects the orphaned FunctionResponse.
func replayInput(input json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil || obj == nil {
		return "{}"
	}
	return string(input)
}

func convertAssistantMessage(m core.Message) []fantasy.Message {
	var content []fantasy.MessagePart
	var callOrder []string
	resultByCall := map[string]*core.ToolResult{}

	for _, p := range m.Parts {
		switch p.Kind {
		case core.PartText:
			content = append(content, fantasy.TextPart{Text: p.Text})
		case core.PartReasoning:
			content = append(content, fantasy.ReasoningPart{Text: p.Text})
		case core.PartToolCall:
			content = append(content, fantasy.ToolCallPart{
				ToolCallID: p.Call.ID,
				ToolName:   p.Call.Name,
				Input:      replayInput(p.Call.Input),
			})
			callOrder = append(callOrder, p.Call.ID)
		case core.PartToolResult:
			resultByCall[p.Result.CallID] = p.Result
		}
	}

	if len(content) == 0 {
		return nil
	}

	msgs := []fantasy.Message{{Role: fantasy.MessageRoleAssistant, Content: content}}
	if len(callOrder) == 0 {
		return msgs
	}

	results := make([]fantasy.MessagePart, 0, len(callOrder))
	for _, id := range callOrder {
		if r, ok := resultByCall[id]; ok {
			results = append(results, toolResultPart(id, r))
			continue
		}
		results = append(results, interruptedResultPart(id))
	}
	return append(msgs, fantasy.Message{Role: fantasy.MessageRoleTool, Content: results})
}

// toolResultPart converts a core.ToolResult into a fantasy.ToolResultPart,
// using ToolResultOutputContentError for an error result (its media are
// ignored), ToolResultOutputContentMedia for a result whose first medium
// has loaded Data (further media are dropped with a note in Text; a
// provider takes one medium per result), and ToolResultOutputContentText
// otherwise.
func toolResultPart(callID string, r *core.ToolResult) fantasy.ToolResultPart {
	if r.IsError {
		return fantasy.ToolResultPart{
			ToolCallID: callID,
			Output:     fantasy.ToolResultOutputContentError{Error: errors.New(r.Output)},
		}
	}
	if len(r.Media) > 0 && r.Media[0].Data != nil {
		text := r.Output
		if extra := len(r.Media) - 1; extra > 0 {
			text += fmt.Sprintf(" [+%d more images omitted]", extra)
		}
		return fantasy.ToolResultPart{
			ToolCallID: callID,
			Output: fantasy.ToolResultOutputContentMedia{
				Data:      base64.StdEncoding.EncodeToString(r.Media[0].Data),
				MediaType: r.Media[0].MIME,
				Text:      text,
			},
		}
	}
	return fantasy.ToolResultPart{
		ToolCallID: callID,
		Output:     fantasy.ToolResultOutputContentText{Text: r.Output},
	}
}

// interruptedResultPart builds the synthesized error result for a
// ToolCallPart with no matching ToolResult.
func interruptedResultPart(callID string) fantasy.ToolResultPart {
	return fantasy.ToolResultPart{
		ToolCallID: callID,
		Output:     fantasy.ToolResultOutputContentError{Error: errors.New(interruptedResultText)},
	}
}

// convertTools converts core.ToolSpec into fantasy.FunctionTool, returning
// nil for an empty specs slice so an empty Call.Tools stays nil.
func convertTools(specs []core.ToolSpec) []fantasy.Tool {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]fantasy.Tool, len(specs))
	for i, s := range specs {
		tools[i] = fantasy.FunctionTool{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: s.Schema,
		}
	}
	return tools
}
