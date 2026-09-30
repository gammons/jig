// Package permission evaluates tool-call permission rules, blocks or asks
// the user through an ext.ToolHook, and answers permission requests raised
// over the event bus.
package permission

import (
	"strings"

	"github.com/gammons/jig/internal/core"
)

// Defaults returns jig's built-in permission rules: read-only and
// low-risk tools are allowed, and tools with side effects ask.
func Defaults() core.PermissionRules {
	return core.PermissionRules{
		"read":  {Default: core.Allow},
		"glob":  {Default: core.Allow},
		"grep":  {Default: core.Allow},
		"todo":  {Default: core.Allow},
		"task":  {Default: core.Allow},
		"skill": {Default: core.Allow},
		"write": {Default: core.Ask},
		"edit":  {Default: core.Ask},
		"bash":  {Default: core.Ask},
	}
}

// AgentBrowserPreset returns the read-only/low-risk agent-browser bash
// subcommands the integration allows when enabled. It is not a config
// layer: the Hook consults it (see WithPreset) only for a call whose
// action is an ask that came from the tool's Default, so it never
// overrides a deny or any user pattern. Every other "agent-browser ..."
// invocation keeps that default ask. "screenshot" takes no path argument,
// since a path would let the call overwrite an arbitrary file.
func AgentBrowserPreset() core.PermissionRules {
	return core.PermissionRules{
		"bash": {
			Patterns: map[string]core.Action{
				"agent-browser snapshot*":  core.Allow,
				"agent-browser screenshot": core.Allow,
				"agent-browser console*":   core.Allow,
				"agent-browser errors*":    core.Allow,
				"agent-browser get *":      core.Allow,
				"agent-browser is *":       core.Allow,
				"agent-browser tab":        core.Allow,
				"agent-browser a11y*":      core.Allow,
				"agent-browser vitals*":    core.Allow,
				"agent-browser read*":      core.Allow,
			},
		},
	}
}

// Effective merges an agent's permission rules over a config's over
// Defaults(), per key, covering the union of every key named by any of
// the three (including glob keys). Each output entry is
// EffectiveFor(agent, cfg, key): the key (glob or exact) is resolved
// within each layer independently via RuleFor, as if it were itself the
// tool name, before the three layers are overlaid (agent highest, then
// cfg, then Defaults()). This means a higher-precedence layer's glob can
// out-rank a lower-precedence layer's more specific glob or even its
// exact key for the same tool — resolve a tool's final rule with
// RuleFor(Effective(agent, cfg), tool), which then only needs to pick
// among Effective's exact and glob keys (each already an across-layer
// result) to find the single best match for that tool.
func Effective(agent, cfg core.PermissionRules) core.PermissionRules {
	defaults := Defaults()

	keys := make(map[string]struct{}, len(defaults)+len(cfg)+len(agent))
	for _, rules := range []core.PermissionRules{defaults, cfg, agent} {
		for key := range rules {
			keys[key] = struct{}{}
		}
	}

	out := make(core.PermissionRules, len(keys))
	for key := range keys {
		out[key] = EffectiveFor(agent, cfg, key)
	}
	return out
}

// EffectiveFor resolves tool's rule across all three layers: agent
// highest, then cfg, then Defaults(). Each layer is first resolved to a
// single rule via RuleFor (so a layer's glob key can supply tool's rule),
// and the three results are then overlaid in that precedence order, so a
// higher-precedence layer's glob (even one less specific than a
// lower-precedence layer's glob or exact key) always wins.
func EffectiveFor(agent, cfg core.PermissionRules, tool string) core.Rule {
	return overlayRule(RuleFor(Defaults(), tool), RuleFor(cfg, tool), RuleFor(agent, tool))
}

// RuleFor resolves tool's rule within one layer of rules: the exact key
// wins if present. Otherwise, among the keys that contain '*' and whose
// Match(key, tool) is true, the highest specificity wins; on a tie among
// equally specific glob keys, the more restrictive Default wins
// (actionRank), and the tied keys' Patterns are merged, a shared pattern
// key keeping the more restrictive action. No match returns the zero
// Rule.
func RuleFor(rules core.PermissionRules, tool string) core.Rule {
	if r, ok := rules[tool]; ok {
		return r
	}

	bestSpecificity := -1
	var best core.Rule
	matched := false

	for key, r := range rules {
		if !strings.Contains(key, "*") || !Match(key, tool) {
			continue
		}
		spec := specificity(key)
		switch {
		case !matched || spec > bestSpecificity:
			bestSpecificity, best, matched = spec, r, true
		case spec == bestSpecificity:
			best = mergeGlobTie(best, r)
		}
	}
	return best
}

// mergeGlobTie combines two equally specific glob rules that tie for a
// tool: the more restrictive Default wins (actionRank), and Patterns
// merge, a shared pattern key keeping the more restrictive action.
func mergeGlobTie(a, b core.Rule) core.Rule {
	out := core.Rule{Default: a.Default}
	if actionRank(b.Default) > actionRank(out.Default) {
		out.Default = b.Default
	}
	for pattern, action := range a.Patterns {
		out.Patterns = putIfMoreRestrictive(out.Patterns, pattern, action)
	}
	for pattern, action := range b.Patterns {
		out.Patterns = putIfMoreRestrictive(out.Patterns, pattern, action)
	}
	return out
}

func putIfMoreRestrictive(m map[string]core.Action, pattern string, action core.Action) map[string]core.Action {
	if m == nil {
		m = make(map[string]core.Action)
	}
	if existing, ok := m[pattern]; !ok || actionRank(action) > actionRank(existing) {
		m[pattern] = action
	}
	return m
}

// overlayRule applies layers in order, lowest precedence first: a later
// layer's non-empty Default replaces the running result, and a later
// layer's Patterns are merged in, overwriting shared keys.
func overlayRule(layers ...core.Rule) core.Rule {
	var out core.Rule
	for _, layer := range layers {
		if layer.Default != "" {
			out.Default = layer.Default
		}
		if layer.Patterns == nil {
			continue
		}
		if out.Patterns == nil {
			out.Patterns = make(map[string]core.Action, len(layer.Patterns))
		}
		for pattern, action := range layer.Patterns {
			out.Patterns[pattern] = action
		}
	}
	return out
}

// Evaluate resolves r's action for subject: the pattern with the most
// literal (non-'*') characters that matches subject wins; a tie among
// equally specific matches favors Deny over Ask over Allow. When no
// pattern matches, r.Default applies, and an empty Default means Ask.
func Evaluate(r core.Rule, subject string) core.Action {
	a, _ := evaluate(r, subject)
	return a
}

// evaluate is Evaluate that also reports whether the action came from a
// matching pattern (true) rather than r.Default (false).
func evaluate(r core.Rule, subject string) (core.Action, bool) {
	bestSpecificity := -1
	var bestAction core.Action
	matched := false

	for pattern, action := range r.Patterns {
		if !Match(pattern, subject) {
			continue
		}
		spec := specificity(pattern)
		switch {
		case !matched, spec > bestSpecificity:
			bestSpecificity, bestAction, matched = spec, action, true
		case spec == bestSpecificity && actionRank(action) > actionRank(bestAction):
			bestAction = action
		}
	}

	if !matched {
		if r.Default == "" {
			return core.Ask, false
		}
		return r.Default, false
	}
	return bestAction, true
}

// specificity counts pattern's non-'*' runes: more literal characters
// means a more specific (higher-priority) pattern.
func specificity(pattern string) int {
	n := 0
	for _, r := range pattern {
		if r != '*' {
			n++
		}
	}
	return n
}

// actionRank orders Actions so a tie between equally specific patterns can
// favor the more restrictive one: Deny > Ask > Allow.
func actionRank(a core.Action) int {
	switch a {
	case core.Deny:
		return 2
	case core.Ask:
		return 1
	default:
		return 0
	}
}

// Match reports whether pattern matches subject as an anchored, full-string
// glob: '*' matches any run of characters (including none, and including
// '/'), and every other rune must match literally.
func Match(pattern, subject string) bool {
	p, s := []rune(pattern), []rune(subject)
	pi, si := 0, 0
	starIdx, matchIdx := -1, 0

	for si < len(s) {
		switch {
		case pi < len(p) && p[pi] == s[si]:
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			starIdx, matchIdx = pi, si
			pi++
		case starIdx != -1:
			pi = starIdx + 1
			matchIdx++
			si = matchIdx
		default:
			return false
		}
	}

	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
