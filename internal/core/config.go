package core

import "fmt"

// Config is an immutable, merged snapshot of jig's configuration. See
// Task 8 for the TOML keys it is loaded from.
type Config struct {
	DefaultModel string
	SmallModel   string
	Providers    map[string]ProviderConfig
	Agents       map[string]AgentConfig
	ModelAliases map[string]string
	Permissions  PermissionRules
	SkillPaths   []string
	Instructions []string
	Keybinds     map[string]string
	Theme        string
	AgentBrowser Toggle // TOML: [integrations.agent_browser] enabled = ...
	MCP          MCPConfig
}

// ProviderConfig configures one LLM provider.
type ProviderConfig struct {
	Type        string
	APIKey      string
	BaseURL     string
	Models      []string
	Options     map[string]any
	ImageModels []string // TOML: image_models; models that accept image input (R10)
}

// Toggle is a tri-state setting: "" (unset), "auto", "true", or "false".
type Toggle string

const (
	ToggleAuto  Toggle = "auto"
	ToggleTrue  Toggle = "true"
	ToggleFalse Toggle = "false"
)

// UnmarshalTOML implements github.com/BurntSushi/toml's Unmarshaler
// interface, accepting either a bool or one of the strings "auto",
// "true", "false".
func (t *Toggle) UnmarshalTOML(v any) error {
	switch val := v.(type) {
	case bool:
		if val {
			*t = ToggleTrue
		} else {
			*t = ToggleFalse
		}
		return nil
	case string:
		switch val {
		case string(ToggleAuto), string(ToggleTrue), string(ToggleFalse):
			*t = Toggle(val)
			return nil
		default:
			return fmt.Errorf("core: invalid toggle %q", val)
		}
	default:
		return fmt.Errorf("core: toggle: want bool or string, got %T", v)
	}
}

// AgentConfig configures one agent, overlaying or replacing a built-in.
// CanSpawn and Hidden are pointers so config merging can distinguish
// "unset" from "explicitly false".
type AgentConfig struct {
	Description string
	Mode        string
	Model       string
	Prompt      string
	MaxSteps    int
	CanSpawn    *bool
	Hidden      *bool
	Tools       []string
	Permissions PermissionRules
	Source      string
}
