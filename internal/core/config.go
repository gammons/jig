package core

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
}

// ProviderConfig configures one LLM provider.
type ProviderConfig struct {
	Type    string
	APIKey  string
	BaseURL string
	Models  []string
	Options map[string]any
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
