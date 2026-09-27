package core

import "context"

// SendRequest asks a ChatService to send a message in a session, creating
// the session if SessionID is empty.
type SendRequest struct {
	SessionID SessionID
	Agent     string
	Model     string
	Text      string
}

// SendResult is the outcome of a ChatService.Send call.
type SendResult struct {
	SessionID SessionID
	Message   Message
}

// ChatService is the port the UIs call to drive a conversation.
type ChatService interface {
	Send(ctx context.Context, req SendRequest) (SendResult, error)
	Cancel(id SessionID)
	Close(ctx context.Context) error
}

// SessionService is the port the UIs call to browse session history.
type SessionService interface {
	// List returns root sessions (no ParentID), newest first.
	List(ctx context.Context, limit int) ([]Session, error)
	Get(ctx context.Context, id SessionID) (Session, error)
	Messages(ctx context.Context, id SessionID) ([]Message, error)
	Compact(ctx context.Context, id SessionID) error
}

// ReplyKind is a user's decision on a pending permission request.
type ReplyKind string

const (
	ReplyOnce   ReplyKind = "once"
	ReplyAlways ReplyKind = "always"
	ReplyDeny   ReplyKind = "deny"
)

// PermissionReply is the UI's answer to a permission request.
type PermissionReply struct {
	Kind    ReplyKind
	Message string
}

// PermissionService is the port the UIs call to answer permission requests.
type PermissionService interface {
	Reply(requestID string, r PermissionReply) error
}
