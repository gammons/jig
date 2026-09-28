// Package event defines the typed events the UI observes as a run
// progresses, and Bus, the in-process publisher that fans them out to
// subscribers.
package event

import "github.com/gammons/jig/internal/core"

// Event is anything that can travel over the Bus. Every concrete event
// embeds Base, which supplies Session and Root.
type Event interface {
	Session() core.SessionID
	Root() core.SessionID
}

// Base identifies the Session an Event belongs to, and the root session of
// the run that produced it. All events embed it by value.
type Base struct {
	SessionID core.SessionID
	RootID    core.SessionID
}

// Session returns the ID of the session the event belongs to.
func (b Base) Session() core.SessionID { return b.SessionID }

// Root returns the root session of the run that produced the event:
// RootID, or SessionID when RootID is empty.
func (b Base) Root() core.SessionID {
	if b.RootID != "" {
		return b.RootID
	}
	return b.SessionID
}

// SessionCreated announces that a new Session has started.
type SessionCreated struct {
	Base
	Info core.Session
}

// MessageStarted announces that an assistant message has begun streaming.
type MessageStarted struct {
	Base
	MessageID core.MessageID
	Agent     string
	Model     string
}

// TextDelta carries a chunk of assistant text as it streams in. Adjacent
// TextDelta events for the same message may be merged by the Bus while a
// subscriber lags.
type TextDelta struct {
	Base
	MessageID core.MessageID
	Text      string
}

// ReasoningDelta carries a chunk of assistant reasoning as it streams in.
// Adjacent ReasoningDelta events for the same message may be merged by the
// Bus while a subscriber lags.
type ReasoningDelta struct {
	Base
	MessageID core.MessageID
	Text      string
}

// ToolCallStarted announces that the model has requested a tool call.
type ToolCallStarted struct {
	Base
	MessageID core.MessageID
	Call      core.ToolCall
}

// ToolCallFinished announces the outcome of a tool call.
type ToolCallFinished struct {
	Base
	MessageID core.MessageID
	Result    core.ToolResult
}

// PermissionRequested announces that a tool call is waiting on a permission
// decision.
type PermissionRequested struct {
	Base
	RequestID string
	Tool      string
	Subject   string
	Call      core.ToolCall
}

// PermissionResolved announces the outcome of a permission request.
type PermissionResolved struct {
	Base
	RequestID string
	Reply     core.PermissionReply
}

// SubagentSpawned announces that a subagent session was created under a
// parent session. CallID is the parent's task tool call that spawned it,
// which disambiguates parallel task calls.
type SubagentSpawned struct {
	Base
	Child       core.SessionID
	Agent       string
	Description string
	CallID      string
}

// TodosUpdated announces the current state of an agent's todo list.
type TodosUpdated struct {
	Base
	Todos []core.Todo
}

// RunFinished announces that a run completed successfully.
type RunFinished struct {
	Base
	MessageID core.MessageID
	Usage     core.Usage
	CostUSD   float64
}

// StepFinished announces that one model step (an assistant message, with
// any tool calls it made and their results) completed and was saved. It is
// published for every completed step of a run, never for an aborted one.
type StepFinished struct {
	Base
	MessageID core.MessageID
	Usage     core.Usage
	CostUSD   float64
}

// SessionUpdated announces that a Session's stored fields (title, agent,
// model, UpdatedAt) changed.
type SessionUpdated struct {
	Base
	Info core.Session
}

// RunFailed announces that a run ended in an error.
type RunFailed struct {
	Base
	Err string
}

// Publisher is the narrow interface services depend on to emit events,
// without needing to know about subscriptions.
type Publisher interface {
	Publish(Event)
}
