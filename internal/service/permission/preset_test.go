package permission

import (
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/config"
)

// TestAgentBrowserPreset_NoDefaultOrDeny pins the soundness argument Task
// 14's Restrict relies on: the preset adds only allow/ask patterns, never
// a Default (which could rank above the baseline's) or a deny.
func TestAgentBrowserPreset_NoDefaultOrDeny(t *testing.T) {
	for tool, rule := range AgentBrowserPreset() {
		if rule.Default != "" {
			t.Errorf("tool %s: Default = %q, want empty", tool, rule.Default)
		}
		for pattern, action := range rule.Patterns {
			if action == core.Deny {
				t.Errorf("tool %s pattern %q: action = deny, want allow or ask", tool, pattern)
			}
		}
	}
}

func TestPreset_Decisions(t *testing.T) {
	merged := config.Merge(core.Config{Permissions: AgentBrowserPreset()}, core.Config{})
	h := NewHook(merged.Permissions, nil)

	cases := []struct {
		subject string
		want    core.Action
	}{
		{"agent-browser snapshot -i", core.Allow},
		{"agent-browser click @e1", core.Ask},
		{"agent-browser snapshot; rm -rf x", core.Ask}, // metachar downgrade
	}
	for _, c := range cases {
		if got := h.decideOne(nil, "bash", c.subject); got != c.want {
			t.Errorf("decideOne(%q) = %q, want %q", c.subject, got, c.want)
		}
	}
}
