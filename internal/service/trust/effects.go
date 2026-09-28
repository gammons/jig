package trust

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// Effect is one thing a project layer changes: a config Key, its Value
// (empty for keys named without a value, such as an agent's prompt), and
// the declaring file in Source ("" for config.toml keys). Values never
// carry secrets: API keys and provider options are "(set)".
type Effect struct{ Key, Value, Source string }

// String renders e for display, e.g. "permissions.bash → allow",
// "instructions += docs/rules.md", or
// "agents.reviewer (from /p/.claude/agents/reviewer.md) prompt". The
// result is untrusted text; sanitize it at the render boundary.
func (e Effect) String() string {
	key := e.Key
	if e.Source != "" {
		key = withSource(key, e.Source)
	}
	switch {
	case e.Key == "instructions" || e.Key == "skills.paths":
		return key + " += " + e.Value
	case e.Value == "":
		return key
	default:
		return key + " → " + e.Value
	}
}

// withSource inserts "(from src)" after key's agent-name segment for an
// "agents.<name> ..." key, or appends it otherwise.
func withSource(key, src string) string {
	from := " (from " + src + ")"
	rest, ok := strings.CutPrefix(key, "agents.")
	if !ok {
		return key + from
	}
	n := strings.IndexByte(rest, ' ')
	if strings.HasPrefix(rest, `"`) {
		if q, err := strconv.QuotedPrefix(rest); err == nil {
			n = len(q)
		}
	}
	if n < 0 || n > len(rest) {
		return key + from
	}
	split := len("agents.") + n
	return key[:split] + from + key[split:]
}

// Effects lists everything the project layer (Project and ProjectMD)
// changes, sorted by Key, then Value, then Source; nil when nothing.
func Effects(l Layers) []Effect {
	var out []Effect
	out = configEffects(out, l.Project)
	out = agentEffects(out, l.Project.Agents, false)
	out = agentEffects(out, l.ProjectMD, true)
	out = tokenEffects(out, l.ProjectTokens)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		return a.Source < b.Source
	})
	return out
}

func configEffects(out []Effect, p core.Config) []Effect {
	out = scalar(out, "default_model", p.DefaultModel)
	out = scalar(out, "small_model", p.SmallModel)
	out = scalar(out, "theme", p.Theme)
	out = scalar(out, "integrations.agent_browser.enabled", string(p.AgentBrowser))
	for name, pc := range p.Providers {
		out = providerEffects(out, "providers."+seg(name)+".", pc)
	}
	for _, v := range p.Instructions {
		out = append(out, Effect{Key: "instructions", Value: v})
	}
	for _, v := range p.SkillPaths {
		out = append(out, Effect{Key: "skills.paths", Value: v})
	}
	for k, v := range p.ModelAliases {
		out = append(out, Effect{Key: "model_aliases." + seg(k), Value: v})
	}
	for k, v := range p.Keybinds {
		out = append(out, Effect{Key: "keybinds." + seg(k), Value: v})
	}
	return permEffects(out, "permissions.", p.Permissions, "")
}

// providerEffects describes a provider's fields without printing secrets:
// api_key and options are "(set)", and a base_url carrying userinfo or a
// query (or not parsing at all) is "(set)" too.
func providerEffects(out []Effect, prefix string, pc core.ProviderConfig) []Effect {
	out = scalar(out, prefix+"type", pc.Type)
	if pc.APIKey != "" {
		out = append(out, Effect{Key: prefix + "api_key", Value: "(set)"})
	}
	if pc.BaseURL != "" {
		out = append(out, Effect{Key: prefix + "base_url", Value: safeURL(pc.BaseURL)})
	}
	if pc.Models != nil {
		out = append(out, Effect{Key: prefix + "models", Value: list(pc.Models)})
	}
	if pc.Options != nil {
		out = append(out, Effect{Key: prefix + "options", Value: "(set)"})
	}
	if pc.ImageModels != nil {
		out = append(out, Effect{Key: prefix + "image_models", Value: list(pc.ImageModels)})
	}
	return out
}

func safeURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "(set)"
	}
	return s
}

// agentEffects describes every agent in m. Only markdown agents
// (withSource) carry their file as Source; TOML agents are config.toml
// keys.
func agentEffects(out []Effect, m map[string]core.AgentConfig, withSource bool) []Effect {
	for name, a := range m {
		src := ""
		if withSource {
			src = a.Source
		}
		prefix := "agents." + seg(name) + " "
		add := func(field, value string) {
			out = append(out, Effect{Key: prefix + field, Value: value, Source: src})
		}
		if a.Description != "" {
			add("description", "")
		}
		if a.Prompt != "" {
			add("prompt", "")
		}
		if a.Mode != "" {
			add("mode", a.Mode)
		}
		if a.Model != "" {
			add("model", a.Model)
		}
		if a.MaxSteps != 0 {
			add("max_steps", strconv.Itoa(a.MaxSteps))
		}
		if a.CanSpawn != nil {
			add("can_spawn", strconv.FormatBool(*a.CanSpawn))
		}
		if a.Hidden != nil {
			add("hidden", strconv.FormatBool(*a.Hidden))
		}
		if a.Tools != nil {
			add("tools", list(a.Tools))
		}
		out = permEffects(out, prefix+"permissions.", a.Permissions, src)
	}
	return out
}

// permEffects adds one effect per rule Default and per pattern; patterns
// are quoted.
func permEffects(out []Effect, prefix string, rules core.PermissionRules, src string) []Effect {
	for tool, r := range rules {
		key := prefix + seg(tool)
		if r.Default != "" {
			out = append(out, Effect{Key: key, Value: string(r.Default), Source: src})
		}
		for p, a := range r.Patterns {
			out = append(out, Effect{Key: key + " " + strconv.Quote(p), Value: string(a), Source: src})
		}
	}
	return out
}

// tokenEffects describes each token-valued permission action by its raw
// token, keyed like permEffects.
func tokenEffects(out []Effect, tokens []TokenAction) []Effect {
	for _, t := range tokens {
		key := "permissions." + seg(t.Tool)
		if t.Agent != "" {
			key = "agents." + seg(t.Agent) + " " + key
		}
		if t.Pattern != "" {
			key += " " + strconv.Quote(t.Pattern)
		}
		out = append(out, Effect{Key: key, Value: t.Token})
	}
	return out
}

func scalar(out []Effect, key, value string) []Effect {
	if value == "" {
		return out
	}
	return append(out, Effect{Key: key, Value: value})
}

func list(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

// seg returns name as a key segment: bare when it is a plain identifier
// (letters, digits, '_', '-', '.'), quoted otherwise, so an odd name can
// neither smuggle control characters nor blur where the segment ends.
func seg(name string) string {
	if name == "" {
		return strconv.Quote(name)
	}
	for _, r := range name {
		plain := r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !plain {
			return strconv.Quote(name)
		}
	}
	return name
}
