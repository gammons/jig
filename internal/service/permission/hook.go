package permission

import (
	"context"
	"fmt"
	"sync"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// Hook is an ext.ToolHook that enforces permission rules: it evaluates
// Effective(rc.Agent.Permissions, cfg) for each call, blocks denied calls,
// and consults asker (remembering "always" answers per root session) for
// calls that ask. Hook is safe for concurrent Before calls.
type Hook struct {
	cfg   core.PermissionRules
	asker Asker

	mu     sync.Mutex
	grants map[core.SessionID]map[string][]string // root -> tool -> granted subjects ("" = whole tool)
}

// NewHook returns a Hook that evaluates against cfg and asks through
// asker.
func NewHook(cfg core.PermissionRules, asker Asker) *Hook {
	return &Hook{
		cfg:    cfg,
		asker:  asker,
		grants: make(map[core.SessionID]map[string][]string),
	}
}

// Before implements ext.ToolHook.
func (h *Hook) Before(ctx context.Context, rc ext.RunContext, tool ext.Tool, call core.ToolCall) (core.ToolCall, ext.Verdict, error) {
	subject := subjectOf(tool, call)
	rule := Effective(rc.Agent.Permissions, h.cfg)[call.Name]

	switch Evaluate(rule, subject) {
	case core.Allow:
		return call, ext.Verdict{}, nil
	case core.Deny:
		return call, ext.Verdict{Block: true, Reason: fmt.Sprintf("denied by permission rule for %s", call.Name)}, nil
	default: // core.Ask
		return h.ask(ctx, rc, call, subject)
	}
}

// After implements ext.ToolHook by returning res unchanged.
func (h *Hook) After(_ context.Context, _ ext.RunContext, _ ext.Tool, _ core.ToolCall, res core.ToolResult) core.ToolResult {
	return res
}

// subjectOf derives a call's permission subject from tool, when tool
// implements ext.Subjecter, or "" otherwise.
func subjectOf(tool ext.Tool, call core.ToolCall) string {
	s, ok := tool.(ext.Subjecter)
	if !ok {
		return ""
	}
	return s.Subject(call.Input)
}

// ask honors an existing session grant for call.Name/subject under rc's
// root session, or falls through to asker.Ask, recording a grant on
// ReplyAlways.
func (h *Hook) ask(ctx context.Context, rc ext.RunContext, call core.ToolCall, subject string) (core.ToolCall, ext.Verdict, error) {
	root := rootKey(rc)
	if h.granted(root, call.Name, subject) {
		return call, ext.Verdict{}, nil
	}

	reply, err := h.asker.Ask(ctx, Request{SessionID: rc.SessionID, Tool: call.Name, Subject: subject, Call: call})
	if err != nil {
		return call, ext.Verdict{}, err
	}

	switch reply.Kind {
	case core.ReplyAlways:
		h.grant(root, call.Name, subject)
		return call, ext.Verdict{}, nil
	case core.ReplyDeny:
		reason := "user denied"
		if reply.Message != "" {
			reason = "user denied: " + reply.Message
		}
		return call, ext.Verdict{Block: true, Reason: reason}, nil
	default: // core.ReplyOnce
		return call, ext.Verdict{}, nil
	}
}

// rootKey returns the session grants are keyed on: rc.RootID, falling back
// to rc.SessionID when RootID is empty.
func rootKey(rc ext.RunContext) core.SessionID {
	if rc.RootID != "" {
		return rc.RootID
	}
	return rc.SessionID
}

// granted reports whether root has a grant for tool covering subject: an
// exact subject match, or a "" grant meaning the whole tool.
func (h *Hook) granted(root core.SessionID, tool, subject string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, s := range h.grants[root][tool] {
		if s == "" || s == subject {
			return true
		}
	}
	return false
}

// grant records that root may use tool/subject without asking again.
func (h *Hook) grant(root core.SessionID, tool, subject string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.grants[root] == nil {
		h.grants[root] = make(map[string][]string)
	}
	h.grants[root][tool] = append(h.grants[root][tool], subject)
}
