package opencode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gammons/jig/internal/core"
)

// assistantData is the shape of an "assistant" message's data.
type assistantData struct {
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Agent   string        `json:"agent"`
	Model   sessionModel  `json:"model"`
	Content []contentItem `json:"content"`
	Finish  string        `json:"finish"`
	Cost    float64       `json:"cost"`
	Tokens  struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// contentItem is one entry of an assistant message's content[]: a
// "text", "reasoning", or "tool" item.
type contentItem struct {
	Type  string    `json:"type"`
	Text  string    `json:"text"`
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	State toolState `json:"state"`
}

// toolState is a "tool" content item's state.
type toolState struct {
	Status  string            `json:"status"`
	Input   json.RawMessage   `json:"input"`
	Content []toolContentItem `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
	Metadata struct {
		SessionID string `json:"sessionId"`
	} `json:"metadata"`
}

// toolContentItem is one entry of a tool's state.content[]: a "text" or
// "file" item.
type toolContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URI  string `json:"uri"`
}

// addAssistant translates an "assistant" message into a RoleAssistant
// core.Message, per spec §5.3.
func (t *translator) addAssistant(m messageRow) {
	var data assistantData
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		t.skip(m, err)
		return
	}

	var parts []core.Part
	for _, item := range data.Content {
		switch item.Type {
		case "text":
			parts = append(parts, core.Part{Kind: core.PartText, Text: item.Text})
		case "reasoning":
			if item.Text == "" {
				continue
			}
			parts = append(parts, core.Part{Kind: core.PartReasoning, Text: item.Text})
		case "tool":
			parts = append(parts, t.translateToolCall(item)...)
		}
	}

	t.msgs = append(t.msgs, core.Message{
		ID:        core.MessageID(m.ID),
		SessionID: t.sess.ID,
		Role:      core.RoleAssistant,
		Agent:     normalizeAgent(t.agents, t.sess.ParentID != "", data.Agent),
		Model:     modelString(data.Model.ProviderID, data.Model.ID),
		Parts:     parts,
		Usage: core.Usage{
			Input:      data.Tokens.Input,
			Output:     data.Tokens.Output + data.Tokens.Reasoning,
			CacheRead:  data.Tokens.Cache.Read,
			CacheWrite: data.Tokens.Cache.Write,
		},
		CostUSD:   data.Cost,
		Status:    assistantStatus(data),
		CreatedAt: time.UnixMilli(m.Created),
	})
}

// assistantStatus derives a Message's Status from its assistant data, per
// spec §5.3.
func assistantStatus(data assistantData) core.MessageStatus {
	switch {
	case data.Finish == "error":
		return core.StatusFailed
	case data.Time.Completed == 0:
		return core.StatusInterrupted
	default:
		return core.StatusComplete
	}
}

// translateToolCall translates one "tool" content item into a PartToolCall
// and, when the tool reached a terminal status, the PartToolResult right
// after it (spec §5.3).
func (t *translator) translateToolCall(item contentItem) []core.Part {
	jigName, jigInput, translated := translateCall(item.Name, item.State.Input)
	if !translated {
		t.stats.UntranslatedTools[item.Name]++
	}

	call := core.Part{Kind: core.PartToolCall, Call: &core.ToolCall{
		ID:    item.ID,
		Name:  jigName,
		Input: jigInput,
	}}

	switch item.State.Status {
	case "completed", "error":
		return []core.Part{call, t.translateToolResult(item, jigName)}
	default:
		return []core.Part{call}
	}
}

// translateToolResult builds the PartToolResult for a completed or errored
// tool call, per spec §5.3.
func (t *translator) translateToolResult(item contentItem, jigName string) core.Part {
	var texts []string
	var media []core.Media
	var notes []string
	for _, c := range item.State.Content {
		switch c.Type {
		case "text":
			texts = append(texts, c.Text)
		case "file":
			if m, ok := decodeDataURI(c.URI); ok {
				media = append(media, m)
			} else {
				notes = append(notes, fmt.Sprintf("[file not imported: %s]", uriMIME(c.URI)))
			}
		}
	}

	isError := item.State.Status == "error"
	var output string
	if isError {
		output = "tool error"
		if item.State.Error != nil && item.State.Error.Message != "" {
			output = item.State.Error.Message
		}
		if joined := strings.Join(texts, "\n"); joined != "" {
			output += "\n" + joined
		}
	} else {
		output = strings.Join(texts, "\n")
	}
	for _, n := range notes {
		output += "\n" + n
	}

	if jigName == "task" && !isError {
		output = translateTaskResult(output, item.State.Metadata.SessionID)
	}

	return core.Part{Kind: core.PartToolResult, Result: &core.ToolResult{
		CallID:  item.ID,
		Name:    jigName,
		Output:  output,
		IsError: isError,
		Media:   media,
	}}
}

// decodeDataURI decodes a "data:<mime>;base64,<data>" URI whose mime is an
// image type. ok is false for a non-image mime or a malformed/undecodable
// URI.
func decodeDataURI(uri string) (m core.Media, ok bool) {
	mime := uriMIME(uri)
	if !strings.HasPrefix(mime, "image/") {
		return core.Media{}, false
	}
	_, b64, found := strings.Cut(uri, ",")
	if !found {
		return core.Media{}, false
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return core.Media{}, false
	}
	return core.Media{MIME: mime, Data: decoded}, true
}

// uriMIME extracts the mime from a "data:<mime>;base64,..." URI, "" if it
// doesn't look like one.
func uriMIME(uri string) string {
	rest, ok := strings.CutPrefix(uri, "data:")
	if !ok {
		return ""
	}
	mime, _, _ := strings.Cut(rest, ";")
	return mime
}
