// Package llm adapts jig's core.LLM port onto charm.land/fantasy: it
// converts requests and streamed responses between the two shapes, maps
// fantasy's provider errors onto core.LLMError, and resolves a
// core.ModelRef into a credentialed core.LLM through Source.
package llm

import (
	"errors"

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

// convertUserMessage joins every text part of m into one fantasy user
// message.
func convertUserMessage(m core.Message) fantasy.Message {
	var text string
	for _, p := range m.Parts {
		if p.Kind == core.PartText {
			text += p.Text
		}
	}
	return fantasy.NewUserMessage(text)
}

// convertAssistantMessage splits m's parts into an assistant message
// (text, reasoning, and tool-call parts) and, if it contains any tool
// calls, a following tool-role message carrying one ToolResultPart per
// call: the call's real result if any, else a synthesized "interrupted"
// error result. An assistant message with no content parts at all (e.g. a
// run interrupted before it produced anything) is dropped: Anthropic and
// friends reject empty message content.
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
				Input:      string(p.Call.Input),
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
// using ToolResultOutputContentError for an error result and
// ToolResultOutputContentText otherwise.
func toolResultPart(callID string, r *core.ToolResult) fantasy.ToolResultPart {
	if r.IsError {
		return fantasy.ToolResultPart{
			ToolCallID: callID,
			Output:     fantasy.ToolResultOutputContentError{Error: errors.New(r.Output)},
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
