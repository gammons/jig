package core

import (
	"context"
	"io"
)

// SendRequest asks a ChatService to send a message in a session, creating
// the session if SessionID is empty.
type SendRequest struct {
	SessionID SessionID
	Agent     string
	Model     string
	Text      string
	// Attachments is a list of file paths to attach to Text; a relative
	// path resolves against the ChatService's WorkDir.
	Attachments []string
}

// SendResult is the outcome of a ChatService.Send call.
type SendResult struct {
	SessionID SessionID
	Message   Message
}

// ChatService is the port the UIs call to drive a conversation.
type ChatService interface {
	Send(ctx context.Context, req SendRequest) (SendResult, error)
	// Compact returns ErrBusy while a run is in progress on id.
	Compact(ctx context.Context, id SessionID) error
	Cancel(id SessionID)
	Close(ctx context.Context) error
}

// SessionService is the port the UIs call to browse and manage sessions.
type SessionService interface {
	// List returns root sessions (no ParentID), newest first.
	List(ctx context.Context, limit int) ([]Session, error)
	// ListForCwd returns root sessions whose Cwd matches pathid.Key(cwd),
	// newest first.
	ListForCwd(ctx context.Context, cwd string, limit int) ([]Session, error)
	Get(ctx context.Context, id SessionID) (Session, error)
	Messages(ctx context.Context, id SessionID) ([]Message, error)
	Todos(ctx context.Context, id SessionID) ([]Todo, error)
	// Rename sets id's title to the trimmed, capped title. "" is an error.
	Rename(ctx context.Context, id SessionID, title string) error
	// Configure sets id's agent and/or model. "" leaves a field unchanged.
	Configure(ctx context.Context, id SessionID, agent, model string) error
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

// ProviderStatus pairs a catalog provider with whether its credentials
// are resolvable right now (an api_key in config, its env var set, or no
// key required at all).
type ProviderStatus struct {
	Info       ProviderInfo
	Configured bool
}

// CatalogService is the port the UIs call to list providers (with
// credential status) for the model picker.
type CatalogService interface {
	// Providers returns every catalog provider, sorted by ID.
	Providers() []ProviderStatus
}

// AgentService is the port the UIs call to list agents for the agent
// picker.
type AgentService interface {
	// Primary returns every non-hidden agent whose mode is primary or
	// all, build and plan first, then sorted by name.
	Primary() []Agent
}

// ProjectFile is one file under a project's workdir, as returned by
// ProjectService.Files.
type ProjectFile struct {
	// Path is workdir-relative and slash-separated.
	Path string
	// Modified reports whether git status sees the file as changed.
	Modified bool
}

// ProjectService is the port the UIs call to list and read project files,
// for the file picker (@ mentions) and tool-call details panes.
type ProjectService interface {
	// Files returns every eligible project file, gitignore-aware, at
	// most 20000.
	Files(ctx context.Context) ([]ProjectFile, error)
	// ReadFile returns path's bytes. A relative path resolves against
	// the workdir; the resolved path (symlinks followed) must fall
	// inside the workdir, the runtime's private spill dir, or the blob
	// store's directory, and be at most 10 MiB.
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// Branch returns the workdir's checked-out git branch (a detached
	// HEAD as its short SHA), or "" outside a git repository.
	Branch(ctx context.Context) (string, error)
}

// BlobService is the port the UIs call to open a stored attachment or
// screenshot blob.
type BlobService interface {
	// Open returns ref's bytes and their detected MIME type (via
	// http.DetectContentType).
	Open(ref string) ([]byte, string, error)
}

// ExecCommand has the method set of tea.ExecCommand, so ui can run a
// port's returned command through tea.Exec without importing os/exec.
type ExecCommand interface {
	Run() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

// EditorService is the port the UIs call to edit text in $VISUAL/$EDITOR.
type EditorService interface {
	// Edit writes text to a private temp file and returns the command
	// to open it in the user's editor, plus a result func that reads
	// the edited text back (and removes the temp file) after the
	// command exits.
	Edit(text string) (ExecCommand, func() (string, error), error)
}
