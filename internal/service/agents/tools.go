package agents

import (
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/service/permission"
)

// ToolsFor filters all down to the tools a may call: only the names in
// a.Tools when it is non-nil (all tools otherwise; an entry containing
// '*' matches any tool name via permission.Match), never "task" unless
// a.CanSpawn, and never a tool a's own permissions plainly deny (Default
// deny with no patterns, resolved per tool via permission.RuleFor so a
// glob key denies every tool it matches; global config denies are
// enforced later, at call time, by the permission hook).
func ToolsFor(a core.Agent, all []ext.Tool) []ext.Tool {
	out := make([]ext.Tool, 0, len(all))
	for _, t := range all {
		name := t.Name()
		if a.Tools != nil && !toolAllowed(a.Tools, name) {
			continue
		}
		if name == "task" && !a.CanSpawn {
			continue
		}
		if rule := permission.RuleFor(a.Permissions, name); rule.Default == core.Deny && len(rule.Patterns) == 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}

// toolAllowed reports whether name is in allowed, either as an exact
// entry or matched by a glob entry (one containing '*') via
// permission.Match.
func toolAllowed(allowed []string, name string) bool {
	for _, entry := range allowed {
		if entry == name {
			return true
		}
		if strings.Contains(entry, "*") && permission.Match(entry, name) {
			return true
		}
	}
	return false
}
