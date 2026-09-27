package permission

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// fakeTool is a minimal ext.Tool. When subjecter is non-nil it also
// implements ext.Subjecter, deriving the subject from that func.
type fakeTool struct {
	name      string
	subjecter func(input json.RawMessage) string
}

func (f fakeTool) Name() string           { return f.name }
func (f fakeTool) Description() string    { return "" }
func (f fakeTool) Schema() map[string]any { return nil }
func (f fakeTool) Concurrent() bool       { return false }
func (f fakeTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}

type subjecterTool struct {
	fakeTool
}

func (s subjecterTool) Subject(_ ext.RunContext, input json.RawMessage) string {
	return s.subjecter(input)
}

// scriptedAsker returns a fixed reply/error for every Ask call, and records
// the requests it received.
type scriptedAsker struct {
	reply    core.PermissionReply
	err      error
	requests []Request
}

func (s *scriptedAsker) Ask(ctx context.Context, req Request) (core.PermissionReply, error) {
	s.requests = append(s.requests, req)
	return s.reply, s.err
}

func rc(session, root core.SessionID, agentPerms core.PermissionRules) ext.RunContext {
	return ext.RunContext{
		SessionID: session,
		RootID:    root,
		Agent:     core.Agent{Name: "build", Permissions: agentPerms},
	}
}

func TestHook_AllowDenyAsk(t *testing.T) {
	cfg := core.PermissionRules{
		"read":  {Default: core.Allow},
		"bash":  {Default: core.Deny},
		"write": {Default: core.Ask},
	}

	t.Run("allow passes without asking", func(t *testing.T) {
		asker := &scriptedAsker{}
		h := NewHook(cfg, asker)
		tool := fakeTool{name: "read"}
		call := core.ToolCall{ID: "1", Name: "read"}
		_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if v.Block {
			t.Errorf("Verdict.Block = true, want false")
		}
		if len(asker.requests) != 0 {
			t.Errorf("asker was called %d times, want 0", len(asker.requests))
		}
	})

	t.Run("deny blocks with reason", func(t *testing.T) {
		asker := &scriptedAsker{}
		h := NewHook(cfg, asker)
		tool := fakeTool{name: "bash"}
		call := core.ToolCall{ID: "1", Name: "bash"}
		_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if !v.Block {
			t.Fatal("Verdict.Block = false, want true")
		}
		want := "denied by permission rule for bash"
		if v.Reason != want {
			t.Errorf("Verdict.Reason = %q, want %q", v.Reason, want)
		}
	})

	t.Run("ask calls the asker and passes on ReplyOnce", func(t *testing.T) {
		asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyOnce}}
		h := NewHook(cfg, asker)
		tool := fakeTool{name: "write"}
		call := core.ToolCall{ID: "1", Name: "write"}
		_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if v.Block {
			t.Errorf("Verdict.Block = true, want false")
		}
		if len(asker.requests) != 1 {
			t.Fatalf("asker was called %d times, want 1", len(asker.requests))
		}
	})
}

func TestHook_AlwaysGrantScopedToRoot(t *testing.T) {
	cfg := core.PermissionRules{"write": {Default: core.Ask}}
	asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyAlways}}
	h := NewHook(cfg, asker)
	tool := fakeTool{name: "write"}
	call := core.ToolCall{ID: "1", Name: "write"}

	// First call under root A: asker is consulted, and grants for the
	// future.
	_, v, err := h.Before(context.Background(), rc("a", "rootA", nil), tool, call)
	if err != nil || v.Block {
		t.Fatalf("Before under rootA: v=%+v err=%v", v, err)
	}
	if len(asker.requests) != 1 {
		t.Fatalf("asker calls = %d, want 1", len(asker.requests))
	}

	// Second call, still under root A (different session id, e.g. a child
	// run): the grant is honored without asking again.
	_, v, err = h.Before(context.Background(), rc("child-of-a", "rootA", nil), tool, call)
	if err != nil || v.Block {
		t.Fatalf("Before under rootA child: v=%+v err=%v", v, err)
	}
	if len(asker.requests) != 1 {
		t.Fatalf("asker calls after grant = %d, want 1 (grant should have been used)", len(asker.requests))
	}

	// A call under a different root must not see the grant.
	_, v, err = h.Before(context.Background(), rc("b", "rootB", nil), tool, call)
	if err != nil || v.Block {
		t.Fatalf("Before under rootB: v=%+v err=%v", v, err)
	}
	if len(asker.requests) != 2 {
		t.Fatalf("asker calls under rootB = %d, want 2 (grant must not cross roots)", len(asker.requests))
	}
}

func TestHook_DenyMessagePropagates(t *testing.T) {
	cfg := core.PermissionRules{"write": {Default: core.Ask}}
	tool := fakeTool{name: "write"}
	call := core.ToolCall{ID: "1", Name: "write"}

	t.Run("with a message", func(t *testing.T) {
		asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyDeny, Message: "not today"}}
		h := NewHook(cfg, asker)
		_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if !v.Block {
			t.Fatal("Verdict.Block = false, want true")
		}
		want := "user denied: not today"
		if v.Reason != want {
			t.Errorf("Verdict.Reason = %q, want %q", v.Reason, want)
		}
	})

	t.Run("without a message", func(t *testing.T) {
		asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyDeny}}
		h := NewHook(cfg, asker)
		_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if !v.Block {
			t.Fatal("Verdict.Block = false, want true")
		}
		want := "user denied"
		if v.Reason != want {
			t.Errorf("Verdict.Reason = %q, want %q", v.Reason, want)
		}
	})
}

func TestHook_CtxErrorFromAskerIsReturned(t *testing.T) {
	cfg := core.PermissionRules{"write": {Default: core.Ask}}
	asker := &scriptedAsker{err: context.Canceled}
	h := NewHook(cfg, asker)
	tool := fakeTool{name: "write"}
	call := core.ToolCall{ID: "1", Name: "write"}

	_, _, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Before err = %v, want context.Canceled", err)
	}
}

func TestHook_UsesSubjecterAndAgentPermissions(t *testing.T) {
	cfg := core.PermissionRules{}
	asker := &scriptedAsker{}
	h := NewHook(cfg, asker)
	tool := subjecterTool{fakeTool{name: "bash", subjecter: func(input json.RawMessage) string { return "rm -rf /" }}}
	call := core.ToolCall{ID: "1", Name: "bash", Input: []byte(`{}`)}

	agentPerms := core.PermissionRules{"bash": {Default: core.Deny}}
	_, v, err := h.Before(context.Background(), rc("s1", "s1", agentPerms), tool, call)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if !v.Block {
		t.Fatal("Verdict.Block = false, want true (agent permission overrides default allow)")
	}
	if len(asker.requests) != 0 {
		t.Fatalf("asker calls = %d, want 0", len(asker.requests))
	}
}

func TestHook_UnknownReplyKindBlocks(t *testing.T) {
	cfg := core.PermissionRules{"write": {Default: core.Ask}}
	tool := fakeTool{name: "write"}
	call := core.ToolCall{ID: "1", Name: "write"}

	for _, kind := range []core.ReplyKind{"", "bogus"} {
		t.Run(string(kind), func(t *testing.T) {
			// A message is set to confirm it is ignored: fail-closed
			// blocks with a bare "user denied", never leaking whatever
			// the unrecognized reply happened to carry.
			asker := &scriptedAsker{reply: core.PermissionReply{Kind: kind, Message: "should be ignored"}}
			h := NewHook(cfg, asker)
			_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
			if err != nil {
				t.Fatalf("Before: %v", err)
			}
			if !v.Block {
				t.Fatal("Verdict.Block = false, want true (fail closed on unknown reply kind)")
			}
			want := "user denied"
			if v.Reason != want {
				t.Errorf("Verdict.Reason = %q, want %q", v.Reason, want)
			}
		})
	}
}

func TestHook_RootKeyFallsBackToSessionID(t *testing.T) {
	cfg := core.PermissionRules{"write": {Default: core.Ask}}
	asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyAlways}}
	h := NewHook(cfg, asker)
	tool := fakeTool{name: "write"}
	call := core.ToolCall{ID: "1", Name: "write"}

	// RootID is empty on both calls, so grants must key on SessionID.
	_, v, err := h.Before(context.Background(), rc("s1", "", nil), tool, call)
	if err != nil || v.Block {
		t.Fatalf("Before first call: v=%+v err=%v", v, err)
	}
	if len(asker.requests) != 1 {
		t.Fatalf("asker calls = %d, want 1", len(asker.requests))
	}

	// Same SessionID, RootID still empty: grant must be honored.
	_, v, err = h.Before(context.Background(), rc("s1", "", nil), tool, call)
	if err != nil || v.Block {
		t.Fatalf("Before second call (same session): v=%+v err=%v", v, err)
	}
	if len(asker.requests) != 1 {
		t.Fatalf("asker calls after grant = %d, want 1 (grant should have been used)", len(asker.requests))
	}

	// Different SessionID, RootID still empty: must not see the grant.
	_, v, err = h.Before(context.Background(), rc("s2", "", nil), tool, call)
	if err != nil || v.Block {
		t.Fatalf("Before third call (different session): v=%+v err=%v", v, err)
	}
	if len(asker.requests) != 2 {
		t.Fatalf("asker calls under s2 = %d, want 2 (grant must not cross sessions)", len(asker.requests))
	}
}

func TestHook_EmptyToolNameGoesToAskPath(t *testing.T) {
	// No cfg or agent rule names "", and Defaults() doesn't either, so
	// Evaluate falls back to an empty Rule{} -> empty Default -> Ask.
	asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyOnce}}
	h := NewHook(core.PermissionRules{}, asker)
	tool := fakeTool{name: ""}
	call := core.ToolCall{ID: "1", Name: ""}

	_, v, err := h.Before(context.Background(), rc("s1", "s1", nil), tool, call)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if v.Block {
		t.Errorf("Verdict.Block = true, want false")
	}
	if len(asker.requests) != 1 {
		t.Fatalf("asker calls = %d, want 1 (empty tool name must still go through Ask)", len(asker.requests))
	}
}

func TestHook_After_ReturnsResultUnchanged(t *testing.T) {
	h := NewHook(nil, &scriptedAsker{})
	res := core.ToolResult{CallID: "1", Output: "hi"}
	got := h.After(context.Background(), rc("s1", "s1", nil), fakeTool{name: "read"}, core.ToolCall{}, res)
	if got.CallID != res.CallID || got.Output != res.Output {
		t.Errorf("After = %+v, want unchanged %+v", got, res)
	}
}

func TestHook_AncestorRulesMostRestrictiveWins(t *testing.T) {
	cfg := core.PermissionRules{"bash": {Default: core.Allow}}
	tool := fakeTool{name: "bash"}
	call := core.ToolCall{ID: "1", Name: "bash"}

	t.Run("ancestor deny blocks", func(t *testing.T) {
		asker := &scriptedAsker{}
		h := NewHook(cfg, asker)
		r := rc("child", "root", nil)
		r.Ancestors = []core.PermissionRules{{"bash": {Default: core.Deny}}}
		_, v, err := h.Before(context.Background(), r, tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if !v.Block {
			t.Fatal("Verdict.Block = false, want true (ancestor denies bash)")
		}
	})

	t.Run("ancestor ask asks even though child allows", func(t *testing.T) {
		asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyDeny}}
		h := NewHook(cfg, asker)
		r := rc("child", "root", core.PermissionRules{"bash": {Default: core.Allow}})
		r.Ancestors = []core.PermissionRules{nil, {"bash": {Default: core.Ask}}}
		_, v, err := h.Before(context.Background(), r, tool, call)
		if err != nil {
			t.Fatalf("Before: %v", err)
		}
		if len(asker.requests) != 1 {
			t.Fatalf("asker called %d times, want 1", len(asker.requests))
		}
		if !v.Block {
			t.Error("Verdict.Block = false, want true (user denied)")
		}
	})

	t.Run("child deny wins over ancestor allow", func(t *testing.T) {
		asker := &scriptedAsker{}
		h := NewHook(cfg, asker)
		r := rc("child", "root", core.PermissionRules{"bash": {Default: core.Deny}})
		r.Ancestors = []core.PermissionRules{{"bash": {Default: core.Allow}}}
		_, v, _ := h.Before(context.Background(), r, tool, call)
		if !v.Block {
			t.Fatal("Verdict.Block = false, want true")
		}
	})

	t.Run("all allow passes", func(t *testing.T) {
		asker := &scriptedAsker{}
		h := NewHook(cfg, asker)
		r := rc("child", "root", nil)
		r.Ancestors = []core.PermissionRules{{"bash": {Default: core.Allow}}}
		_, v, _ := h.Before(context.Background(), r, tool, call)
		if v.Block || len(asker.requests) != 0 {
			t.Fatalf("Block=%v asks=%d, want pass without asking", v.Block, len(asker.requests))
		}
	})
}

func TestHook_BashPatternAllowDowngradedOnShellMetachars(t *testing.T) {
	cfg := core.PermissionRules{"bash": {Default: core.Ask, Patterns: map[string]core.Action{"git status*": core.Allow}}}
	tool := subjecterTool{fakeTool{name: "bash", subjecter: func(in json.RawMessage) string { return string(in) }}}

	cases := []struct {
		cmd     string
		wantAsk bool
	}{
		{"git status", false},
		{"git status --short", false},
		{"git status; rm -rf x", true},
		{"git status && rm -rf x", true},
		{"git status | sh", true},
		{"git status `rm x`", true},
		{"git status $(rm x)", true},
		{"git status > f", true},
		{"git status < f", true},
		{"git status\nrm -rf x", true},
	}
	for _, c := range cases {
		asker := &scriptedAsker{reply: core.PermissionReply{Kind: core.ReplyDeny}}
		h := NewHook(cfg, asker)
		call := core.ToolCall{ID: "1", Name: "bash", Input: json.RawMessage(c.cmd)}
		_, v, err := h.Before(context.Background(), rc("s", "s", nil), tool, call)
		if err != nil {
			t.Fatalf("%q: Before: %v", c.cmd, err)
		}
		asked := len(asker.requests) == 1
		if asked != c.wantAsk {
			t.Errorf("%q: asked = %v, want %v", c.cmd, asked, c.wantAsk)
		}
		if c.wantAsk && !v.Block {
			t.Errorf("%q: not blocked after deny reply", c.cmd)
		}
	}

	t.Run("tool-default allow is not downgraded", func(t *testing.T) {
		asker := &scriptedAsker{}
		h := NewHook(core.PermissionRules{"bash": {Default: core.Allow}}, asker)
		call := core.ToolCall{ID: "1", Name: "bash", Input: json.RawMessage("ls | wc -l")}
		_, v, _ := h.Before(context.Background(), rc("s", "s", nil), tool, call)
		if v.Block || len(asker.requests) != 0 {
			t.Fatalf("Block=%v asks=%d, want pass", v.Block, len(asker.requests))
		}
	})
}
