package permission

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// Hook is an ext.ToolHook that enforces permission rules: it evaluates
// Effective(rc.Agent.Permissions, cfg) and Effective(ancestor, cfg) for
// every rc.Ancestors entry, takes the most restrictive result, blocks denied calls,
// and consults asker (remembering "always" answers per root session) for
// calls that ask. Hook is safe for concurrent Before calls.
type Hook struct {
	cfg    core.PermissionRules
	preset core.PermissionRules // relaxes default asks only; see WithPreset
	asker  Asker

	mu     sync.Mutex
	grants map[core.SessionID]map[string][]string // root -> tool -> granted subjects ("" = whole tool)
}

// HookOption configures a Hook.
type HookOption func(*Hook)

// WithPreset makes the Hook consult preset (e.g. AgentBrowserPreset) for
// a call whose action is an ask that came from the tool's Default (no
// user pattern matched): a matching preset allow turns it into an allow,
// subject to the bash metacharacter downgrade. The preset never overrides
// a deny or a user pattern, and does not affect tool visibility.
func WithPreset(preset core.PermissionRules) HookOption {
	return func(h *Hook) { h.preset = preset }
}

// NewHook returns a Hook that evaluates against cfg and asks through
// asker.
func NewHook(cfg core.PermissionRules, asker Asker, opts ...HookOption) *Hook {
	h := &Hook{
		cfg:    cfg,
		asker:  asker,
		grants: make(map[core.SessionID]map[string][]string),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Before implements ext.ToolHook.
func (h *Hook) Before(ctx context.Context, rc ext.RunContext, tool ext.Tool, call core.ToolCall) (core.ToolCall, ext.Verdict, error) {
	subject := subjectOf(tool, rc, call)

	switch h.decide(rc, call.Name, subject) {
	case core.Allow:
		return call, ext.Verdict{}, nil
	case core.Deny:
		return call, ext.Verdict{Block: true, Reason: fmt.Sprintf("denied by permission rule for %s", call.Name)}, nil
	default: // core.Ask
		return h.ask(ctx, rc, call, subject)
	}
}

// decide evaluates tool/subject against the current agent's rules and
// every ancestor agent's rules (each merged over cfg), returning the most
// restrictive action: Deny > Ask > Allow. A subagent can therefore never
// do what an agent above it could not.
func (h *Hook) decide(rc ext.RunContext, tool, subject string) core.Action {
	worst := h.decideOne(rc.Agent.Permissions, tool, subject)
	for _, anc := range rc.Ancestors {
		if a := h.decideOne(anc, tool, subject); actionRank(a) > actionRank(worst) {
			worst = a
		}
	}
	return worst
}

// decideOne evaluates tool/subject under Effective(agent, cfg). A bash
// allow that came from a pattern (not the tool's default) is downgraded to
// Ask when the command contains shell metacharacters, since a pattern such
// as "git status*" would otherwise also allow "git status; rm -rf x".
// An ask from the tool's Default may be relaxed by h.preset (see
// WithPreset), under the same downgrade.
func (h *Hook) decideOne(agent core.PermissionRules, tool, subject string) core.Action {
	rule := Effective(agent, h.cfg)[tool]
	action, fromPattern := evaluate(rule, subject)
	if action == core.Ask && !fromPattern && rule.Default != core.Deny {
		if pa, matched := evaluate(core.Rule{Patterns: h.preset[tool].Patterns}, subject); matched && pa == core.Allow {
			action, fromPattern = core.Allow, true
		}
	}
	if tool == "bash" && action == core.Allow && fromPattern && hasShellMeta(subject) {
		return core.Ask
	}
	return action
}

// hasShellMeta reports whether cmd contains a character or sequence that
// can chain, substitute, expand, or redirect shell commands.
func hasShellMeta(cmd string) bool {
	return strings.ContainsAny(cmd, ";&|`><\n$")
}

// After implements ext.ToolHook by returning res unchanged.
func (h *Hook) After(_ context.Context, _ ext.RunContext, _ ext.Tool, _ core.ToolCall, res core.ToolResult) core.ToolResult {
	return res
}

// subjectOf derives a call's permission subject from tool, when tool
// implements ext.Subjecter, or "" otherwise.
func subjectOf(tool ext.Tool, rc ext.RunContext, call core.ToolCall) string {
	s, ok := tool.(ext.Subjecter)
	if !ok {
		return ""
	}
	return s.Subject(rc, call.Input)
}

// ask honors an existing session grant for call.Name/subject under rc's
// root session, or falls through to asker.Ask, recording a grant on
// ReplyAlways.
func (h *Hook) ask(ctx context.Context, rc ext.RunContext, call core.ToolCall, subject string) (core.ToolCall, ext.Verdict, error) {
	root := rootKey(rc)
	if h.granted(root, call.Name, subject) {
		return call, ext.Verdict{}, nil
	}

	reply, err := h.asker.Ask(ctx, Request{SessionID: rc.SessionID, RootID: root, Tool: call.Name, Subject: subject, Call: call})
	if err != nil {
		return call, ext.Verdict{}, err
	}

	switch reply.Kind {
	case core.ReplyAlways:
		h.grant(root, call.Name, subject)
		return call, ext.Verdict{}, nil
	case core.ReplyOnce:
		return call, ext.Verdict{}, nil
	case core.ReplyDeny:
		reason := "user denied"
		if reply.Message != "" {
			reason = "user denied: " + reply.Message
		}
		return call, ext.Verdict{Block: true, Reason: reason}, nil
	default:
		// Fail closed: an unrecognized Kind (including the zero value)
		// must never pass a call. Never surface reply.Message here, since
		// it came from an untrusted/invalid reply.
		return call, ext.Verdict{Block: true, Reason: "user denied"}, nil
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
