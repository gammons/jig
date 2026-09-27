// Package permission evaluates tool-call permission rules, blocks or asks
// the user through an ext.ToolHook, and answers permission requests raised
// over the event bus.
package permission

import "github.com/gammons/jig/internal/core"

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

// Effective merges an agent's permission rules over a config's over
// Defaults(), per tool, covering the union of every tool named by any of
// the three. Precedence (highest first) is agent, cfg, Defaults(): a
// tool's Default comes from the highest-precedence source that sets it,
// and its Patterns are merged, with a higher-precedence source winning a
// pattern key it shares with a lower one.
func Effective(agent, cfg core.PermissionRules) core.PermissionRules {
	defaults := Defaults()

	tools := make(map[string]struct{}, len(defaults)+len(cfg)+len(agent))
	for _, rules := range []core.PermissionRules{defaults, cfg, agent} {
		for tool := range rules {
			tools[tool] = struct{}{}
		}
	}

	out := make(core.PermissionRules, len(tools))
	for tool := range tools {
		out[tool] = overlayRule(defaults[tool], cfg[tool], agent[tool])
	}
	return out
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
			return core.Ask
		}
		return r.Default
	}
	return bestAction
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
