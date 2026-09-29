package permission

import "github.com/gammons/jig/internal/core"

// Overlay applies hi over lo per tool (overlayRule semantics): hi's
// non-empty Default replaces lo's, and hi's Patterns are merged in, winning
// shared keys. It covers the union of both tools and never aliases either
// input's Patterns maps.
func Overlay(lo, hi core.PermissionRules) core.PermissionRules {
	out := make(core.PermissionRules, len(lo)+len(hi))
	for tool, r := range lo {
		out[tool] = overlayRule(r, hi[tool])
	}
	for tool, r := range hi {
		if _, ok := lo[tool]; !ok {
			out[tool] = overlayRule(r)
		}
	}
	return out
}

// Tighten splits add into the entries that can be overlaid on baseline
// (already Effective, Defaults included) without loosening any decision,
// and the entries it drops (R7). Per tool (an add key may itself be a
// glob; it is matched against baseline via RuleFor, so a glob key ties
// against whatever baseline entry — exact or glob — actually governs that
// key):
//
//   - a Default is kept iff it ranks at least the baseline's Default
//     (empty means ask);
//   - a deny pattern is always kept;
//   - an ask pattern is kept iff the baseline rule has no deny anywhere
//     (Default or pattern), since a more specific ask could otherwise win
//     over a baseline deny;
//   - an allow pattern is never kept, since it can out-specify any
//     baseline pattern.
//
// Anything that is not a valid Action is dropped. A tool absent from
// baseline is judged against an empty rule (ask), never against
// Defaults(). kept and dropped are fresh maps, nil when empty.
func Tighten(baseline, add core.PermissionRules) (kept, dropped core.PermissionRules) {
	for tool, r := range add {
		base := RuleFor(baseline, tool)
		var k, d core.Rule
		if r.Default != "" {
			if validAction(r.Default) && actionRank(r.Default) >= actionRank(defaultOf(base)) {
				k.Default = r.Default
			} else {
				d.Default = r.Default
			}
		}
		baseDenies := HasDeny(base)
		for pattern, action := range r.Patterns {
			if action == core.Deny || (action == core.Ask && !baseDenies) {
				k.Patterns = put(k.Patterns, pattern, action)
			} else {
				d.Patterns = put(d.Patterns, pattern, action)
			}
		}
		kept = putRule(kept, tool, k)
		dropped = putRule(dropped, tool, d)
	}
	return kept, dropped
}

func validAction(a core.Action) bool {
	return a == core.Allow || a == core.Ask || a == core.Deny
}

// defaultOf is r's effective Default: empty means Ask.
func defaultOf(r core.Rule) core.Action {
	if r.Default == "" {
		return core.Ask
	}
	return r.Default
}

// HasDeny reports whether r denies anything, by Default or by pattern.
func HasDeny(r core.Rule) bool {
	if r.Default == core.Deny {
		return true
	}
	for _, a := range r.Patterns {
		if a == core.Deny {
			return true
		}
	}
	return false
}

func put(m map[string]core.Action, pattern string, a core.Action) map[string]core.Action {
	if m == nil {
		m = make(map[string]core.Action)
	}
	m[pattern] = a
	return m
}

// putRule stores r under tool unless r is empty, allocating rules lazily.
func putRule(rules core.PermissionRules, tool string, r core.Rule) core.PermissionRules {
	if r.Default == "" && len(r.Patterns) == 0 {
		return rules
	}
	if rules == nil {
		rules = make(core.PermissionRules)
	}
	rules[tool] = r
	return rules
}
