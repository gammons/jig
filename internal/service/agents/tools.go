package agents

import (
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// ToolsFor filters all down to the tools a may call: only the names in
// a.Tools when it is non-nil (all tools otherwise), never "task" unless
// a.CanSpawn, and never a tool a's own permissions plainly deny (Default
// deny with no patterns; global config denies are enforced later, at call
// time, by the permission hook).
func ToolsFor(a core.Agent, all []ext.Tool) []ext.Tool {
	var allowed map[string]bool
	if a.Tools != nil {
		allowed = make(map[string]bool, len(a.Tools))
		for _, name := range a.Tools {
			allowed[name] = true
		}
	}

	out := make([]ext.Tool, 0, len(all))
	for _, t := range all {
		name := t.Name()
		if allowed != nil && !allowed[name] {
			continue
		}
		if name == "task" && !a.CanSpawn {
			continue
		}
		if rule, ok := a.Permissions[name]; ok && rule.Default == core.Deny && len(rule.Patterns) == 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}
