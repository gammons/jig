package config

import (
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/gammons/jig/internal/core"
)

// decodeTOMLForTest is a merge_test.go-only helper that skips substitution:
// it decodes s directly into a tomlFile so merge unit tests can focus on
// the merge logic itself.
func decodeTOMLForTest(t *testing.T, s string) (tomlFile, toml.MetaData) {
	t.Helper()
	var dto tomlFile
	md, err := toml.Decode(s, &dto)
	if err != nil {
		t.Fatalf("toml.Decode: %v", err)
	}
	return dto, md
}

func TestMerge_PermissionsPatternsKeyMerge(t *testing.T) {
	var s state
	dto1, md1 := decodeTOMLForTest(t, `
[permissions.bash]
"git status*" = "allow"
"*" = "ask"
`)
	s.apply(dto1, md1, "file1", "/dir1", "/home/u")

	dto2, md2 := decodeTOMLForTest(t, `
[permissions.bash]
"rm *" = "deny"
`)
	s.apply(dto2, md2, "file2", "/dir2", "/home/u")

	rule := s.cfg.Permissions["bash"]
	want := map[string]core.Action{
		"git status*": core.Allow,
		"*":           core.Ask,
		"rm *":        core.Deny,
	}
	if !reflect.DeepEqual(rule.Patterns, want) {
		t.Errorf("Patterns = %v, want %v", rule.Patterns, want)
	}
}

func TestMerge_PermissionsDefaultReplacedIfSet(t *testing.T) {
	var s state
	dto1, md1 := decodeTOMLForTest(t, `permissions.bash = "deny"`)
	s.apply(dto1, md1, "file1", "/dir1", "/home/u")

	dto2, md2 := decodeTOMLForTest(t, `
[permissions.bash]
"safe *" = "allow"
`)
	s.apply(dto2, md2, "file2", "/dir2", "/home/u")

	rule := s.cfg.Permissions["bash"]
	if rule.Default != core.Deny {
		t.Errorf("Default = %q, want %q (a table entry with no Default must not clear it)", rule.Default, core.Deny)
	}
	if rule.Patterns["safe *"] != core.Allow {
		t.Errorf("Patterns[safe *] = %q, want allow", rule.Patterns["safe *"])
	}
}

func TestMerge_InstructionsAppendDedup(t *testing.T) {
	var dst []string
	appendDedup(&dst, []string{"AGENTS.md", "/abs/shared.md"}, "/global/dir", "/home/u")
	appendDedup(&dst, []string{"/abs/shared.md", "PROJECT.md"}, "/project/dir", "/home/u")

	want := []string{"/global/dir/AGENTS.md", "/abs/shared.md", "/project/dir/PROJECT.md"}
	if !reflect.DeepEqual(dst, want) {
		t.Errorf("dst = %v, want %v", dst, want)
	}
}

func TestMerge_AgentSourceIsLastFileToTouchIt(t *testing.T) {
	var s state
	dto1, md1 := decodeTOMLForTest(t, `
[agents.explore]
model = "openai/gpt-5"
`)
	s.apply(dto1, md1, "global.toml", "/global", "/home/u")

	dto2, md2 := decodeTOMLForTest(t, `
[agents.explore]
max_steps = 12
`)
	s.apply(dto2, md2, "project.toml", "/project", "/home/u")

	agent := s.cfg.Agents["explore"]
	if agent.Model != "openai/gpt-5" {
		t.Errorf("Model = %q, want %q", agent.Model, "openai/gpt-5")
	}
	if agent.MaxSteps != 12 {
		t.Errorf("MaxSteps = %d, want 12", agent.MaxSteps)
	}
	if agent.Source != "project.toml" {
		t.Errorf("Source = %q, want %q", agent.Source, "project.toml")
	}
}

func TestMerge_ScalarUnsetDoesNotClearPrevious(t *testing.T) {
	var s state
	dto1, md1 := decodeTOMLForTest(t, `theme = "dark"`)
	s.apply(dto1, md1, "file1", "/dir1", "/home/u")

	dto2, md2 := decodeTOMLForTest(t, `default_model = "x/y"`)
	s.apply(dto2, md2, "file2", "/dir2", "/home/u")

	if s.cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want %q (unset in file2 must not clear it)", s.cfg.Theme, "dark")
	}
	if s.cfg.DefaultModel != "x/y" {
		t.Errorf("DefaultModel = %q, want %q", s.cfg.DefaultModel, "x/y")
	}
}

// TestMerge_Table exercises Merge(lo, hi), one subtest per per-key rule
// from the package doc: scalars last-defined wins (an empty hi scalar
// keeps lo), providers field by field, permissions rule by rule (Default
// replaced if set, Patterns merged by key), model_aliases/keybinds by key,
// instructions/skills.paths appended and de-duplicated, agents field by
// field with Source from whichever layer touched it, and AgentBrowser.
func TestMerge_Table(t *testing.T) {
	t.Run("scalar: hi wins when set", func(t *testing.T) {
		got := Merge(core.Config{DefaultModel: "lo/model"}, core.Config{DefaultModel: "hi/model"})
		if got.DefaultModel != "hi/model" {
			t.Errorf("DefaultModel = %q, want %q", got.DefaultModel, "hi/model")
		}
	})

	t.Run("scalar: empty hi keeps lo", func(t *testing.T) {
		got := Merge(core.Config{Theme: "dark"}, core.Config{})
		if got.Theme != "dark" {
			t.Errorf("Theme = %q, want %q (empty hi must not clear lo)", got.Theme, "dark")
		}
	})

	t.Run("AgentBrowser wins when set", func(t *testing.T) {
		got := Merge(core.Config{AgentBrowser: core.ToggleFalse}, core.Config{AgentBrowser: core.ToggleAuto})
		if got.AgentBrowser != core.ToggleAuto {
			t.Errorf("AgentBrowser = %q, want %q", got.AgentBrowser, core.ToggleAuto)
		}
	})

	t.Run("AgentBrowser: empty hi keeps lo", func(t *testing.T) {
		got := Merge(core.Config{AgentBrowser: core.ToggleTrue}, core.Config{})
		if got.AgentBrowser != core.ToggleTrue {
			t.Errorf("AgentBrowser = %q, want %q", got.AgentBrowser, core.ToggleTrue)
		}
	})

	t.Run("providers: field by field, Options replaced wholesale", func(t *testing.T) {
		lo := core.Config{Providers: map[string]core.ProviderConfig{
			"openai": {Type: "openai", BaseURL: "https://api.openai.com/v1", Options: map[string]any{"temperature": 0.5}},
		}}
		hi := core.Config{Providers: map[string]core.ProviderConfig{
			"openai": {APIKey: "sk-test", Options: map[string]any{"max_tokens": 2048}, ImageModels: []string{"gpt-4o"}},
		}}
		got := Merge(lo, hi)
		prov := got.Providers["openai"]
		if prov.Type != "openai" {
			t.Errorf("Type = %q, want %q (unset in hi must survive)", prov.Type, "openai")
		}
		if prov.APIKey != "sk-test" {
			t.Errorf("APIKey = %q, want %q", prov.APIKey, "sk-test")
		}
		wantOpts := map[string]any{"max_tokens": 2048}
		if !reflect.DeepEqual(prov.Options, wantOpts) {
			t.Errorf("Options = %v, want %v (hi's non-nil map replaces lo's wholesale)", prov.Options, wantOpts)
		}
		if !reflect.DeepEqual(prov.ImageModels, []string{"gpt-4o"}) {
			t.Errorf("ImageModels = %v, want %v", prov.ImageModels, []string{"gpt-4o"})
		}
		// lo must be untouched.
		if lo.Providers["openai"].APIKey != "" {
			t.Errorf("Merge mutated lo.Providers[openai].APIKey = %q, want empty", lo.Providers["openai"].APIKey)
		}
	})

	t.Run("permissions: Default replaced if set, Patterns merged by key", func(t *testing.T) {
		lo := core.Config{Permissions: core.PermissionRules{
			"bash": {Default: core.Deny, Patterns: map[string]core.Action{"git status*": core.Allow}},
		}}
		hi := core.Config{Permissions: core.PermissionRules{
			"bash": {Patterns: map[string]core.Action{"rm *": core.Deny}},
		}}
		got := Merge(lo, hi)
		rule := got.Permissions["bash"]
		if rule.Default != core.Deny {
			t.Errorf("Default = %q, want %q (hi did not set one)", rule.Default, core.Deny)
		}
		want := map[string]core.Action{"git status*": core.Allow, "rm *": core.Deny}
		if !reflect.DeepEqual(rule.Patterns, want) {
			t.Errorf("Patterns = %v, want %v", rule.Patterns, want)
		}
		if lo.Permissions["bash"].Patterns["rm *"] != "" {
			t.Error("Merge mutated lo.Permissions[bash].Patterns")
		}
	})

	t.Run("model_aliases and keybinds merge by key", func(t *testing.T) {
		lo := core.Config{
			ModelAliases: map[string]string{"fast": "openai/gpt-4-mini"},
			Keybinds:     map[string]string{"quit": "ctrl+c"},
		}
		hi := core.Config{
			ModelAliases: map[string]string{"fast": "anthropic/haiku", "smart": "anthropic/opus"},
		}
		got := Merge(lo, hi)
		wantAliases := map[string]string{"fast": "anthropic/haiku", "smart": "anthropic/opus"}
		if !reflect.DeepEqual(got.ModelAliases, wantAliases) {
			t.Errorf("ModelAliases = %v, want %v", got.ModelAliases, wantAliases)
		}
		wantKeybinds := map[string]string{"quit": "ctrl+c"}
		if !reflect.DeepEqual(got.Keybinds, wantKeybinds) {
			t.Errorf("Keybinds = %v, want %v (hi did not set any)", got.Keybinds, wantKeybinds)
		}
	})

	t.Run("instructions and skills.paths appended and de-duplicated", func(t *testing.T) {
		lo := core.Config{
			Instructions: []string{"/global/AGENTS.md", "/abs/shared.md"},
			SkillPaths:   []string{"/global/skills"},
		}
		hi := core.Config{
			Instructions: []string{"/abs/shared.md", "/project/PROJECT.md"},
			SkillPaths:   []string{"/global/skills", "/project/skills"},
		}
		got := Merge(lo, hi)
		wantInstr := []string{"/global/AGENTS.md", "/abs/shared.md", "/project/PROJECT.md"}
		if !reflect.DeepEqual(got.Instructions, wantInstr) {
			t.Errorf("Instructions = %v, want %v", got.Instructions, wantInstr)
		}
		wantSkills := []string{"/global/skills", "/project/skills"}
		if !reflect.DeepEqual(got.SkillPaths, wantSkills) {
			t.Errorf("SkillPaths = %v, want %v", got.SkillPaths, wantSkills)
		}
	})

	t.Run("agents: field by field, Source from the layer that touched it", func(t *testing.T) {
		canSpawn := true
		lo := core.Config{Agents: map[string]core.AgentConfig{
			"explore": {Model: "openai/gpt-5", CanSpawn: &canSpawn, Source: "global.toml"},
		}}
		hi := core.Config{Agents: map[string]core.AgentConfig{
			"explore": {MaxSteps: 12, Source: "project.toml"},
		}}
		got := Merge(lo, hi)
		agent := got.Agents["explore"]
		if agent.Model != "openai/gpt-5" {
			t.Errorf("Model = %q, want %q (unset in hi must survive)", agent.Model, "openai/gpt-5")
		}
		if agent.MaxSteps != 12 {
			t.Errorf("MaxSteps = %d, want 12", agent.MaxSteps)
		}
		if agent.Source != "project.toml" {
			t.Errorf("Source = %q, want %q (hi touched the agent)", agent.Source, "project.toml")
		}
	})

	t.Run("agents: CanSpawn nil in hi keeps lo", func(t *testing.T) {
		canSpawn := true
		lo := core.Config{Agents: map[string]core.AgentConfig{
			"explore": {CanSpawn: &canSpawn},
		}}
		hi := core.Config{Agents: map[string]core.AgentConfig{
			"explore": {Description: "override"},
		}}
		got := Merge(lo, hi)
		agent := got.Agents["explore"]
		if agent.CanSpawn == nil || *agent.CanSpawn != true {
			t.Errorf("CanSpawn = %v, want pointer to true (nil in hi must not clear lo)", agent.CanSpawn)
		}
		if agent.Description != "override" {
			t.Errorf("Description = %q, want %q", agent.Description, "override")
		}
	})

	t.Run("agents: no hi entry keeps lo untouched, including Source", func(t *testing.T) {
		lo := core.Config{Agents: map[string]core.AgentConfig{
			"explore": {Model: "openai/gpt-5", Source: "global.toml"},
		}}
		got := Merge(lo, core.Config{})
		agent := got.Agents["explore"]
		if agent.Source != "global.toml" {
			t.Errorf("Source = %q, want %q (hi set nothing)", agent.Source, "global.toml")
		}
	})
}

func TestMerge_EffortFields(t *testing.T) {
	lo := core.Config{
		DefaultEffort: "high",
		Providers:     map[string]core.ProviderConfig{"p": {Efforts: []string{"low"}}},
		Agents:        map[string]core.AgentConfig{"a": {Effort: "low"}},
	}
	hi := core.Config{
		Providers: map[string]core.ProviderConfig{"p": {Efforts: []string{"high"}}},
		Agents:    map[string]core.AgentConfig{"a": {Effort: "max"}},
	}
	got := Merge(lo, hi)
	if got.DefaultEffort != "high" {
		t.Errorf("DefaultEffort = %q, want lo's high (hi leaves it unset)", got.DefaultEffort)
	}
	if !reflect.DeepEqual(got.Providers["p"].Efforts, []string{"high"}) {
		t.Errorf("Providers[p].Efforts = %v, want [high]", got.Providers["p"].Efforts)
	}
	if got.Agents["a"].Effort != "max" {
		t.Errorf("Agents[a].Effort = %q, want max", got.Agents["a"].Effort)
	}
}
