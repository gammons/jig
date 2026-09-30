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
	loaded, err := Load(p, filepath.Join(root, "work"), fakeGetenv(map[string]string{"ANTHROPIC_API_KEY": "sk-test"}), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	merged := Merge(loaded.Global, loaded.Project)
	if merged.DefaultModel != "anthropic/claude-sonnet-4-6" {
		t.Errorf("DefaultModel = %q, want %q", merged.DefaultModel, "anthropic/claude-sonnet-4-6")
	}
	if merged.SmallModel != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("SmallModel = %q, want %q", merged.SmallModel, "anthropic/claude-haiku-4-5-20251001")
	}
	prov, ok := merged.Providers["anthropic"]
	if !ok {
		t.Fatalf("Providers[anthropic] missing, got %v", merged.Providers)
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
		if got := merged.ModelAliases[alias]; got != want {
			t.Errorf("ModelAliases[%q] = %q, want %q", alias, got, want)
		}
	}
	explore, ok := merged.Agents["explore"]
	if !ok {
		t.Fatalf("Agents[explore] missing, got %v", merged.Agents)
	}
	if explore.Model != "haiku" {
		t.Errorf("Agents[explore].Model = %q, want %q", explore.Model, "haiku")
	}
	if explore.Permissions["bash"].Default != core.Deny {
		t.Errorf("Agents[explore].Permissions[bash].Default = %q, want %q", explore.Permissions["bash"].Default, core.Deny)
	}
	if rule := merged.Permissions["bash"]; rule.Patterns["rm *"] != core.Deny {
		t.Errorf("Permissions[bash].Patterns[rm *] = %q, want %q", rule.Patterns["rm *"], core.Deny)
	}
	if len(merged.SkillPaths) != 2 {
		t.Errorf("SkillPaths = %v, want 2 entries", merged.SkillPaths)
	}
	if len(merged.Instructions) != 1 {
		t.Errorf("Instructions = %v, want 1 entry", merged.Instructions)
	}
	if merged.Keybinds["normal.x"] != "transcript.yank" {
		t.Errorf("Keybinds[normal.x] = %q, want %q", merged.Keybinds["normal.x"], "transcript.yank")
	}
}
