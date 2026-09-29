// Package ext defines the extension points every built-in and future
// plugin registers through: tools, permission hooks, context transforms,
// event subscribers, commands, keybinds, and provider factories. Registry
// collects them at startup; View is the read-only, frozen snapshot that
// runtime code depends on.
package ext

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// RunContext carries the identifiers and settings ambient to a single tool
// call, hook invocation, or context transform within a run.
type RunContext struct {
	SessionID core.SessionID
	RootID    core.SessionID
	MessageID core.MessageID
	Agent     core.Agent
	Model     core.ModelRef
	WorkDir   string
	Depth     int
	// Ancestors holds the permission rules of every agent above this one
	// in the subagent chain, root first. A tool call must pass each of
	// them as well as Agent.Permissions.
	Ancestors []core.PermissionRules
}

// Tool is an executable capability the model can call.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Concurrent() bool
	Run(ctx context.Context, rc RunContext, call core.ToolCall) (core.ToolResult, error)
}

// Subjecter is an optional Tool extension that derives the permission
// subject (what a call is really asking permission for, e.g. a shell
// command or a file path) from the call's RunContext and raw input. Tools
// that resolve relative paths (or other rc-dependent values) must use the
// same resolution rc gives Run, so Subject and Run can never disagree.
type Subjecter interface {
	Subject(rc RunContext, input json.RawMessage) string
}

// Verdict is a ToolHook's decision about whether a tool call may proceed.
type Verdict struct {
	Block  bool
	Reason string
}

// ToolHook observes and may intercept every tool call.
type ToolHook interface {
	// Before runs before the tool executes. It may rewrite the call and/or
	// block it. tool is resolved by the Runner, so hooks never need the
	// registry.
	Before(ctx context.Context, rc RunContext, tool Tool, call core.ToolCall) (core.ToolCall, Verdict, error)
	// After runs once the tool has produced a result, and may rewrite it.
	// It runs only when the tool's Run ran, including when Run returned an
	// error, panicked, or was cancelled. It does not run for calls that a
	// hook blocked, that name an unknown tool, or whose input was invalid.
	After(ctx context.Context, rc RunContext, tool Tool, call core.ToolCall, res core.ToolResult) core.ToolResult
}

// ContextTransform mutates an LLMRequest before it is sent, e.g. to inject
// skills or trim history. Transforms run in ascending Priority order.
type ContextTransform interface {
	Priority() int
	Transform(ctx context.Context, rc RunContext, req *core.LLMRequest) error
}

// EventSubscriber observes events published on the event.Bus.
type EventSubscriber interface {
	Handle(ctx context.Context, e event.Event)
}

// Command is a picker action: an entry the ctrl+p picker lists under
// "ext.<name>". jig has no slash commands; every Command is reached
// through the picker or a key bound to it via Keybind.
type Command interface {
	Name() string
	Description() string
	Run(ctx context.Context, args []string) error
}

// Keybind maps a key in a UI Mode to a Command name. When two Keybinds
// share the same Mode and Key, the later-registered one wins.
type Keybind struct {
	Mode    string
	Key     string
	Command string
}

// ProviderFactory constructs a core.LLM for a given provider Type.
type ProviderFactory interface {
	Type() string
	New(info core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error)
}

// ToolSource supplies tools whose set changes at runtime (MCP). The
// registry holds at most one; it is registered before Freeze like every
// other extension, but Tools is called on every model step and may return
// a different set each time. Implementations own their own locking and
// must not do I/O in Tools.
type ToolSource interface {
	Tools() []Tool
}

// ErrFrozen is returned by every Registry Add* method once Freeze has been
// called.
var ErrFrozen = errors.New("ext: registry is frozen")
