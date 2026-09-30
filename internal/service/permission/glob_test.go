package permission

import (
	"context"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

func TestRuleFor_ExactBeatsGlob(t *testing.T) {
	rules := core.PermissionRules{
		"mcp__gh__*":   {Default: core.Deny},
		"mcp__gh__get": {Default: core.Allow},
	}
	got := RuleFor(rules, "mcp__gh__get")
	if got.Default != core.Allow {
		t.Errorf("RuleFor = %+v, want Default %q (exact key beats glob)", got, core.Allow)
	}
}

func TestRuleFor_MostSpecificGlob(t *testing.T) {
	rules := core.PermissionRules{
		"mcp__*":         {Default: core.Ask},
		"mcp__gh__get_*": {Default: core.Allow},
	}

	got := RuleFor(rules, "mcp__gh__get_issue")
	if got.Default != core.Allow {
		t.Errorf("RuleFor(mcp__gh__get_issue) = %+v, want Default %q (most specific glob wins)", got, core.Allow)
	}

	got = RuleFor(rules, "mcp__gh__push")
	if got.Default != core.Ask {
		t.Errorf("RuleFor(mcp__gh__push) = %+v, want Default %q (only the less specific glob matches)", got, core.Ask)
	}
}

func TestRuleFor_TieFavorsRestrictive(t *testing.T) {
	// Both patterns have 10 literal characters, so they tie on
	// specificity; deny must win the tie regardless of map iteration
	// order.
	rules := core.PermissionRules{
		"mcp__*__get": {Default: core.Allow},
		"mcp__gh__g*": {Default: core.Deny},
	}
	got := RuleFor(rules, "mcp__gh__get")
	if got.Default != core.Deny {
		t.Errorf("RuleFor = %+v, want Default %q (tie must favor deny)", got, core.Deny)
	}
}

func TestRuleFor_NoMatchReturnsZeroRule(t *testing.T) {
	rules := core.PermissionRules{"mcp__gh__*": {Default: core.Allow}}
	got := RuleFor(rules, "mcp__aws__get")
	if got.Default != "" || len(got.Patterns) != 0 {
		t.Errorf("RuleFor(no match) = %+v, want the zero Rule", got)
	}
}

func TestEffective_GlobAcrossLayers(t *testing.T) {
	cfg := core.PermissionRules{
		"mcp__gh__*": {Default: core.Allow},
	}
	agent := core.PermissionRules{
		"mcp__gh__push": {Default: core.Deny},
	}

	eff := Effective(agent, cfg)

	if got := RuleFor(eff, "mcp__gh__push"); got.Default != core.Deny {
		t.Errorf("RuleFor(eff, mcp__gh__push) = %+v, want Default %q (agent exact deny wins)", got, core.Deny)
	}
	if got := RuleFor(eff, "mcp__gh__get"); got.Default != core.Allow {
		t.Errorf("RuleFor(eff, mcp__gh__get) = %+v, want Default %q (config glob allow applies)", got, core.Allow)
	}
}

func TestEffectiveFor_AgentGlobDenyBeatsCfgExactAllow(t *testing.T) {
	agent := core.PermissionRules{"mcp__*": {Default: core.Deny}}
	cfg := core.PermissionRules{"mcp__gh__push": {Default: core.Allow}}

	got := EffectiveFor(agent, cfg, "mcp__gh__push")
	if got.Default != core.Deny {
		t.Errorf("EffectiveFor = %+v, want Default %q (agent glob deny must beat cfg's more specific exact allow)", got, core.Deny)
	}
}

func TestEffectiveFor_AgentGlobBeatsMoreSpecificCfgGlob(t *testing.T) {
	agent := core.PermissionRules{"mcp__*": {Default: core.Deny}}
	cfg := core.PermissionRules{"mcp__gh__push_*": {Default: core.Allow}}

	got := EffectiveFor(agent, cfg, "mcp__gh__push_x")
	if got.Default != core.Deny {
		t.Errorf("EffectiveFor = %+v, want Default %q (agent's less-specific glob still outranks cfg's more specific one, by layer precedence)", got, core.Deny)
	}
}

func TestEffectiveFor_CfgGlobAppliesWhenAgentSilent(t *testing.T) {
	agent := core.PermissionRules{}
	cfg := core.PermissionRules{"mcp__gh__*": {Default: core.Allow}}

	got := EffectiveFor(agent, cfg, "mcp__gh__get")
	if got.Default != core.Allow {
		t.Errorf("EffectiveFor = %+v, want Default %q (cfg glob applies when agent has no matching rule)", got, core.Allow)
	}
}

func TestHook_AgentGlobDenyBlocksOverCfgExactAllow(t *testing.T) {
	cfg := core.PermissionRules{"mcp__gh__push": {Default: core.Allow}}
	asker := &scriptedAsker{}
	h := NewHook(cfg, asker)
	tool := fakeTool{name: "mcp__gh__push"}
	call := core.ToolCall{ID: "1", Name: "mcp__gh__push"}

	r := rcForGlob("s1", "s1")
	r.Agent.Permissions = core.PermissionRules{"mcp__*": {Default: core.Deny}}

	_, v, err := h.Before(context.Background(), r, tool, call)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if !v.Block {
		t.Fatal("Verdict.Block = false, want true (agent glob deny must beat cfg exact allow)")
	}
	if len(asker.requests) != 0 {
		t.Errorf("asker was called %d times, want 0", len(asker.requests))
	}
}

func TestHook_GlobAllow(t *testing.T) {
	cfg := core.PermissionRules{
		"mcp__gh__*": {Default: core.Allow},
	}
	asker := &scriptedAsker{}
	h := NewHook(cfg, asker)
	tool := fakeTool{name: "mcp__gh__get"}
	call := core.ToolCall{ID: "1", Name: "mcp__gh__get"}

	_, v, err := h.Before(context.Background(), rcForGlob("s1", "s1"), tool, call)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if v.Block {
		t.Errorf("Verdict.Block = true, want false (glob allow)")
	}
	if len(asker.requests) != 0 {
		t.Errorf("asker was called %d times, want 0", len(asker.requests))
	}
}

func rcForGlob(session, root core.SessionID) ext.RunContext {
	return ext.RunContext{
		SessionID: session,
		RootID:    root,
		Agent:     core.Agent{Name: "build"},
	}
}
