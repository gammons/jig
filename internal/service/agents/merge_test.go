package agents

import (
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestNew_BuiltinsPresent(t *testing.T) {
	svc, err := New(core.Config{}, Sources{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	build, ok := svc.Get("build")
	if !ok {
		t.Fatal(`Get("build") = false, want true`)
	}
	if build.Mode != core.ModePrimary {
		t.Errorf("build.Mode = %q, want %q", build.Mode, core.ModePrimary)
	}
	if !build.CanSpawn {
		t.Error("build.CanSpawn = false, want true")
	}
	if build.MaxSteps != 100 {
		t.Errorf("build.MaxSteps = %d, want 100", build.MaxSteps)
	}
	if build.Tools != nil {
		t.Errorf("build.Tools = %v, want nil (all tools)", build.Tools)
	}
	if build.Prompt == "" {
		t.Error("build.Prompt is empty")
	}

	plan, ok := svc.Get("plan")
	if !ok {
		t.Fatal(`Get("plan") = false, want true`)
	}
	if plan.Mode != core.ModePrimary {
		t.Errorf("plan.Mode = %q, want %q", plan.Mode, core.ModePrimary)
	}
	if !plan.CanSpawn {
		t.Error("plan.CanSpawn = false, want true")
	}
	for _, tool := range []string{"write", "edit", "bash"} {
		rule, ok := plan.Permissions[tool]
		if !ok || rule.Default != core.Ask {
			t.Errorf("plan.Permissions[%q] = %+v, ok=%v, want Default=ask", tool, rule, ok)
		}
	}

	explore, ok := svc.Get("explore")
	if !ok {
		t.Fatal(`Get("explore") = false, want true`)
	}
	if explore.Mode != core.ModeSubagent {
		t.Errorf("explore.Mode = %q, want %q", explore.Mode, core.ModeSubagent)
	}
	wantTools := []string{"read", "glob", "grep", "skill"}
	if len(explore.Tools) != len(wantTools) {
		t.Fatalf("explore.Tools = %v, want %v", explore.Tools, wantTools)
	}
	for i, tool := range wantTools {
		if explore.Tools[i] != tool {
			t.Errorf("explore.Tools[%d] = %q, want %q", i, explore.Tools[i], tool)
		}
	}
	if explore.MaxSteps != 40 {
		t.Errorf("explore.MaxSteps = %d, want 40", explore.MaxSteps)
	}
	if !strings.Contains(explore.Prompt, "read-only; report findings with file paths and line numbers") {
		t.Errorf("explore.Prompt = %q, missing required phrase", explore.Prompt)
	}

	general, ok := svc.Get("general")
	if !ok {
		t.Fatal(`Get("general") = false, want true`)
	}
	if general.Mode != core.ModeSubagent {
		t.Errorf("general.Mode = %q, want %q", general.Mode, core.ModeSubagent)
	}
	if general.CanSpawn {
		t.Error("general.CanSpawn = true, want false")
	}
	if general.Tools != nil {
		t.Errorf("general.Tools = %v, want nil (all tools)", general.Tools)
	}

	for _, name := range []string{"title", "compaction"} {
		a, ok := svc.Get(name)
		if !ok {
			t.Fatalf("Get(%q) = false, want true", name)
		}
		if !a.Hidden {
			t.Errorf("%s.Hidden = false, want true", name)
		}
		if a.Tools == nil || len(a.Tools) != 0 {
			t.Errorf("%s.Tools = %v, want empty non-nil slice", name, a.Tools)
		}
		if a.Mode != core.ModeSubagent {
			t.Errorf("%s.Mode = %q, want %q", name, a.Mode, core.ModeSubagent)
		}
	}
}

func TestMerge_PrecedenceOrder(t *testing.T) {
	cfg := core.Config{}
	src := Sources{
		GlobalTOML: map[string]core.AgentConfig{
			"build": {Description: "global-toml-desc", MaxSteps: 55},
		},
		GlobalMD: map[string]core.AgentConfig{
			"build": {Description: "global-md-desc"},
		},
		ProjectTOML: map[string]core.AgentConfig{
			"build": {Description: "project-toml-desc"},
		},
		ProjectMD: map[string]core.AgentConfig{
			"build": {Description: "project-md-desc"},
		},
	}

	svc, err := New(cfg, src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	build, ok := svc.Get("build")
	if !ok {
		t.Fatal(`Get("build") = false`)
	}
	if build.Description != "project-md-desc" {
		t.Errorf("build.Description = %q, want %q (project MD wins)", build.Description, "project-md-desc")
	}
	if build.MaxSteps != 55 {
		t.Errorf("build.MaxSteps = %d, want 55 (set only in global TOML, must survive)", build.MaxSteps)
	}
	// Mode was never overridden by any layer, so the built-in's mode
	// (primary) must be kept.
	if build.Mode != core.ModePrimary {
		t.Errorf("build.Mode = %q, want %q", build.Mode, core.ModePrimary)
	}
}

func TestMerge_UnknownAliasErrors(t *testing.T) {
	cfg := core.Config{} // no [model_aliases] entries at all
	src := Sources{
		ProjectMD: map[string]core.AgentConfig{
			"custom": {Model: "haiku"},
		},
	}
	_, err := New(cfg, src)
	if err == nil {
		t.Fatal("New: want error for undefined alias, got nil")
	}
	want := `agent "custom": model alias "haiku" is not defined in [model_aliases]`
	if err.Error() != want {
		t.Errorf("New: err = %q, want %q", err.Error(), want)
	}
}

func TestMerge_InvalidModeErrors(t *testing.T) {
	cfg := core.Config{}
	src := Sources{
		ProjectMD: map[string]core.AgentConfig{
			"custom": {Mode: "bogus"},
		},
	}
	_, err := New(cfg, src)
	if err == nil {
		t.Fatal("New: want error for invalid mode, got nil")
	}
	want := `agent "custom": invalid mode "bogus"`
	if err.Error() != want {
		t.Errorf("New: err = %q, want %q", err.Error(), want)
	}
}

func TestMerge_DefaultModeAll(t *testing.T) {
	t.Run("new agent with no built-in inherits ModeAll", func(t *testing.T) {
		src := Sources{
			ProjectMD: map[string]core.AgentConfig{
				"custom": {Description: "a claude-code style agent"},
			},
		}
		svc, err := New(core.Config{}, src)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		custom, ok := svc.Get("custom")
		if !ok {
			t.Fatal(`Get("custom") = false`)
		}
		if custom.Mode != core.ModeAll {
			t.Errorf("custom.Mode = %q, want %q", custom.Mode, core.ModeAll)
		}
	})

	t.Run("overlay of a built-in keeps the built-in mode when unset", func(t *testing.T) {
		src := Sources{
			ProjectMD: map[string]core.AgentConfig{
				"general": {Description: "overridden description"},
			},
		}
		svc, err := New(core.Config{}, src)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		general, ok := svc.Get("general")
		if !ok {
			t.Fatal(`Get("general") = false`)
		}
		if general.Mode != core.ModeSubagent {
			t.Errorf("general.Mode = %q, want %q (built-in mode kept)", general.Mode, core.ModeSubagent)
		}
	})
}

func TestPrimary_Order(t *testing.T) {
	src := Sources{
		ProjectMD: map[string]core.AgentConfig{
			"zeta":  {}, // no mode -> ModeAll -> included in Primary
			"alpha": {}, // no mode -> ModeAll -> included in Primary
		},
	}
	svc, err := New(core.Config{}, src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	primary := svc.Primary()
	var names []string
	for _, a := range primary {
		names = append(names, a.Name)
	}
	want := []string{"build", "plan", "alpha", "zeta"}
	if len(names) != len(want) {
		t.Fatalf("Primary() names = %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Errorf("Primary()[%d] = %q, want %q (full: %v)", i, names[i], name, names)
		}
	}

	// Subagents is sorted purely alphabetically, and excludes hidden agents.
	subs := svc.Subagents()
	var subNames []string
	for _, a := range subs {
		subNames = append(subNames, a.Name)
	}
	for i := 1; i < len(subNames); i++ {
		if subNames[i-1] > subNames[i] {
			t.Errorf("Subagents() not sorted: %v", subNames)
			break
		}
	}
	for _, hidden := range []string{"title", "compaction"} {
		for _, n := range subNames {
			if n == hidden {
				t.Errorf("Subagents() includes hidden agent %q", hidden)
			}
		}
	}
}

func TestMerge_Effort(t *testing.T) {
	svc, err := New(core.Config{}, Sources{GlobalTOML: map[string]core.AgentConfig{"plan": {Effort: "High"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.Get("plan"); a.Effort != core.EffortHigh {
		t.Errorf("plan.Effort = %q, want high", a.Effort)
	}

	_, err = New(core.Config{}, Sources{ProjectMD: map[string]core.AgentConfig{"custom": {Effort: "turbo"}}})
	if err == nil || !strings.HasPrefix(err.Error(), `agent "custom": unknown effort "turbo"`) {
		t.Errorf("err = %v, want an unknown-effort error naming the agent", err)
	}
}
