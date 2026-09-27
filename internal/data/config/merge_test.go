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
	s.apply(dto1, md1, "file1", "/dir1", "/home/u", true)

	dto2, md2 := decodeTOMLForTest(t, `
[permissions.bash]
"rm *" = "deny"
`)
	s.apply(dto2, md2, "file2", "/dir2", "/home/u", false)

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
	s.apply(dto1, md1, "file1", "/dir1", "/home/u", true)

	dto2, md2 := decodeTOMLForTest(t, `
[permissions.bash]
"safe *" = "allow"
`)
	s.apply(dto2, md2, "file2", "/dir2", "/home/u", false)

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
	s.apply(dto1, md1, "global.toml", "/global", "/home/u", true)

	dto2, md2 := decodeTOMLForTest(t, `
[agents.explore]
max_steps = 12
`)
	s.apply(dto2, md2, "project.toml", "/project", "/home/u", false)

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
	s.apply(dto1, md1, "file1", "/dir1", "/home/u", true)

	dto2, md2 := decodeTOMLForTest(t, `default_model = "x/y"`)
	s.apply(dto2, md2, "file2", "/dir2", "/home/u", false)

	if s.cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want %q (unset in file2 must not clear it)", s.cfg.Theme, "dark")
	}
	if s.cfg.DefaultModel != "x/y" {
		t.Errorf("DefaultModel = %q, want %q", s.cfg.DefaultModel, "x/y")
	}
}
