package trust

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func boolPtr(b bool) *bool { return &b }

func effectStrings(effects []Effect) []string {
	out := make([]string, len(effects))
	for i, e := range effects {
		out[i] = e.String()
	}
	return out
}

func TestEffect_String(t *testing.T) {
	tests := []struct {
		e    Effect
		want string
	}{
		{Effect{Key: "permissions.bash", Value: "allow"}, "permissions.bash → allow"},
		{Effect{Key: `permissions.bash "git push*"`, Value: "deny"}, `permissions.bash "git push*" → deny`},
		{Effect{Key: "instructions", Value: "docs/rules.md"}, "instructions += docs/rules.md"},
		{Effect{Key: "skills.paths", Value: "/p/skills"}, "skills.paths += /p/skills"},
		{Effect{Key: "agents.reviewer prompt", Source: "/p/r.md"}, "agents.reviewer (from /p/r.md) prompt"},
		{Effect{Key: "agents.reviewer permissions.edit", Value: "allow", Source: "/p/.claude/agents/reviewer.md"},
			"agents.reviewer (from /p/.claude/agents/reviewer.md) permissions.edit → allow"},
		{Effect{Key: `agents."my agent" prompt`, Source: "/p/my agent.md"}, `agents."my agent" (from /p/my agent.md) prompt`},
		{Effect{Key: "agents.reviewer prompt"}, "agents.reviewer prompt"},
		{Effect{Key: "theme", Value: "x", Source: "/p/c.toml"}, "theme (from /p/c.toml) → x"},
	}
	for _, tt := range tests {
		if got := tt.e.String(); got != tt.want {
			t.Errorf("%#v.String() = %q, want %q", tt.e, got, tt.want)
		}
	}
}

func everyKindLayers() Layers {
	return Layers{
		Project: core.Config{
			DefaultModel:  "anthropic/claude-opus-5-5",
			DefaultEffort: "high",
			SmallModel:    "anthropic/claude-haiku-4-5",
			Theme:         "dark",
			AgentBrowser:  core.ToggleTrue,
			Providers: map[string]core.ProviderConfig{"anthropic": {
				Type:        "anthropic",
				APIKey:      "sk-secret",
				BaseURL:     "https://x",
				Models:      []string{"m1", "m2"},
				Options:     map[string]any{"token": "opt-secret"},
				ImageModels: []string{"m1"},
				Efforts:     []string{"low", "high"},
			}},
			Instructions: []string{"docs/rules.md"},
			SkillPaths:   []string{"/p/skills"},
			ModelAliases: map[string]string{"fast": "anthropic/claude-haiku-4-5"},
			Keybinds:     map[string]string{"quit": "ctrl+q"},
			Permissions: core.PermissionRules{
				"bash": {Default: core.Allow, Patterns: map[string]core.Action{"git push*": core.Deny}},
			},
			Agents: map[string]core.AgentConfig{"reviewer": {
				Description: "Reviews code.",
				Mode:        "subagent",
				Model:       "fast",
				Effort:      "low",
				Prompt:      "You review.",
				MaxSteps:    5,
				CanSpawn:    boolPtr(true),
				Hidden:      boolPtr(false),
				Tools:       []string{"read", "grep"},
				Permissions: core.PermissionRules{"edit": {Default: core.Deny}},
				Source:      "/p/.jig/config.toml",
			}},
		},
		ProjectMD: map[string]core.AgentConfig{"reviewer": {
			Prompt:      "Body.",
			Permissions: core.PermissionRules{"edit": {Default: core.Allow}},
			Source:      "/p/.claude/agents/reviewer.md",
		}},
	}
}

func TestEffects_SortedAndComplete(t *testing.T) {
	const md = "(from /p/.claude/agents/reviewer.md)"
	want := []string{
		"agents.reviewer can_spawn → true",
		"agents.reviewer description",
		"agents.reviewer effort → low",
		"agents.reviewer hidden → false",
		"agents.reviewer max_steps → 5",
		"agents.reviewer mode → subagent",
		"agents.reviewer model → fast",
		"agents.reviewer " + md + " permissions.edit → allow",
		"agents.reviewer permissions.edit → deny",
		"agents.reviewer prompt",
		"agents.reviewer " + md + " prompt",
		"agents.reviewer tools → read, grep",
		"default_effort → high",
		"default_model → anthropic/claude-opus-5-5",
		"instructions += docs/rules.md",
		"integrations.agent_browser.enabled → true",
		"keybinds.quit → ctrl+q",
		"model_aliases.fast → anthropic/claude-haiku-4-5",
		"permissions.bash → allow",
		`permissions.bash "git push*" → deny`,
		"providers.anthropic.api_key → (set)",
		"providers.anthropic.base_url → https://x",
		"providers.anthropic.efforts → low, high",
		"providers.anthropic.image_models → m1",
		"providers.anthropic.models → m1, m2",
		"providers.anthropic.options → (set)",
		"providers.anthropic.type → anthropic",
		"skills.paths += /p/skills",
		"small_model → anthropic/claude-haiku-4-5",
		"theme → dark",
	}
	got := effectStrings(Effects(everyKindLayers()))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effects =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEffects_IgnoresGlobalLayers(t *testing.T) {
	l := Layers{
		Global:   core.Config{DefaultModel: "a/b", Permissions: core.PermissionRules{"bash": {Default: core.Allow}}},
		GlobalMD: map[string]core.AgentConfig{"x": {Prompt: "p", Source: "/g/x.md"}},
	}
	if got := Effects(l); len(got) != 0 {
		t.Errorf("Effects = %v, want none", effectStrings(got))
	}
}

func TestEffects_NeverPrintsSecrets(t *testing.T) {
	l := Layers{Project: core.Config{Providers: map[string]core.ProviderConfig{
		"anthropic": {APIKey: "sk-secret", Options: map[string]any{"key": "sk-secret"}},
		"proxy":     {BaseURL: "https://user:sk-secret@proxy.example/v1"},
		"proxy2":    {BaseURL: "https://sk-secret@proxy.example/v1"},
		"proxy3":    {BaseURL: "https://proxy.example/v1?key=sk-secret"},
		"proxy4":    {BaseURL: "%%sk-secret"},
	}}}
	effects := Effects(l)
	if len(effects) != 6 {
		t.Errorf("got %d effects, want 6: %v", len(effects), effectStrings(effects))
	}
	for _, e := range effects {
		if s := e.String(); strings.Contains(s, "sk-secret") {
			t.Errorf("effect leaks a secret: %q", s)
		}
	}
	_, dropped := Restrict(l)
	for _, e := range dropped {
		if s := e.String(); strings.Contains(s, "sk-secret") {
			t.Errorf("dropped effect leaks a secret: %q", s)
		}
	}
}

func TestEffects_QuotesOddNames(t *testing.T) {
	l := Layers{Project: core.Config{
		Permissions:  core.PermissionRules{"my tool": {Patterns: map[string]core.Action{"a\nb": core.Deny}}},
		ModelAliases: map[string]string{"x y": "a/b"},
	}}
	want := []string{
		`model_aliases."x y" → a/b`,
		`permissions."my tool" "a\nb" → deny`,
	}
	if got := effectStrings(Effects(l)); !reflect.DeepEqual(got, want) {
		t.Errorf("Effects = %q, want %q", got, want)
	}
}

func providerEffectStrings(pc core.ProviderConfig) []string {
	return effectStrings(Effects(Layers{Project: core.Config{Providers: map[string]core.ProviderConfig{"p": pc}}}))
}

func TestEffects_BaseURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://evil.example/v1?x", "https://evil.example/v1?…"},
		{"https://evil.example/v1?key=sekrit#frag", "https://evil.example/v1?…"},
		{"https://user:pw@h/p", "https://h/p"},
		{"http://h:8080/v1/", "http://h:8080/v1/"},
		{"https://h/p#frag", "https://h/p"},
		{"{env:BASE_URL}", "{env:BASE_URL}"},
		{"https://{env:HOST}/v1", "https://{env:HOST}/v1"},
		// Credentials placed in the path are shown: the path is part of
		// what the user must see to judge where requests go.
		{"https://h/sk-literal/v1", "https://h/sk-literal/v1"},
	}
	for _, c := range cases {
		got := providerEffectStrings(core.ProviderConfig{BaseURL: c.in})
		want := []string{"providers.p.base_url → " + c.want}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("base_url %q: effects = %v, want %v", c.in, got, want)
		}
	}
}

func TestEffects_SecretsShowTokensNotLiterals(t *testing.T) {
	cases := []struct {
		pc   core.ProviderConfig
		want string
	}{
		{core.ProviderConfig{APIKey: "sk-live-123"}, "providers.p.api_key → (set)"},
		{core.ProviderConfig{APIKey: "{env:ANTHROPIC_API_KEY}"}, "providers.p.api_key → {env:ANTHROPIC_API_KEY}"},
		{core.ProviderConfig{APIKey: "sk-{file:/k}"}, "providers.p.api_key → {file:/k}"},
		{core.ProviderConfig{Options: map[string]any{"org": "o-123"}}, "providers.p.options → (set)"},
		{core.ProviderConfig{Options: map[string]any{"org": "{env:ORG}", "n": map[string]any{"h": "{file:/h}"}}}, "providers.p.options → {env:ORG}, {file:/h}"},
	}
	for _, c := range cases {
		got := providerEffectStrings(c.pc)
		if !reflect.DeepEqual(got, []string{c.want}) {
			t.Errorf("%+v: effects = %v, want [%s]", c.pc, got, c.want)
		}
	}
}
