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
	if !reflect.DeepEqual(Merge(loaded.Global, loaded.Project), core.Config{}) {
		t.Errorf("Merge(Global, Project) = %+v, want zero value", Merge(loaded.Global, loaded.Project))
	}
	if len(loaded.GlobalFiles) != 0 {
		t.Errorf("GlobalFiles = %v, want empty", loaded.GlobalFiles)
	}
	if len(loaded.ProjectFiles) != 0 {
		t.Errorf("ProjectFiles = %v, want empty", loaded.ProjectFiles)
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
	merged := Merge(loaded.Global, loaded.Project)
	if merged.DefaultModel != "project/model" {
		t.Errorf("DefaultModel = %q, want %q", merged.DefaultModel, "project/model")
	}
	if merged.Theme != "dark" {
		t.Errorf("Theme = %q, want %q (only global sets it)", merged.Theme, "dark")
	}
	wantGlobalFiles := []string{filepath.Join(configDir, "config.toml")}
	if !reflect.DeepEqual(loaded.GlobalFiles, wantGlobalFiles) {
		t.Errorf("GlobalFiles = %v, want %v", loaded.GlobalFiles, wantGlobalFiles)
	}
	wantProjectFiles := []string{filepath.Join(workDir, ".jig", "config.toml")}
	if !reflect.DeepEqual(loaded.ProjectFiles, wantProjectFiles) {
		t.Errorf("ProjectFiles = %v, want %v", loaded.ProjectFiles, wantProjectFiles)
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
	merged := Merge(loaded.Global, loaded.Project)
	agent, ok := merged.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", merged.Agents)
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
	merged := Merge(loaded.Global, loaded.Project)
	prov, ok := merged.Providers["openai"]
	if !ok {
		t.Fatalf("Providers[openai] missing, got %v", merged.Providers)
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
	merged := Merge(loaded.Global, loaded.Project)
	agent, ok := merged.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", merged.Agents)
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
	merged := Merge(loaded.Global, loaded.Project)
	agent, ok := merged.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", merged.Agents)
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
	merged := Merge(loaded.Global, loaded.Project)
	wantAliases := map[string]string{
		"fast":  "anthropic/haiku", // project overrides the same key
		"smart": "anthropic/opus",  // project-only key survives
	}
	if !reflect.DeepEqual(merged.ModelAliases, wantAliases) {
		t.Errorf("ModelAliases = %v, want %v", merged.ModelAliases, wantAliases)
	}
	wantKeybinds := map[string]string{"quit": "ctrl+c"} // untouched by project file
	if !reflect.DeepEqual(merged.Keybinds, wantKeybinds) {
		t.Errorf("Keybinds = %v, want %v", merged.Keybinds, wantKeybinds)
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

	projectAgent, ok := loaded.Project.Agents["explore"]
	if !ok {
		t.Fatalf("Project.Agents[explore] missing, got %v", loaded.Project.Agents)
	}
	if projectAgent.Description != "sub desc" {
		t.Errorf("Project.Agents[explore].Description = %q, want %q (closer file wins)", projectAgent.Description, "sub desc")
	}
	if projectAgent.Model != "openai/gpt-5" {
		t.Errorf("Project.Agents[explore].Model = %q, want %q (unset in sub, must survive from root)", projectAgent.Model, "openai/gpt-5")
	}
	if projectAgent.MaxSteps != 12 {
		t.Errorf("Project.Agents[explore].MaxSteps = %d, want 12", projectAgent.MaxSteps)
	}
	if projectAgent.Source != subFile {
		t.Errorf("Project.Agents[explore].Source = %q, want %q (last project file to touch it)", projectAgent.Source, subFile)
	}

	merged := Merge(loaded.Global, loaded.Project)
	cfgAgent, ok := merged.Agents["explore"]
	if !ok {
		t.Fatalf("Merged Agents[explore] missing, got %v", merged.Agents)
	}
	if !reflect.DeepEqual(cfgAgent, projectAgent) {
		t.Errorf("Merged Agents[explore] = %+v, want it to match Project.Agents[explore] = %+v (no global file here)", cfgAgent, projectAgent)
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
	merged := Merge(loaded.Global, loaded.Project)
	rule := merged.Permissions["bash"]
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
	merged := Merge(loaded.Global, loaded.Project)
	want := []string{
		filepath.Join(configDir, "local.md"),
		"/abs/shared.md",
		filepath.Join(workDir, ".jig", "extra.md"),
	}
	if !reflect.DeepEqual(merged.Instructions, want) {
		t.Errorf("Instructions = %v, want %v", merged.Instructions, want)
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
	merged := Merge(loaded.Global, loaded.Project)
	if merged.DefaultModel != "sub/model" {
		t.Errorf("DefaultModel = %q, want %q", merged.DefaultModel, "sub/model")
	}
	wantFiles := []string{
		filepath.Join(repo, ".jig", "config.toml"),
		filepath.Join(sub, ".jig", "config.toml"),
	}
	if !reflect.DeepEqual(loaded.ProjectFiles, wantFiles) {
		t.Errorf("ProjectFiles = %v, want %v", loaded.ProjectFiles, wantFiles)
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

	if _, ok := loaded.Global.Agents["explore"]; !ok {
		t.Errorf("Global.Agents missing explore: %v", loaded.Global.Agents)
	}
	if _, ok := loaded.Global.Agents["reviewer"]; ok {
		t.Errorf("Global.Agents unexpectedly has reviewer: %v", loaded.Global.Agents)
	}
	if _, ok := loaded.Project.Agents["reviewer"]; !ok {
		t.Errorf("Project.Agents missing reviewer: %v", loaded.Project.Agents)
	}
	if _, ok := loaded.Project.Agents["explore"]; ok {
		t.Errorf("Project.Agents unexpectedly has explore: %v", loaded.Project.Agents)
	}
	merged := Merge(loaded.Global, loaded.Project)
	if len(merged.Agents) != 2 {
		t.Errorf("Merged Agents = %v, want 2 entries", merged.Agents)
	}
}

func TestLoad_LayersSeparated(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")
	projectFile := filepath.Join(workDir, ".jig", "config.toml")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `permissions.bash = "ask"`)
	writeConfigFile(t, projectFile, `permissions.bash = "allow"`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded.Global.Permissions["bash"].Default; got != core.Ask {
		t.Errorf("Global.Permissions[bash].Default = %q, want %q", got, core.Ask)
	}
	if got := loaded.Project.Permissions["bash"].Default; got != core.Allow {
		t.Errorf("Project.Permissions[bash].Default = %q, want %q", got, core.Allow)
	}
	wantProjectFiles := []string{projectFile}
	if !reflect.DeepEqual(loaded.ProjectFiles, wantProjectFiles) {
		t.Errorf("ProjectFiles = %v, want %v", loaded.ProjectFiles, wantProjectFiles)
	}
}

func TestLoad_ImageModelsAndIntegrations(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[integrations.agent_browser]
enabled = "auto"

[providers.custom]
image_models = ["custom-vision"]
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Global.AgentBrowser != core.ToggleAuto {
		t.Errorf("Global.AgentBrowser = %q, want %q", loaded.Global.AgentBrowser, core.ToggleAuto)
	}
	prov, ok := loaded.Global.Providers["custom"]
	if !ok {
		t.Fatalf("Global.Providers[custom] missing, got %v", loaded.Global.Providers)
	}
	if !reflect.DeepEqual(prov.ImageModels, []string{"custom-vision"}) {
		t.Errorf("ImageModels = %v, want %v", prov.ImageModels, []string{"custom-vision"})
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
