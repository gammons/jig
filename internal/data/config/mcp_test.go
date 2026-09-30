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

func TestLoad_MCPTOMLStdioAndHTTP(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[mcp.servers.playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
env = { DEBUG = "0" }

[mcp.servers.github]
url = "https://api.githubcopilot.com/mcp/"
headers = { Authorization = "Bearer tok" }
`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	playwright := loaded.Global.MCP.Servers["playwright"]
	if playwright.Transport != core.MCPStdio {
		t.Errorf("playwright.Transport = %q, want stdio", playwright.Transport)
	}
	if playwright.Command != "npx" {
		t.Errorf("playwright.Command = %q, want npx", playwright.Command)
	}
	if !reflect.DeepEqual(playwright.Args, []string{"-y", "@playwright/mcp@latest"}) {
		t.Errorf("playwright.Args = %v", playwright.Args)
	}
	if playwright.StartupTimeout != 0 || playwright.ToolTimeout != 0 {
		t.Errorf("playwright timeouts = %v/%v, want zero (Manager applies defaults)", playwright.StartupTimeout, playwright.ToolTimeout)
	}

	github := loaded.Global.MCP.Servers["github"]
	if github.Transport != core.MCPHTTP {
		t.Errorf("github.Transport = %q, want http", github.Transport)
	}
	if github.URL != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("github.URL = %q", github.URL)
	}
}

func TestLoad_MCPInvalidName(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	configFile := filepath.Join(configDir, "config.toml")
	writeConfigFile(t, configFile, `
[mcp.servers.GitHub]
command = "npx"
`)
	p := paths.Paths{Home: root, ConfigDir: configDir}
	_, err := Load(p, filepath.Join(root, "work"), fakeGetenv(nil), Options{})
	if err == nil {
		t.Fatal("Load: want error for invalid server name, got nil")
	}
	if !strings.Contains(err.Error(), configFile) {
		t.Errorf("error = %q, want it to name %q", err.Error(), configFile)
	}
}

func TestLoad_MCPCommandAndURL(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	configFile := filepath.Join(configDir, "config.toml")
	writeConfigFile(t, configFile, `
[mcp.servers.dual]
command = "npx"
url = "https://example.com/mcp"
`)
	p := paths.Paths{Home: root, ConfigDir: configDir}
	_, err := Load(p, filepath.Join(root, "work"), fakeGetenv(nil), Options{})
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
	if !strings.Contains(err.Error(), configFile) {
		t.Errorf("error = %q, want it to name %q", err.Error(), configFile)
	}
}

func TestLoad_MCPNeither(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	configFile := filepath.Join(configDir, "config.toml")
	writeConfigFile(t, configFile, `
[mcp.servers.neither]
args = ["x"]
`)
	p := paths.Paths{Home: root, ConfigDir: configDir}
	_, err := Load(p, filepath.Join(root, "work"), fakeGetenv(nil), Options{})
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
	if !strings.Contains(err.Error(), configFile) {
		t.Errorf("error = %q, want it to name %q", err.Error(), configFile)
	}
}

func TestLoad_MCPBadDuration(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	configFile := filepath.Join(configDir, "config.toml")
	writeConfigFile(t, configFile, `
[mcp.servers.bad]
command = "npx"
startup_timeout = "not-a-duration"
`)
	p := paths.Paths{Home: root, ConfigDir: configDir}
	_, err := Load(p, filepath.Join(root, "work"), fakeGetenv(nil), Options{})
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
	if !strings.Contains(err.Error(), configFile) {
		t.Errorf("error = %q, want it to name %q", err.Error(), configFile)
	}
}

func TestLoad_MCPJSONClaudeFormat(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	src, err := os.ReadFile(filepath.Join("testdata", "mcp", "claude.mcp.json"))
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}
	writeConfigFile(t, filepath.Join(workDir, mcpJSONFileName), string(src))

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	playwright := loaded.Project.MCP.Servers["playwright"]
	if playwright.Transport != core.MCPStdio {
		t.Errorf("playwright.Transport = %q, want stdio", playwright.Transport)
	}
	if playwright.Command != "npx" {
		t.Errorf("playwright.Command = %q, want npx", playwright.Command)
	}
	github := loaded.Project.MCP.Servers["github"]
	if github.Transport != core.MCPHTTP {
		t.Errorf("github.Transport = %q, want http", github.Transport)
	}
	if len(loaded.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly 1", loaded.Warnings)
	}
	if !strings.Contains(loaded.Warnings[0], `"disabled"`) {
		t.Errorf("Warnings[0] = %q, want it to mention the unknown field", loaded.Warnings[0])
	}
}

func TestLoad_MCPJSONExpansion(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")
	writeConfigFile(t, filepath.Join(workDir, mcpJSONFileName), `{
  "mcpServers": {
    "x": {
      "command": "${HOME_X}",
      "args": ["${MISSING:-d}", "${MISSING}"]
    }
  }
}`)
	p := paths.Paths{Home: root, ConfigDir: configDir}
	getenv := fakeGetenv(map[string]string{"HOME_X": "/home/x"})

	off, err := Load(p, workDir, getenv, Options{})
	if err != nil {
		t.Fatalf("Load(off): %v", err)
	}
	if got := off.Project.MCP.Servers["x"].Command; got != "${HOME_X}" {
		t.Errorf("off: Command = %q, want raw token", got)
	}

	on, err := Load(p, workDir, getenv, Options{SubstituteProject: true})
	if err != nil {
		t.Fatalf("Load(on): %v", err)
	}
	x := on.Project.MCP.Servers["x"]
	if x.Command != "/home/x" {
		t.Errorf("on: Command = %q, want %q", x.Command, "/home/x")
	}
	if len(x.Args) != 2 || x.Args[0] != "d" || x.Args[1] != "" {
		t.Errorf("on: Args = %v, want [d, \"\"]", x.Args)
	}
	if len(on.Warnings) != 1 || !strings.Contains(on.Warnings[0], "${MISSING}") {
		t.Errorf("on: Warnings = %v, want one for ${MISSING}", on.Warnings)
	}
}

func TestLoad_MCPLayerOrder(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	writeConfigFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")

	writeConfigFile(t, filepath.Join(repo, mcpJSONFileName), `{
  "mcpServers": {"a": {"command": "root-cmd"}}
}`)
	writeConfigFile(t, filepath.Join(repo, ".jig", "config.toml"), `
[mcp.servers.a]
command = "toml-cmd"
`)

	// leaf: same dir here for simplicity, but must be a project sub-dir.
	leaf := filepath.Join(repo, "sub")
	writeConfigFile(t, filepath.Join(leaf, mcpJSONFileName), `{
  "mcpServers": {"a": {"enabled": false}}
}`)

	p := paths.Paths{Home: root, ConfigDir: filepath.Join(root, "config")}
	loaded, err := Load(p, leaf, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := loaded.Project.MCP.Servers["a"]
	if a.Command != "toml-cmd" {
		t.Errorf("a.Command = %q, want %q", a.Command, "toml-cmd")
	}
	if a.Enabled == nil || *a.Enabled {
		t.Errorf("a.Enabled = %v, want pointer to false", a.Enabled)
	}
	if a.Transport != core.MCPStdio {
		t.Errorf("a.Transport = %q, want stdio (the full entry must survive)", a.Transport)
	}
}

func TestLoad_MCPDefaultCwd(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	workDir := filepath.Join(root, "work")

	writeConfigFile(t, filepath.Join(configDir, "config.toml"), `
[mcp.servers.global]
command = "npx"
`)
	writeConfigFile(t, filepath.Join(workDir, mcpJSONFileName), `{
  "mcpServers": {"proj": {"command": "npx"}}
}`)

	p := paths.Paths{Home: root, ConfigDir: configDir}
	loaded, err := Load(p, workDir, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded.Global.MCP.Servers["global"].Cwd; got != "" {
		t.Errorf("global.Cwd = %q, want empty (Manager fills the workdir)", got)
	}
	if got := loaded.Project.MCP.Servers["proj"].Cwd; got != workDir {
		t.Errorf("proj.Cwd = %q, want %q", got, workDir)
	}
}

func TestMerge_MCPDisabledUnion(t *testing.T) {
	lo := core.Config{MCP: core.MCPConfig{Disabled: []string{"a", "b"}}}
	hi := core.Config{MCP: core.MCPConfig{Disabled: []string{"b", "c"}}}
	got := Merge(lo, hi)
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got.MCP.Disabled, want) {
		t.Errorf("MCP.Disabled = %v, want %v", got.MCP.Disabled, want)
	}
}

func TestLoad_ProjectMCPFiles(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	writeConfigFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	sub := filepath.Join(repo, "sub")
	writeConfigFile(t, filepath.Join(sub, ".jig", "config.toml"), "") // ensure dir exists

	p := paths.Paths{Home: root, ConfigDir: filepath.Join(root, "config")}
	loaded, err := Load(p, sub, fakeGetenv(nil), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{
		filepath.Join(repo, mcpJSONFileName),
		filepath.Join(sub, mcpJSONFileName),
	}
	if !reflect.DeepEqual(loaded.ProjectMCPFiles, want) {
		t.Errorf("ProjectMCPFiles = %v, want %v", loaded.ProjectMCPFiles, want)
	}
}

func TestResolvedMCP(t *testing.T) {
	falseVal := false
	cfg := core.MCPConfig{
		Servers: map[string]core.MCPServer{
			"toggle":   {Name: "toggle", Enabled: &falseVal}, // Transport == "", toggle-only
			"disabled": {Name: "disabled", Transport: core.MCPStdio, Enabled: &falseVal},
			"listed":   {Name: "listed", Transport: core.MCPStdio},
			"zebra":    {Name: "zebra", Transport: core.MCPHTTP},
			"apple":    {Name: "apple", Transport: core.MCPHTTP},
		},
		Disabled: []string{"listed"},
	}
	got := ResolvedMCP(cfg)
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"apple", "zebra"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("ResolvedMCP names = %v, want %v", names, want)
	}
}
