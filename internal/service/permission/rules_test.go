package permission

import (
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, subject string
		want             bool
	}{
		{"git status*", "git status --short", true},
		{"git status*", "git status", true},
		{"git status*", "git diff", false},
		{"*", "anything at all", true},
		{"*", "", true},
		{"foo", "foo", true},
		{"foo", "foobar", false},
		{"*.go", "main.go", true},
		{"*.go", "main.go.bak", false},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "ac", false},
		{"a*b*c", "abc", true},
		{"read", "readd", false},
		{"", "", true},
		{"", "x", false},
	}
	for _, tt := range tests {
		if got := Match(tt.pattern, tt.subject); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.subject, got, tt.want)
		}
	}
}

func TestEvaluate_MostSpecificWins(t *testing.T) {
	r := core.Rule{
		Default: core.Deny,
		Patterns: map[string]core.Action{
			"git status*": core.Allow,
			"git *":       core.Ask,
		},
	}
	got := Evaluate(r, "git status --short")
	if got != core.Allow {
		t.Errorf("Evaluate = %q, want %q (more-literal pattern must win)", got, core.Allow)
	}
}

func TestEvaluate_TieDenyWins(t *testing.T) {
	// "a*" and "*a" both match "a" and both have exactly one literal char,
	// so they tie on specificity; deny must win the tie regardless of map
	// iteration order.
	r := core.Rule{
		Default: core.Allow,
		Patterns: map[string]core.Action{
			"a*": core.Allow,
			"*a": core.Deny,
		},
	}
	got := Evaluate(r, "a")
	if got != core.Deny {
		t.Errorf("Evaluate = %q, want %q (tie must favor deny)", got, core.Deny)
	}
}

func TestEvaluate_EmptyDefaultIsAsk(t *testing.T) {
	r := core.Rule{}
	got := Evaluate(r, "anything")
	if got != core.Ask {
		t.Errorf("Evaluate = %q, want %q", got, core.Ask)
	}
}

func TestEvaluate_NoMatchFallsBackToDefault(t *testing.T) {
	r := core.Rule{
		Default: core.Deny,
		Patterns: map[string]core.Action{
			"git status*": core.Allow,
		},
	}
	got := Evaluate(r, "git push")
	if got != core.Deny {
		t.Errorf("Evaluate = %q, want %q", got, core.Deny)
	}
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	allow := []string{"read", "glob", "grep", "todo", "task", "skill"}
	ask := []string{"write", "edit", "bash"}
	for _, tool := range allow {
		if d[tool].Default != core.Allow {
			t.Errorf("Defaults()[%q].Default = %q, want %q", tool, d[tool].Default, core.Allow)
		}
	}
	for _, tool := range ask {
		if d[tool].Default != core.Ask {
			t.Errorf("Defaults()[%q].Default = %q, want %q", tool, d[tool].Default, core.Ask)
		}
	}
}

func TestEffective_Precedence(t *testing.T) {
	agent := core.PermissionRules{
		"bash": {Default: core.Allow, Patterns: map[string]core.Action{"rm *": core.Deny}},
	}
	cfg := core.PermissionRules{
		"bash":  {Default: core.Deny, Patterns: map[string]core.Action{"git *": core.Allow}},
		"write": {Default: core.Deny},
	}

	eff := Effective(agent, cfg)

	// bash.Default: agent sets it, so agent wins over cfg.
	if eff["bash"].Default != core.Allow {
		t.Errorf("eff[bash].Default = %q, want %q (agent wins)", eff["bash"].Default, core.Allow)
	}
	// bash.Patterns: merged from both cfg and agent.
	if eff["bash"].Patterns["rm *"] != core.Deny {
		t.Errorf(`eff[bash].Patterns["rm *"] = %q, want %q`, eff["bash"].Patterns["rm *"], core.Deny)
	}
	if eff["bash"].Patterns["git *"] != core.Allow {
		t.Errorf(`eff[bash].Patterns["git *"] = %q, want %q`, eff["bash"].Patterns["git *"], core.Allow)
	}
	// write: only cfg sets it (Defaults() also sets Ask, but cfg wins).
	if eff["write"].Default != core.Deny {
		t.Errorf("eff[write].Default = %q, want %q (cfg wins over Defaults())", eff["write"].Default, core.Deny)
	}
	// read: neither agent nor cfg sets it, so Defaults() applies.
	if eff["read"].Default != core.Allow {
		t.Errorf("eff[read].Default = %q, want %q (Defaults() applies)", eff["read"].Default, core.Allow)
	}
}
