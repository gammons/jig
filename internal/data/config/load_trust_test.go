package config

import (
	"path/filepath"
	"testing"

	"github.com/gammons/jig/internal/data/paths"
)

func TestLoad_ProjectSubstitutionGated(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")
	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `theme = "{env:SECRET}"`)
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
default_model = "{env:SECRET}"

[agents.build]
prompt = "{file:missing.txt}"
`)
	p := paths.Paths{Home: root, ConfigDir: configDir}
	getenv := fakeGetenv(map[string]string{"SECRET": "s3cr3t"})

	off, err := Load(p, workDir, getenv, Options{})
	if err != nil {
		t.Fatalf("Load(off): %v", err)
	}
	if off.Global.Theme != "s3cr3t" {
		t.Errorf("off: Global.Theme = %q, want the substituted value", off.Global.Theme)
	}
	if off.Project.DefaultModel != "{env:SECRET}" {
		t.Errorf("off: Project.DefaultModel = %q, want the literal token", off.Project.DefaultModel)
	}
	if got := off.Project.Agents["build"].Prompt; got != "{file:missing.txt}" {
		t.Errorf("off: build prompt = %q, want the literal token", got)
	}

	writeConfigFile(t, filepath.Join(workDir, ".jig", "missing.txt"), "included\n")
	on, err := Load(p, workDir, getenv, Options{SubstituteProject: true})
	if err != nil {
		t.Fatalf("Load(on): %v", err)
	}
	if on.Global.Theme != "s3cr3t" || on.Project.DefaultModel != "s3cr3t" {
		t.Errorf("on: Theme %q, DefaultModel %q, want both substituted", on.Global.Theme, on.Project.DefaultModel)
	}
	if got := on.Project.Agents["build"].Prompt; got != "included" {
		t.Errorf("on: build prompt = %q, want the included file", got)
	}
}
