// Package transcript projects a root session's stored messages and live
// events onto an ordered list of display blocks. It is pure: no I/O, and
// it imports only the standard library and internal/core.
package transcript

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// BlockID identifies a Block stably across updates: "u/<msgID>" for a user
// message, "m/<msgID>/<n>" for the n-th (from 0) text or reasoning block of
// an assistant message, the tool call ID for a tool or subagent block, and
// "n/<k>" for the k-th (from 0) notice.
type BlockID string

// Kind distinguishes what a Block displays.
type Kind int

const (
	KindUser Kind = iota
	KindText
	KindReasoning
	KindTool
	KindSubagent
	KindNotice
)

// ToolState is the lifecycle state of a tool or subagent block.
type ToolState string

const (
	StatePending   ToolState = "pending"
	StateAwaiting  ToolState = "awaiting-permission"
	StateRunning   ToolState = "running"
	StateOK        ToolState = "ok"
	StateError     ToolState = "error"
	StateDenied    ToolState = "denied"
	StateCancelled ToolState = "cancelled"
)

// Level is a notice's severity: info renders dim, error renders red.
type Level int

const (
	LevelInfo Level = iota
	LevelError
)

// Block is one displayable unit of a transcript.
type Block struct {
	ID          BlockID
	Version     int // starts at 1; +1 on every change
	Kind        Kind
	MessageID   core.MessageID
	Title       string           // notice: a headline ("compaction summary") when Text is a body
	Text        string           // user, text, reasoning, notice
	Attachments []string         // user: attachment paths
	Call        *core.ToolCall   // tool, subagent
	Result      *core.ToolResult // tool, subagent
	State       ToolState        // tool, subagent
	Permission  *PendingPermission
	Sub         *Subagent // subagent
	Level       Level     // notice
	Streaming   bool      // text/reasoning block still receiving deltas
}

// Subagent is the state of a task call's child session.
type Subagent struct {
	Child       core.SessionID
	Agent       string
	Description string
	Tools       int
	Current     string
}

// PendingPermission is a permission request awaiting the user's reply.
type PendingPermission struct {
	RequestID string
	Session   core.SessionID
	Tool      string
	Subject   string
	Call      core.ToolCall
	Block     BlockID
	Subagent  string
}

// ChangeKind says whether a changed file was created or modified.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
)

// FileChange is a file a successful write or edit touched.
type FileChange struct {
	Path string
	Kind ChangeKind
}

// Notice texts and tool names the projection recognizes.
const (
	noticeCancelled  = "cancelled"
	noticeRunFailed  = "run failed"
	titleCompaction  = "compaction summary"
	cancelledOutput  = "cancelled"
	userDenied       = "user denied"
	ruleDeniedPrefix = "denied by permission rule for "
	taskTool         = "task"
	maxStepsPattern  = `^\[stopped: reached max_steps \(\d+\)\]$`
	taskResultPrefix = `<task_result session_id="`
)

// clone returns a deep copy of b, so callers cannot mutate the projection.
func (b *Block) clone() Block {
	c := *b
	if b.Attachments != nil {
		c.Attachments = append([]string(nil), b.Attachments...)
	}
	if b.Call != nil {
		call := *b.Call
		c.Call = &call
	}
	if b.Result != nil {
		res := *b.Result
		c.Result = &res
	}
	if b.Permission != nil {
		perm := *b.Permission
		c.Permission = &perm
	}
	if b.Sub != nil {
		sub := *b.Sub
		c.Sub = &sub
	}
	return c
}

// stateOf is the final state of a finished tool call. Denials and
// cancellations are recognized only on error results, the only way the
// executor reports them.
func stateOf(r core.ToolResult) ToolState {
	switch {
	case !r.IsError:
		return StateOK
	case r.Output == cancelledOutput:
		return StateCancelled
	case r.Output == userDenied, strings.HasPrefix(r.Output, userDenied+": "),
		strings.HasPrefix(r.Output, ruleDeniedPrefix):
		return StateDenied
	}
	return StateError
}

// isMaxStepsNotice reports whether text is the runner's max-steps notice
// (R18), ignoring surrounding whitespace.
func isMaxStepsNotice(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "[stopped: ") {
		return false // fast path: skip compiling the pattern for ordinary deltas
	}
	ok, _ := regexp.MatchString(maxStepsPattern, text)
	return ok
}

// subagentOf reads a task call's agent, description and, for a resumed
// task, its child session from the call's input. Malformed input yields an
// empty Subagent.
func subagentOf(call core.ToolCall) *Subagent {
	var in struct {
		Agent       string `json:"agent"`
		Description string `json:"description"`
		SessionID   string `json:"session_id"`
	}
	_ = json.Unmarshal(call.Input, &in)
	return &Subagent{Child: core.SessionID(in.SessionID), Agent: in.Agent, Description: in.Description}
}

// childOf parses the child session ID from a task result's
// `<task_result session_id="…">` header, or returns "".
func childOf(output string) core.SessionID {
	rest, ok := strings.CutPrefix(output, taskResultPrefix)
	if !ok {
		return ""
	}
	id, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return core.SessionID(id)
}
