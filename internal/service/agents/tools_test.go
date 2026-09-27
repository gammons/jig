package agents

import (
	"context"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

type fakeTool struct {
	name string
}

func (f fakeTool) Name() string           { return f.name }
func (f fakeTool) Description() string    { return "fake tool " + f.name }
func (f fakeTool) Schema() map[string]any { return nil }
func (f fakeTool) Concurrent() bool       { return false }
func (f fakeTool) Run(_ context.Context, _ ext.RunContext, _ core.ToolCall) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}

func allFakeTools() []ext.Tool {
	names := []string{"read", "write", "edit", "bash", "task", "glob", "grep"}
	out := make([]ext.Tool, 0, len(names))
	for _, n := range names {
		out = append(out, fakeTool{name: n})
	}
	return out
}

func toolNames(tools []ext.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestToolsFor_AllowlistSpawnAndDeny(t *testing.T) {
	all := allFakeTools()

	t.Run("nil Tools means all tools, task dropped without CanSpawn", func(t *testing.T) {
		a := core.Agent{Name: "general", CanSpawn: false}
		got := toolNames(ToolsFor(a, all))
		if containsName(got, "task") {
			t.Errorf("ToolsFor() = %v, want no task tool (CanSpawn is false)", got)
		}
		for _, n := range []string{"read", "write", "edit", "bash", "glob", "grep"} {
			if !containsName(got, n) {
				t.Errorf("ToolsFor() = %v, missing %q", got, n)
			}
		}
	})

	t.Run("task kept when CanSpawn is true", func(t *testing.T) {
		a := core.Agent{Name: "build", CanSpawn: true}
		got := toolNames(ToolsFor(a, all))
		if !containsName(got, "task") {
			t.Errorf("ToolsFor() = %v, want task tool present (CanSpawn is true)", got)
		}
	})

	t.Run("explicit Tools allowlist restricts the set", func(t *testing.T) {
		a := core.Agent{Name: "explore", Tools: []string{"read", "glob", "grep"}}
		got := toolNames(ToolsFor(a, all))
		want := []string{"read", "glob", "grep"}
		if len(got) != len(want) {
			t.Fatalf("ToolsFor() = %v, want %v", got, want)
		}
		for _, n := range want {
			if !containsName(got, n) {
				t.Errorf("ToolsFor() = %v, missing %q", got, n)
			}
		}
	})

	t.Run("plain Deny with no patterns hides the tool", func(t *testing.T) {
		a := core.Agent{
			Name: "restricted",
			Permissions: core.PermissionRules{
				"bash": {Default: core.Deny},
			},
		}
		got := toolNames(ToolsFor(a, all))
		if containsName(got, "bash") {
			t.Errorf("ToolsFor() = %v, want bash hidden (plain Deny)", got)
		}
	})

	t.Run("Deny with patterns does not hide the tool", func(t *testing.T) {
		a := core.Agent{
			Name: "restricted-patterns",
			Permissions: core.PermissionRules{
				"bash": {
					Default:  core.Deny,
					Patterns: map[string]core.Action{"git status*": core.Allow},
				},
			},
		}
		got := toolNames(ToolsFor(a, all))
		if !containsName(got, "bash") {
			t.Errorf("ToolsFor() = %v, want bash visible (Deny has patterns)", got)
		}
	})
}
