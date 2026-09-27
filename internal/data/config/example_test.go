package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/paths"
)

// TestLoad_ExampleConfigParses loads docs/config.example.toml, copied into a
// temp global config dir, so the shipped example never silently drifts from
// the TOML schema it documents.
func TestLoad_ExampleConfigParses(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "config.example.toml"))
	if err != nil {
		t.Fatalf("reading docs/config.example.toml: %v", err)
	}

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	writeConfigFile(t, filepath.Join(configDir, "config.toml"), string(src))

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, filepath.Join(root, "work"), fakeGetenv(map[string]string{"ANTHROPIC_API_KEY": "sk-test"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Config.DefaultModel != "anthropic/claude-sonnet-4-6" {
		t.Errorf("DefaultModel = %q, want %q", loaded.Config.DefaultModel, "anthropic/claude-sonnet-4-6")
	}
	if loaded.Config.SmallModel != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("SmallModel = %q, want %q", loaded.Config.SmallModel, "anthropic/claude-haiku-4-5-20251001")
	}
	prov, ok := loaded.Config.Providers["anthropic"]
	if !ok {
		t.Fatalf("Providers[anthropic] missing, got %v", loaded.Config.Providers)
	}
	if prov.APIKey != "sk-test" {
		t.Errorf("Providers[anthropic].APIKey = %q, want the substituted {env:ANTHROPIC_API_KEY} value %q", prov.APIKey, "sk-test")
	}
	wantAliases := map[string]string{
		"haiku":  "anthropic/claude-haiku-4-5-20251001",
		"sonnet": "anthropic/claude-sonnet-4-6",
		"opus":   "anthropic/claude-opus-4-5-20251101",
	}
	for alias, want := range wantAliases {
		if got := loaded.Config.ModelAliases[alias]; got != want {
			t.Errorf("ModelAliases[%q] = %q, want %q", alias, got, want)
		}
	}
	explore, ok := loaded.Config.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", loaded.Config.Agents)
	}
	if explore.Model != "haiku" {
		t.Errorf("Agents[explore].Model = %q, want %q", explore.Model, "haiku")
	}
	if explore.Permissions["bash"].Default != core.Deny {
		t.Errorf("Agents[explore].Permissions[bash].Default = %q, want %q", explore.Permissions["bash"].Default, core.Deny)
	}
	if rule := loaded.Config.Permissions["bash"]; rule.Patterns["rm *"] != core.Deny {
		t.Errorf("Permissions[bash].Patterns[rm *] = %q, want %q", rule.Patterns["rm *"], core.Deny)
	}
	if len(loaded.Config.SkillPaths) != 2 {
		t.Errorf("SkillPaths = %v, want 2 entries", loaded.Config.SkillPaths)
	}
	if len(loaded.Config.Instructions) != 1 {
		t.Errorf("Instructions = %v, want 1 entry", loaded.Config.Instructions)
	}
	if loaded.Config.Keybinds["quit"] != "ctrl+c" {
		t.Errorf("Keybinds[quit] = %q, want %q", loaded.Config.Keybinds["quit"], "ctrl+c")
	}
}
