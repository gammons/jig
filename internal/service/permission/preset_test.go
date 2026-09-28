package permission

import (
	"testing"

	"github.com/gammons/jig/internal/core"
)

// TestAgentBrowserPreset_NoDefaultOrDeny pins that the preset only ever
// relaxes a default ask: it adds allow patterns and nothing else.
func TestAgentBrowserPreset_NoDefaultOrDeny(t *testing.T) {
	for tool, rule := range AgentBrowserPreset() {
		if rule.Default != "" {
			t.Errorf("tool %s: Default = %q, want empty", tool, rule.Default)
		}
		for pattern, action := range rule.Patterns {
			if action != core.Allow {
				t.Errorf("tool %s pattern %q: action = %q, want allow", tool, pattern, action)
			}
		}
	}
}

func TestPreset_Decisions(t *testing.T) {
	h := NewHook(nil, nil, WithPreset(AgentBrowserPreset()))

	cases := []struct {
		subject string
		want    core.Action
	}{
		{"agent-browser snapshot -i", core.Allow},
		{"agent-browser screenshot", core.Allow},
		{"agent-browser click @e1", core.Ask},
		{"agent-browser snapshot; rm -rf x", core.Ask}, // metachar downgrade
		{"agent-browser screenshot ~/.bashrc", core.Ask},
		{"agent-browser screenshot /tmp/x.png", core.Ask},
		{"agent-browser snapshot $HOME", core.Ask},
		{"agent-browser snapshot ${HOME}", core.Ask},
		{"ls", core.Ask},
	}
	for _, c := range cases {
		if got := h.decideOne(nil, "bash", c.subject); got != c.want {
			t.Errorf("decideOne(%q) = %q, want %q", c.subject, got, c.want)
		}
	}
}

func TestPreset_NotAppliedWithoutOption(t *testing.T) {
	h := NewHook(nil, nil)
	if got := h.decideOne(nil, "bash", "agent-browser snapshot -i"); got != core.Ask {
		t.Errorf("decideOne without preset = %q, want ask", got)
	}
}

// TestPreset_NeverOverridesUserRules pins that the preset only relaxes an
// ask that came from the tool's Default: a deny Default (global or agent)
// and any user pattern (allow, ask, or deny) all beat it.
func TestPreset_NeverOverridesUserRules(t *testing.T) {
	const snap = "agent-browser snapshot -i"
	cases := []struct {
		name  string
		cfg   core.PermissionRules
		agent core.PermissionRules
		want  core.Action
	}{
		{"global bash deny", core.PermissionRules{"bash": {Default: core.Deny}}, nil, core.Deny},
		{"agent bash deny", nil, core.PermissionRules{"bash": {Default: core.Deny}}, core.Deny},
		{"user deny pattern", core.PermissionRules{"bash": {Patterns: map[string]core.Action{"agent-browser *": core.Deny}}}, nil, core.Deny},
		{"user ask pattern", core.PermissionRules{"bash": {Patterns: map[string]core.Action{"agent-browser snapshot*": core.Ask}}}, nil, core.Ask},
		{"agent ask pattern", nil, core.PermissionRules{"bash": {Patterns: map[string]core.Action{"agent-*": core.Ask}}}, core.Ask},
		{"user allow pattern", core.PermissionRules{"bash": {Patterns: map[string]core.Action{"agent-browser *": core.Allow}}}, nil, core.Allow},
		{"global bash allow", core.PermissionRules{"bash": {Default: core.Allow}}, nil, core.Allow},
	}
	for _, c := range cases {
		h := NewHook(c.cfg, nil, WithPreset(AgentBrowserPreset()))
		if got := h.decideOne(c.agent, "bash", snap); got != c.want {
			t.Errorf("%s: decideOne(%q) = %q, want %q", c.name, snap, got, c.want)
		}
	}
}

// TestPreset_AncestorDenyWins pins that the preset does not loosen a
// subagent past an ancestor that denies bash.
func TestPreset_AncestorDenyWins(t *testing.T) {
	h := NewHook(nil, nil, WithPreset(AgentBrowserPreset()))
	r := rc("s", "s", nil)
	r.Ancestors = []core.PermissionRules{{"bash": {Default: core.Deny}}}
	if got := h.decide(r, "bash", "agent-browser snapshot -i"); got != core.Deny {
		t.Errorf("decide = %q, want deny", got)
	}
}

func TestHasShellMeta_Dollar(t *testing.T) {
	for _, s := range []string{"echo $HOME", "echo ${HOME}", "echo $(id)"} {
		if !hasShellMeta(s) {
			t.Errorf("hasShellMeta(%q) = false, want true", s)
		}
	}
}
