package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/paths"
)

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_NoFilesGivesZeroConfig(t *testing.T) {
	root := t.TempDir()
	p := paths.Paths{Home: root, ConfigDir: filepath.Join(root, "config")}
	workDir := filepath.Join(root, "work")

	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.Config, core.Config{}) {
		t.Errorf("Config = %+v, want zero value", loaded.Config)
	}
	if len(loaded.Files) != 0 {
		t.Errorf("Files = %v, want empty", loaded.Files)
	}
	if len(loaded.GlobalAgents) != 0 {
		t.Errorf("GlobalAgents = %v, want empty", loaded.GlobalAgents)
	}
	if len(loaded.ProjectAgents) != 0 {
		t.Errorf("ProjectAgents = %v, want empty", loaded.ProjectAgents)
	}
}

func TestLoad_ProjectOverridesGlobalScalar(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
default_model = "global/model"
theme = "dark"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
default_model = "project/model"
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Config.DefaultModel != "project/model" {
		t.Errorf("DefaultModel = %q, want %q", loaded.Config.DefaultModel, "project/model")
	}
	if loaded.Config.Theme != "dark" {
		t.Errorf("Theme = %q, want %q (only global sets it)", loaded.Config.Theme, "dark")
	}
	wantFiles := []string{
		filepath.Join(configDir, "config.toml"),
		filepath.Join(workDir, ".jig", "config.toml"),
	}
	if !reflect.DeepEqual(loaded.Files, wantFiles) {
		t.Errorf("Files = %v, want %v", loaded.Files, wantFiles)
	}
}

func TestLoad_AgentsMergeFieldwise(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[agents.explore]
model = "openai/gpt-5"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[agents.explore]
max_steps = 12
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	agent, ok := loaded.Config.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", loaded.Config.Agents)
	}
	if agent.Model != "openai/gpt-5" {
		t.Errorf("Model = %q, want %q", agent.Model, "openai/gpt-5")
	}
	if agent.MaxSteps != 12 {
		t.Errorf("MaxSteps = %d, want 12", agent.MaxSteps)
	}
}

func TestLoad_ProvidersMergeFieldwise(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[providers.openai]
type = "openai"
base_url = "https://api.openai.com/v1"
models = ["gpt-4o"]

[providers.openai.options]
temperature = 0.5
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[providers.openai]
api_key = "sk-test"
models = ["gpt-4o", "gpt-4o-mini"]

[providers.openai.options]
max_tokens = 2048
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prov, ok := loaded.Config.Providers["openai"]
	if !ok {
		t.Fatalf("Providers[openai] missing, got %v", loaded.Config.Providers)
	}
	if prov.Type != "openai" {
		t.Errorf("Type = %q, want %q", prov.Type, "openai")
	}
	if prov.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want %q", prov.BaseURL, "https://api.openai.com/v1")
	}
	if prov.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want %q", prov.APIKey, "sk-test")
	}
	wantModels := []string{"gpt-4o", "gpt-4o-mini"}
	if !reflect.DeepEqual(prov.Models, wantModels) {
		t.Errorf("Models = %v, want %v (whole field replaced by the file that set it last)", prov.Models, wantModels)
	}
	// options is replaced wholesale by whichever file last set it (like any
	// other provider scalar field), not deep-merged key by key.
	wantOptions := map[string]any{"max_tokens": int64(2048)}
	if !reflect.DeepEqual(prov.Options, wantOptions) {
		t.Errorf("Options = %v, want %v", prov.Options, wantOptions)
	}
}

func TestLoad_AgentBoolFieldsMerge(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[agents.explore]
can_spawn = true
hidden = true
`)
	// The project file explicitly sets hidden = false, overriding the
	// global true; it does not mention can_spawn, so that must survive.
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[agents.explore]
hidden = false
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	agent, ok := loaded.Config.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", loaded.Config.Agents)
	}
	if agent.CanSpawn == nil || *agent.CanSpawn != true {
		t.Errorf("CanSpawn = %v, want pointer to true (unset in project, must survive from global)", agent.CanSpawn)
	}
	if agent.Hidden == nil || *agent.Hidden != false {
		t.Errorf("Hidden = %v, want pointer to false (project explicitly overrides global true)", agent.Hidden)
	}
}

func TestLoad_AgentPermissionsMerge(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[agents.explore.permissions.bash]
"git status*" = "allow"
"*" = "ask"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[agents.explore.permissions.bash]
"rm *" = "deny"
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	agent, ok := loaded.Config.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", loaded.Config.Agents)
	}
	rule := agent.Permissions["bash"]
	want := map[string]core.Action{
		"git status*": core.Allow,
		"*":           core.Ask,
		"rm *":        core.Deny,
	}
	if !reflect.DeepEqual(rule.Patterns, want) {
		t.Errorf("agents.explore.permissions[bash].Patterns = %v, want %v", rule.Patterns, want)
	}
}

func TestLoad_ModelAliasesAndKeybindsMergeByKey(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[model_aliases]
fast = "openai/gpt-4-mini"

[keybinds]
quit = "ctrl+c"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[model_aliases]
fast = "anthropic/haiku"
smart = "anthropic/opus"
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantAliases := map[string]string{
		"fast":  "anthropic/haiku", // project overrides the same key
		"smart": "anthropic/opus",  // project-only key survives
	}
	if !reflect.DeepEqual(loaded.Config.ModelAliases, wantAliases) {
		t.Errorf("ModelAliases = %v, want %v", loaded.Config.ModelAliases, wantAliases)
	}
	wantKeybinds := map[string]string{"quit": "ctrl+c"} // untouched by project file
	if !reflect.DeepEqual(loaded.Config.Keybinds, wantKeybinds) {
		t.Errorf("Keybinds = %v, want %v", loaded.Config.Keybinds, wantKeybinds)
	}
}

func TestLoad_ProjectAgentsMergeClickWinsAcrossMultipleFiles(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	rootFile := filepath.Join(repo, ".jig", "config.toml")
	subFile := filepath.Join(sub, ".jig", "config.toml")
	writeConfigFile(t, rootFile, `
[agents.explore]
description = "root desc"
model = "openai/gpt-5"
`)
	writeConfigFile(t, subFile, `
[agents.explore]
description = "sub desc"
max_steps = 12
`)

	p := paths.Paths{Home: root, ConfigDir: filepath.Join(root, "config")}
	loaded, err := Load(p, sub, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	projectAgent, ok := loaded.ProjectAgents["explore"]
	if !ok {
		t.Fatalf("ProjectAgents[explore] missing, got %v", loaded.ProjectAgents)
	}
	if projectAgent.Description != "sub desc" {
		t.Errorf("ProjectAgents[explore].Description = %q, want %q (closer file wins)", projectAgent.Description, "sub desc")
	}
	if projectAgent.Model != "openai/gpt-5" {
		t.Errorf("ProjectAgents[explore].Model = %q, want %q (unset in sub, must survive from root)", projectAgent.Model, "openai/gpt-5")
	}
	if projectAgent.MaxSteps != 12 {
		t.Errorf("ProjectAgents[explore].MaxSteps = %d, want 12", projectAgent.MaxSteps)
	}
	if projectAgent.Source != subFile {
		t.Errorf("ProjectAgents[explore].Source = %q, want %q (last project file to touch it)", projectAgent.Source, subFile)
	}

	cfgAgent, ok := loaded.Config.Agents["explore"]
	if !ok {
		t.Fatalf("Config.Agents[explore] missing, got %v", loaded.Config.Agents)
	}
	if !reflect.DeepEqual(cfgAgent, projectAgent) {
		t.Errorf("Config.Agents[explore] = %+v, want it to match ProjectAgents[explore] = %+v (no global file here)", cfgAgent, projectAgent)
	}
}

func TestLoad_PermissionPatternsMerge(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[permissions.bash]
"git status*" = "allow"
"*" = "ask"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[permissions.bash]
"rm *" = "deny"
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rule := loaded.Config.Permissions["bash"]
	want := map[string]core.Action{
		"git status*": core.Allow,
		"*":           core.Ask,
		"rm *":        core.Deny,
	}
	if !reflect.DeepEqual(rule.Patterns, want) {
		t.Errorf("Patterns = %v, want %v", rule.Patterns, want)
	}
}

func TestLoad_InstructionsAppendDedup(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
instructions = ["local.md", "/abs/shared.md"]
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
instructions = ["/abs/shared.md", "extra.md"]
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{
		filepath.Join(configDir, "local.md"),
		"/abs/shared.md",
		filepath.Join(workDir, ".jig", "extra.md"),
	}
	if !reflect.DeepEqual(loaded.Config.Instructions, want) {
		t.Errorf("Instructions = %v, want %v", loaded.Config.Instructions, want)
	}
}

func TestLoad_NestedProjectDirsCloserWins(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	writeConfigFile(t, filepath.Join(repo, ".jig", "config.toml"), `default_model = "root/model"`)
	writeConfigFile(t, filepath.Join(sub, ".jig", "config.toml"), `default_model = "sub/model"`)

	p := paths.Paths{Home: root, ConfigDir: filepath.Join(root, "config")}
	loaded, err := Load(p, sub, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Config.DefaultModel != "sub/model" {
		t.Errorf("DefaultModel = %q, want %q", loaded.Config.DefaultModel, "sub/model")
	}
	wantFiles := []string{
		filepath.Join(repo, ".jig", "config.toml"),
		filepath.Join(sub, ".jig", "config.toml"),
	}
	if !reflect.DeepEqual(loaded.Files, wantFiles) {
		t.Errorf("Files = %v, want %v", loaded.Files, wantFiles)
	}
}

func TestLoad_AgentsSplitByScope(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[agents.explore]
description = "explore desc"
`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[agents.reviewer]
description = "reviewer desc"
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := loaded.GlobalAgents["explore"]; !ok {
		t.Errorf("GlobalAgents missing explore: %v", loaded.GlobalAgents)
	}
	if _, ok := loaded.GlobalAgents["reviewer"]; ok {
		t.Errorf("GlobalAgents unexpectedly has reviewer: %v", loaded.GlobalAgents)
	}
	if _, ok := loaded.ProjectAgents["reviewer"]; !ok {
		t.Errorf("ProjectAgents missing reviewer: %v", loaded.ProjectAgents)
	}
	if _, ok := loaded.ProjectAgents["explore"]; ok {
		t.Errorf("ProjectAgents unexpectedly has explore: %v", loaded.ProjectAgents)
	}
	if len(loaded.Config.Agents) != 2 {
		t.Errorf("Config.Agents = %v, want 2 entries", loaded.Config.Agents)
	}
}

func TestLoad_SyntaxErrorNamesFile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")
	badFile := filepath.Join(configDir, "config.toml")

	writeConfigFile(t, badFile, "default_model = \n")

	p := paths.Paths{Home: root, ConfigDir: configDir}
	_, err := Load(p, workDir, fakeGetenv(nil))
	if err == nil {
		t.Fatal("Load: want error for invalid TOML syntax, got nil")
	}
	if !strings.Contains(err.Error(), badFile) {
		t.Errorf("error = %q, want it to name the file %q", err.Error(), badFile)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("error = %q, want it to name a line", err.Error())
	}
}
