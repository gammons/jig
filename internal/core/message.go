// Package core holds jig's value types and ports: the data shapes and
// service interfaces shared across layers, with no I/O and no service
// logic of its own.
package core

import (
	"encoding/json"
	"time"
)

// SessionID identifies a Session.
type SessionID string

// MessageID identifies a Message.
type MessageID string

// Role distinguishes who authored a Message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// PartKind distinguishes the kinds of content that can appear in a Part.
type PartKind string

const (
	PartText       PartKind = "text"
	PartReasoning  PartKind = "reasoning"
	PartToolCall   PartKind = "tool_call"
	PartToolResult PartKind = "tool_result"
	PartCompaction PartKind = "compaction"
	PartAttachment PartKind = "attachment"
)

// MessageStatus tracks a Message's lifecycle as it streams in.
type MessageStatus string

const (
	StatusStreaming   MessageStatus = "streaming"
	StatusComplete    MessageStatus = "complete"
	StatusInterrupted MessageStatus = "interrupted"
	StatusFailed      MessageStatus = "failed"
)

// ToolCall is a model-requested invocation of a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the outcome of executing a ToolCall.
type ToolResult struct {
	CallID   string
	Name     string
	Output   string
	IsError  bool
	Metadata map[string]string
	Media    []Media
}

// Part is one piece of a Message's content.
type Part struct {
	Kind       PartKind
	Text       string
	Call       *ToolCall
	Result     *ToolResult
	Attachment *Attachment
}

// Usage tracks token counts for a single LLM exchange.
type Usage struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Message is one turn in a Session's history.
type Message struct {
	ID        MessageID
	SessionID SessionID
	Role      Role
	Agent     string
	Model     string
	Parts     []Part
	Usage     Usage
	CostUSD   float64
	Status    MessageStatus
	CreatedAt time.Time
}

// Todo is one item in an agent's todo list.
type Todo struct {
	Content string
	Status  string // "pending" | "in_progress" | "completed"
}
