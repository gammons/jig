package agentfs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/paths"
)

func writeAgent(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestAgentDiscover_ClaudeStyleTools(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "reviewer", "---\ndescription: reviews code\ntools: Read, Grep, Glob\n---\nBody\n")

	agents, warnings := Discover([]string{dir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	agent, ok := agents["reviewer"]
	if !ok {
		t.Fatalf("agents = %v, want reviewer", agents)
	}
	want := []string{"read", "grep", "glob"}
	if !reflect.DeepEqual(agent.Tools, want) {
		t.Errorf("Tools = %v, want %v", agent.Tools, want)
	}
}

func TestAgentDiscover_BodyIsPrompt(t *testing.T) {
	dir := t.TempDir()
	path := writeAgent(t, dir, "explorer", "---\ndescription: explores\n---\nYou are an explorer.\nBe curious.\n")

	agents, warnings := Discover([]string{dir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	agent, ok := agents["explorer"]
	if !ok {
		t.Fatalf("agents = %v, want explorer", agents)
	}
	if agent.Prompt != "You are an explorer.\nBe curious.\n" {
		t.Errorf("Prompt = %q, want body text", agent.Prompt)
	}
	if agent.Description != "explores" {
		t.Errorf("Description = %q, want %q", agent.Description, "explores")
	}
	if agent.Source != path {
		t.Errorf("Source = %q, want %q", agent.Source, path)
	}
}

func TestAgentDiscover_NameIsFileStemIgnoringFrontmatterName(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "actual-stem", "---\nname: some-other-name\ndescription: x\n---\nBody\n")

	agents, _ := Discover([]string{dir})
	if _, ok := agents["actual-stem"]; !ok {
		t.Errorf("agents = %v, want key actual-stem (frontmatter name field is ignored)", agents)
	}
	if _, ok := agents["some-other-name"]; ok {
		t.Errorf("agents = %v, should not use frontmatter name as key", agents)
	}
}

func TestAgentDiscover_FullFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "full", `---
description: does everything
mode: subagent
model: anthropic/opus
max_steps: 7
can_spawn: true
hidden: false
tools: [read, write, Bash]
permissions:
  bash: deny
  edit:
    "*.go": allow
    "*": ask
---
Prompt body.
`)

	agents, warnings := Discover([]string{dir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	agent, ok := agents["full"]
	if !ok {
		t.Fatalf("agents = %v, want full", agents)
	}
	if agent.Mode != "subagent" {
		t.Errorf("Mode = %q, want %q", agent.Mode, "subagent")
	}
	if agent.Model != "anthropic/opus" {
		t.Errorf("Model = %q, want %q", agent.Model, "anthropic/opus")
	}
	if agent.MaxSteps != 7 {
		t.Errorf("MaxSteps = %d, want 7", agent.MaxSteps)
	}
	if agent.CanSpawn == nil || *agent.CanSpawn != true {
		t.Errorf("CanSpawn = %v, want pointer to true", agent.CanSpawn)
	}
	if agent.Hidden == nil || *agent.Hidden != false {
		t.Errorf("Hidden = %v, want pointer to false", agent.Hidden)
	}
	wantTools := []string{"read", "write", "bash"}
	if !reflect.DeepEqual(agent.Tools, wantTools) {
		t.Errorf("Tools = %v, want %v", agent.Tools, wantTools)
	}
	if agent.Permissions["bash"].Default != core.Deny {
		t.Errorf("Permissions[bash].Default = %v, want deny", agent.Permissions["bash"].Default)
	}
	wantEditPatterns := map[string]core.Action{"*.go": core.Allow, "*": core.Ask}
	if !reflect.DeepEqual(agent.Permissions["edit"].Patterns, wantEditPatterns) {
		t.Errorf("Permissions[edit].Patterns = %v, want %v", agent.Permissions["edit"].Patterns, wantEditPatterns)
	}
}

func TestAgentDiscover_CanSpawnHiddenNilWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "plain", "---\ndescription: plain\n---\nBody\n")

	agents, _ := Discover([]string{dir})
	agent := agents["plain"]
	if agent.CanSpawn != nil {
		t.Errorf("CanSpawn = %v, want nil", agent.CanSpawn)
	}
	if agent.Hidden != nil {
		t.Errorf("Hidden = %v, want nil", agent.Hidden)
	}
}

func TestAgentDiscover_BadYAMLWarnsAndSkips(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "badperm", "---\ndescription: bad\npermissions:\n  bash: not-a-real-action\n---\nBody\n")

	agents, warnings := Discover([]string{dir})
	if _, ok := agents["badperm"]; ok {
		t.Errorf("agents = %v, badperm should be skipped", agents)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}
	if warnings[0].Path != filepath.Join(dir, "badperm.md") {
		t.Errorf("warning Path = %q, want %q", warnings[0].Path, filepath.Join(dir, "badperm.md"))
	}
}

func TestAgentDiscover_BadYAMLSyntaxWarnsAndSkips(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "brokenyaml", "---\ndescription: [unterminated\n---\nBody\n")

	agents, warnings := Discover([]string{dir})
	if _, ok := agents["brokenyaml"]; ok {
		t.Errorf("agents = %v, brokenyaml should be skipped", agents)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}
}

func TestAgentDiscover_NonMarkdownFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeAgent(t, dir, "real", "---\ndescription: real\n---\nBody\n")

	agents, warnings := Discover([]string{dir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(agents) != 1 {
		t.Errorf("agents = %v, want just real", agents)
	}
}

func TestAgentDirs_Order(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	gitRoot := filepath.Join(root, "repo")
	workDir := filepath.Join(gitRoot, "sub")

	global, project := Dirs(p, gitRoot, workDir)

	wantGlobal := []string{filepath.Join(p.ConfigDir, "agents")}
	if !reflect.DeepEqual(global, wantGlobal) {
		t.Errorf("global = %v, want %v", global, wantGlobal)
	}

	wantProject := []string{
		filepath.Join(gitRoot, ".jig", "agents"),
		filepath.Join(gitRoot, ".claude", "agents"),
		filepath.Join(workDir, ".jig", "agents"),
		filepath.Join(workDir, ".claude", "agents"),
	}
	if !reflect.DeepEqual(project, wantProject) {
		t.Errorf("project = %v, want %v", project, wantProject)
	}
}

func TestAgentDirs_NoGitRootUsesWorkDirOnly(t *testing.T) {
	home := t.TempDir()
	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	workDir := filepath.Join(home, "work")

	_, project := Dirs(p, "", workDir)

	want := []string{
		filepath.Join(workDir, ".jig", "agents"),
		filepath.Join(workDir, ".claude", "agents"),
	}
	if !reflect.DeepEqual(project, want) {
		t.Errorf("project = %v, want %v", project, want)
	}
}

func TestAgentDiscover_LaterDirWins(t *testing.T) {
	low := t.TempDir()
	high := t.TempDir()
	writeAgent(t, low, "shared", "---\ndescription: low\n---\nBody\n")
	writeAgent(t, high, "shared", "---\ndescription: high\n---\nBody\n")

	agents, _ := Discover([]string{low, high})
	if agents["shared"].Description != "high" {
		t.Errorf("Description = %q, want %q (later dir wins)", agents["shared"].Description, "high")
	}
}

func TestAgentDiscover_SymlinkedFileDiscovered(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	realPath := writeAgent(t, realDir, "real-agent", "---\ndescription: via symlink\n---\nBody text\n")

	linksDir := filepath.Join(root, "links")
	if err := os.MkdirAll(linksDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", linksDir, err)
	}
	linkPath := filepath.Join(linksDir, "linked-agent.md")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	agents, warnings := Discover([]string{linksDir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	agent, ok := agents["linked-agent"]
	if !ok {
		t.Fatalf("agents = %v, want linked-agent (symlinked .md file must be discovered)", agents)
	}
	if agent.Description != "via symlink" {
		t.Errorf("Description = %q, want %q", agent.Description, "via symlink")
	}
	if agent.Prompt != "Body text\n" {
		t.Errorf("Prompt = %q, want %q", agent.Prompt, "Body text\n")
	}
}

func TestAgentDiscover_MissingDirsSkipped(t *testing.T) {
	agents, warnings := Discover([]string{"/no/such/agents/dir"})
	if len(agents) != 0 {
		t.Errorf("agents = %v, want none", agents)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}
