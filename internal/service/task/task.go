// Package task implements the "task" tool: it launches a subagent session
// and drives it to completion, so a primary agent can delegate autonomous
// work to a purpose-built subagent.
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
)

// MaxDepth bounds how many levels deep subagents may spawn subagents of
// their own.
const MaxDepth = 3

// Sessions creates and looks up subagent sessions.
type Sessions interface {
	CreateChild(ctx context.Context, parent core.SessionID, agent string, model core.ModelRef, title string) (core.Session, error)
	Get(ctx context.Context, id core.SessionID) (core.Session, error)
}

// Agents resolves agent definitions and their models.
type Agents interface {
	Get(name string) (core.Agent, bool)
	Subagents() []core.Agent
	ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
}

// Runner drives a session's turn to completion.
type Runner interface {
	Run(ctx context.Context, rc ext.RunContext, text string, atts ...core.Attachment) (core.Message, error)
}

// taskInput is the JSON input task accepts.
type taskInput struct {
	Agent       string `json:"agent"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	SessionID   string `json:"session_id,omitempty"`
}

// taskTool implements ext.Tool for the "task" tool.
type taskTool struct {
	sessions Sessions
	agents   Agents
	runner   Runner
	pub      event.Publisher
}

// New returns the "task" tool, backed by s, a, and r, publishing
// event.SubagentSpawned on pub whenever a subagent is engaged.
func New(s Sessions, a Agents, r Runner, pub event.Publisher) ext.Tool {
	return &taskTool{sessions: s, agents: a, runner: r, pub: pub}
}

func (t *taskTool) Name() string { return "task" }

// Description is built on each call from the currently available
// subagents, so it always reflects config/markdown-defined agents too.
func (t *taskTool) Description() string {
	var b strings.Builder
	b.WriteString("Launch a subagent to handle a task autonomously.\n\n")
	b.WriteString("Available agents:\n")
	for _, a := range t.agents.Subagents() {
		fmt.Fprintf(&b, "- %s: %s\n", a.Name, a.Description)
	}
	return b.String()
}

func (t *taskTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent":       map[string]any{"type": "string", "description": "The name of the subagent to run."},
			"description": map[string]any{"type": "string", "description": "A short description of the task, used as the session title."},
			"prompt":      map[string]any{"type": "string", "description": "The task for the subagent to perform."},
			"session_id":  map[string]any{"type": "string", "description": "An existing subagent session's ID, to continue it instead of starting a new one."},
		},
		"required": []string{"agent", "description", "prompt"},
	}
}

func (t *taskTool) Concurrent() bool { return true }

// Run implements ext.Tool.
func (t *taskTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	in, errMsg := parseTaskInput(call.Input)
	if errMsg != "" {
		return core.ToolError(call, errMsg), nil
	}

	if rc.Depth >= MaxDepth {
		return core.ToolError(call, fmt.Sprintf("subagent depth limit (%d) reached", MaxDepth)), nil
	}

	sub, ok := t.agents.Get(in.Agent)
	if !ok || !isSubagent(t.agents.Subagents(), in.Agent) {
		return core.ToolError(call, unknownAgentMsg(in.Agent, t.agents.Subagents())), nil
	}

	model, err := t.agents.ResolveModel(sub, rc.Model, core.ModelRef{})
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}

	childID, errMsg, err := t.resolveChild(ctx, rc, in, model)
	if err != nil {
		return core.ToolResult{}, err
	}
	if errMsg != "" {
		return core.ToolError(call, errMsg), nil
	}

	t.pub.Publish(event.SubagentSpawned{
		Base:        event.Base{SessionID: rc.SessionID, RootID: rc.RootID},
		Child:       childID,
		Agent:       in.Agent,
		Description: in.Description,
		CallID:      call.ID,
	})

	childRC := ext.RunContext{
		SessionID: childID,
		RootID:    rootID(rc),
		Agent:     sub,
		Model:     model,
		WorkDir:   rc.WorkDir,
		Depth:     rc.Depth + 1,
		Ancestors: append(slices.Clone(rc.Ancestors), rc.Agent.Permissions),
	}
	msg, err := t.runner.Run(ctx, childRC, in.Prompt)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return core.ToolResult{}, ctxErr
		}
		return core.ToolError(call, wrapResult(childID, fmt.Sprintf("error: %v", err))), nil
	}

	return core.ToolOK(call, wrapResult(childID, joinText(msg))), nil
}

// resolveChild returns the session to run: an existing subagent session
// named by in.SessionID (validated as a child of rc.SessionID), or a freshly
// created one. errMsg is set (with a nil error) for a validation failure;
// err is set only for a Go-level failure such as a store error.
func (t *taskTool) resolveChild(ctx context.Context, rc ext.RunContext, in taskInput, model core.ModelRef) (core.SessionID, string, error) {
	if in.SessionID == "" {
		sess, err := t.sessions.CreateChild(ctx, rc.SessionID, in.Agent, model, in.Description)
		if err != nil {
			return "", err.Error(), nil
		}
		return sess.ID, "", nil
	}

	sess, err := t.sessions.Get(ctx, core.SessionID(in.SessionID))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", "", ctxErr
		}
		return "", notSubagentMsg(in.SessionID), nil
	}
	if sess.ParentID != rc.SessionID {
		return "", notSubagentMsg(in.SessionID), nil
	}
	return sess.ID, "", nil
}

// notSubagentMsg is the uniform denial for a session_id that either does
// not exist or is not a subagent session of the calling session: callers
// must not be able to distinguish "not found" from "not yours" by error
// text.
func notSubagentMsg(id string) string {
	return fmt.Sprintf("session %q is not a subagent session of this session", id)
}

// rootID returns rc.RootID, falling back to rc.SessionID when unset.
func rootID(rc ext.RunContext) core.SessionID {
	if rc.RootID != "" {
		return rc.RootID
	}
	return rc.SessionID
}

// parseTaskInput unmarshals raw and checks its required fields, returning
// an error message ("" if in is valid).
func parseTaskInput(raw json.RawMessage) (taskInput, string) {
	var in taskInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, fmt.Sprintf("invalid input: %v", err)
	}
	switch {
	case in.Agent == "":
		return in, "agent is required"
	case in.Description == "":
		return in, "description is required"
	case in.Prompt == "":
		return in, "prompt is required"
	}
	return in, ""
}

// isSubagent reports whether name is among subs.
func isSubagent(subs []core.Agent, name string) bool {
	for _, s := range subs {
		if s.Name == name {
			return true
		}
	}
	return false
}

// unknownAgentMsg builds the "unknown subagent" error, listing subs' names
// sorted and comma-separated.
func unknownAgentMsg(name string, subs []core.Agent) string {
	names := make([]string, len(subs))
	for i, s := range subs {
		names[i] = s.Name
	}
	sort.Strings(names)
	return fmt.Sprintf("unknown subagent %q; available: %s", name, strings.Join(names, ", "))
}

// joinText joins m's PartText parts with "\n".
func joinText(m core.Message) string {
	var parts []string
	for _, p := range m.Parts {
		if p.Kind == core.PartText {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// wrapResult wraps body in the task_result tag naming the child session.
func wrapResult(id core.SessionID, body string) string {
	return fmt.Sprintf("<task_result session_id=%q>\n%s\n</task_result>", id, body)
}
