package trust

import (
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// Effect is one thing a project layer changes: a config Key, its Value
// (empty for keys named without a value, such as an agent's prompt), and
// the declaring file in Source ("" for config.toml keys). Values never
// carry secrets: API keys and provider options are "(set)" (or their raw
// "{env:…}"/"{file:…}" tokens), and a base_url loses userinfo and query.
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
	out = mcpEffects(out, l)
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

// providerEffects describes a provider's fields without printing secrets.
// Effects are computed from unsubstituted project text, so a
// "{env:…}"/"{file:…}" token is a name, not a secret, and is shown raw:
// api_key and options print their tokens, or "(set)" for a literal. A
// base_url prints as scheme://host[:port]/path (see safeURL).
func providerEffects(out []Effect, prefix string, pc core.ProviderConfig) []Effect {
	out = scalar(out, prefix+"type", pc.Type)
	if pc.APIKey != "" {
		out = append(out, Effect{Key: prefix + "api_key", Value: secretValue(tokensIn(nil, pc.APIKey))})
	}
	if pc.BaseURL != "" {
		out = append(out, Effect{Key: prefix + "base_url", Value: safeURL(pc.BaseURL)})
	}
	if pc.Models != nil {
		out = append(out, Effect{Key: prefix + "models", Value: list(pc.Models)})
	}
	if pc.Options != nil {
		out = append(out, Effect{Key: prefix + "options", Value: secretValue(optionTokens(nil, pc.Options))})
	}
	if pc.ImageModels != nil {
		out = append(out, Effect{Key: prefix + "image_models", Value: list(pc.ImageModels)})
	}
	return out
}

// tokenRE matches "{env:VAR}" and "{file:path}" substitution tokens (the
// same syntax internal/data/config substitutes), plus "${VAR}" and
// "${VAR:-default}" tokens (the .mcp.json syntax, spec §5.1/§5.2): a
// server value holding one is a name, not a secret, and prints raw.
const tokenRE = `\{(env|file):[^}]*\}|\$\{[^}]*\}`

// tokensIn appends every substitution token in s to dst.
func tokensIn(dst []string, s string) []string {
	return append(dst, regexp.MustCompile(tokenRE).FindAllString(s, -1)...)
}

// optionTokens appends the tokens in every string reachable from v (maps
// and slices are walked) to dst.
func optionTokens(dst []string, v any) []string {
	switch v := v.(type) {
	case string:
		return tokensIn(dst, v)
	case map[string]any:
		for _, e := range v {
			dst = optionTokens(dst, e)
		}
	case []any:
		for _, e := range v {
			dst = optionTokens(dst, e)
		}
	}
	return dst
}

// secretValue renders a secret-bearing value by its tokens (sorted,
// de-duplicated), or "(set)" when it holds none: literal text is never
// printed, since it may be the secret itself.
func secretValue(tokens []string) string {
	if len(tokens) == 0 {
		return "(set)"
	}
	sort.Strings(tokens)
	return strings.Join(slices.Compact(tokens), ", ")
}

// safeURL renders s as scheme://host[:port]/path, dropping userinfo and
// the fragment, and replacing any query with "?…"; tokens anywhere in the
// kept parts are shown raw. Credentials placed in the path are shown: the
// path is part of where requests go. A value that does not parse as
// scheme://host (e.g. a bare "{env:BASE_URL}") prints like a secret: its
// tokens, or "(set)".
func safeURL(s string) string {
	re := regexp.MustCompile(tokenRE)
	tokens := re.FindAllString(s, -1)
	ph := func(i int) string { return "jigtoken" + strconv.Itoa(i) + "x" }
	i := 0
	masked := re.ReplaceAllStringFunc(s, func(string) string { i++; return ph(i - 1) })

	u, err := url.Parse(masked)
	if err != nil || u.Opaque != "" || u.Scheme == "" || u.Host == "" {
		return secretValue(tokens)
	}
	out := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" || u.ForceQuery {
		out += "?…"
	}
	for i, tok := range tokens {
		out = strings.ReplaceAll(out, ph(i), tok)
	}
	return out
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
