// Package config discovers, substitutes, and merges jig's TOML config
// files into a core.Config (spec §6.2). See Load for the entry point.
package config

import "github.com/gammons/jig/internal/core"

// tomlFile is the raw shape of one config.toml file, decoded with
// github.com/BurntSushi/toml. core types carry no toml tags, so each file
// is decoded into these private DTOs first and converted/merged into typed
// core values afterward (merge.go).
type tomlFile struct {
	DefaultModel  string                 `toml:"default_model"`
	SmallModel    string                 `toml:"small_model"`
	DefaultEffort string                 `toml:"default_effort"`
	Theme         string                 `toml:"theme"`
	Providers     map[string]providerDTO `toml:"providers"`
	Agents        map[string]agentDTO    `toml:"agents"`
	ModelAliases  map[string]string      `toml:"model_aliases"`
	Permissions   map[string]core.Rule   `toml:"permissions"`
	Skills        skillsDTO              `toml:"skills"`
	Instructions  []string               `toml:"instructions"`
	Keybinds      map[string]string      `toml:"keybinds"`
	Integrations  integrationsDTO        `toml:"integrations"`
}

// providerDTO is the raw shape of one [providers.<id>] table.
type providerDTO struct {
	Type        string         `toml:"type"`
	APIKey      string         `toml:"api_key"`
	BaseURL     string         `toml:"base_url"`
	Models      []string       `toml:"models"`
	Options     map[string]any `toml:"options"`
	ImageModels []string       `toml:"image_models"`
	Efforts     []string       `toml:"efforts"`
}

// integrationsDTO is the raw shape of the [integrations] table.
type integrationsDTO struct {
	AgentBrowser agentBrowserDTO `toml:"agent_browser"`
}

// agentBrowserDTO is the raw shape of [integrations.agent_browser].
type agentBrowserDTO struct {
	Enabled core.Toggle `toml:"enabled"`
}

// agentDTO is the raw shape of one [agents.<name>] table. CanSpawn and
// Hidden are pointers so the merge step can tell "unset" apart from
// "explicitly false", matching core.AgentConfig.
type agentDTO struct {
	Description string               `toml:"description"`
	Mode        string               `toml:"mode"`
	Model       string               `toml:"model"`
	Effort      string               `toml:"effort"`
	Prompt      string               `toml:"prompt"`
	MaxSteps    int                  `toml:"max_steps"`
	CanSpawn    *bool                `toml:"can_spawn"`
	Hidden      *bool                `toml:"hidden"`
	Tools       []string             `toml:"tools"`
	Permissions map[string]core.Rule `toml:"permissions"`
}

// skillsDTO is the raw shape of the [skills] table.
type skillsDTO struct {
	Paths []string `toml:"paths"`
}
