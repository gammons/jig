package core

import "fmt"

// AgentMode controls which contexts an Agent may run in.
type AgentMode string

const (
	ModePrimary  AgentMode = "primary"
	ModeSubagent AgentMode = "subagent"
	ModeAll      AgentMode = "all"
)

// Action is the permission decision for a tool invocation.
type Action string

const (
	Allow Action = "allow"
	Ask   Action = "ask"
	Deny  Action = "deny"
)

// Rule is one tool's permission rule: either a blanket Default action, or a
// set of glob Patterns each mapped to an Action.
type Rule struct {
	Default  Action
	Patterns map[string]Action
}

// UnmarshalTOML implements github.com/BurntSushi/toml's Unmarshaler
// interface, accepting either a bare action string ("deny") or a table of
// glob pattern -> action ({"git status*" = "allow", "*" = "ask"}).
func (r *Rule) UnmarshalTOML(v any) error {
	switch val := v.(type) {
	case string:
		action := Action(val)
		if err := validateAction(action); err != nil {
			return err
		}
		r.Default = action
		return nil
	case map[string]any:
		patterns := make(map[string]Action, len(val))
		for pattern, raw := range val {
			s, ok := raw.(string)
			if !ok {
				return fmt.Errorf("core: permission rule pattern %q: want string action, got %T", pattern, raw)
			}
			action := Action(s)
			if err := validateAction(action); err != nil {
				return err
			}
			patterns[pattern] = action
		}
		r.Patterns = patterns
		return nil
	default:
		return fmt.Errorf("core: permission rule: want string or table, got %T", v)
	}
}

func validateAction(a Action) error {
	switch a {
	case Allow, Ask, Deny:
		return nil
	default:
		return fmt.Errorf("core: invalid permission action %q", a)
	}
}

// PermissionRules maps a tool name to its Rule.
type PermissionRules map[string]Rule

// Agent describes a configured agent persona.
type Agent struct {
	Name        string
	Description string
	Prompt      string
	Mode        AgentMode
	Model       ModelRef
	ModelAlias  string
	MaxSteps    int
	CanSpawn    bool
	Hidden      bool
	Tools       []string
	Permissions PermissionRules
}
