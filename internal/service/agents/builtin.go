// Package agents merges built-in agents with user-configured overlays from
// TOML config and markdown agent files, resolves each agent's model, and
// filters the tool set an agent may call.
package agents

import (
	"embed"
	"fmt"

	"github.com/gammons/jig/internal/core"
)

//go:embed prompts/*.md
var promptFS embed.FS

// mustPrompt reads the embedded prompt for name, panicking if it is
// missing: the prompts directory is part of the build, so a missing file
// is a programmer error, not a runtime condition.
func mustPrompt(name string) string {
	b, err := promptFS.ReadFile("prompts/" + name + ".md")
	if err != nil {
		panic(fmt.Sprintf("agents: missing embedded prompt %q: %v", name, err))
	}
	return string(b)
}

// builtins returns the built-in agents, keyed by name. It returns a fresh
// map and fresh Agent values on every call so callers may mutate the
// result freely.
func builtins() map[string]core.Agent {
	return map[string]core.Agent{
		"build": {
			Name:        "build",
			Description: "Autonomous coding agent with full tool access.",
			Prompt:      mustPrompt("build"),
			Mode:        core.ModePrimary,
			MaxSteps:    100,
			CanSpawn:    true,
		},
		"plan": {
			Name:        "plan",
			Description: "Planning agent; asks before writing, editing, or running shell commands.",
			Prompt:      mustPrompt("plan"),
			Mode:        core.ModePrimary,
			MaxSteps:    100,
			CanSpawn:    true,
			Permissions: core.PermissionRules{
				"write": {Default: core.Ask},
				"edit":  {Default: core.Ask},
				"bash":  {Default: core.Ask},
			},
		},
		"explore": {
			Name:        "explore",
			Description: "Read-only subagent for investigating the codebase.",
			Prompt:      mustPrompt("explore"),
			Mode:        core.ModeSubagent,
			Tools:       []string{"read", "glob", "grep", "skill"},
			MaxSteps:    40,
		},
		"general": {
			Name:        "general",
			Description: "General-purpose subagent with full tool access.",
			Prompt:      mustPrompt("general"),
			Mode:        core.ModeSubagent,
			CanSpawn:    false,
		},
		"title": {
			Name:        "title",
			Description: "Generates a short title for a conversation.",
			Prompt:      mustPrompt("title"),
			Mode:        core.ModeSubagent,
			Hidden:      true,
			Tools:       []string{},
		},
		"compaction": {
			Name:        "compaction",
			Description: "Summarizes a conversation for compaction.",
			Prompt:      mustPrompt("compaction"),
			Mode:        core.ModeSubagent,
			Hidden:      true,
			Tools:       []string{},
		},
	}
}
