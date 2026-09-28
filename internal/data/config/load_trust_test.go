package config

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
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

func TestLoad_ProjectFileRefs(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")
	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `theme = "{file:global-only.txt}"`)
	writeConfigFile(t, filepath.Join(configDir, "global-only.txt"), "g")
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
theme = "a {file:mode} b {file:~/home.txt}"
instructions = ["{file:mode}"]

[permissions]
bash = "{file:/abs/x.txt}"

[agents.build]
prompt = "{file:../prompt.md}"
`)
	p := paths.Paths{Home: filepath.Join(root, "home"), ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Sorted, deduplicated, resolved as substitution would; the global
	// file's refs are not included.
	want := []string{
		"/abs/x.txt",
		filepath.Join(root, "home", "home.txt"),
		filepath.Join(workDir, ".jig", "mode"),
		filepath.Join(workDir, "prompt.md"),
	}
	if !reflect.DeepEqual(loaded.ProjectFileRefs, want) {
		t.Errorf("ProjectFileRefs = %v, want %v", loaded.ProjectFileRefs, want)
	}
}

// A token in a permission action can't be validated unsubstituted, so the
// untrusted pass treats that entry as unset rather than failing the load.
func TestLoad_UnsubstitutedPermissionTokenIsUnset(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	writeConfigFile(t, filepath.Join(workDir, ".jig", "mode"), "allow\n")
	writeConfigFile(t, filepath.Join(workDir, ".jig", "config.toml"), `
[permissions]
bash = "{file:mode}"
read = "deny"
write = { "*.go" = "{env:MODE}", "*.md" = "deny" }

[agents.build.permissions]
edit = "{file:mode}"
`)
	p := paths.Paths{Home: root, ConfigDir: filepath.Join(root, "config")}
	getenv := fakeGetenv(map[string]string{"MODE": "allow"})

	off, err := Load(p, workDir, getenv, Options{})
	if err != nil {
		t.Fatalf("Load(off): %v", err)
	}
	perms := off.Project.Permissions
	if _, ok := perms["bash"]; ok {
		t.Errorf("off: bash = %+v, want unset", perms["bash"])
	}
	if perms["read"].Default != core.Deny {
		t.Errorf("off: read = %+v, want deny kept", perms["read"])
	}
	if want := map[string]core.Action{"*.md": core.Deny}; !reflect.DeepEqual(perms["write"].Patterns, want) {
		t.Errorf("off: write patterns = %v, want %v", perms["write"].Patterns, want)
	}
	if _, ok := off.Project.Agents["build"].Permissions["edit"]; ok {
		t.Errorf("off: build edit = %+v, want unset", off.Project.Agents["build"].Permissions["edit"])
	}
	if len(off.ProjectFileRefs) != 1 {
		t.Errorf("off: ProjectFileRefs = %v, want the mode file", off.ProjectFileRefs)
	}

	on, err := Load(p, workDir, getenv, Options{SubstituteProject: true})
	if err != nil {
		t.Fatalf("Load(on): %v", err)
	}
	if on.Project.Permissions["bash"].Default != core.Allow || on.Project.Permissions["write"].Patterns["*.go"] != core.Allow {
		t.Errorf("on: permissions = %+v, want substituted allows", on.Project.Permissions)
	}
}
