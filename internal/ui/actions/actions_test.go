package actions

import (
	"context"
	"testing"

	"github.com/gammons/jig/internal/core/ext"
)

// fakeCommand is a minimal ext.Command stub for tests.
type fakeCommand struct{ name, desc string }

func (f fakeCommand) Name() string                        { return f.name }
func (f fakeCommand) Description() string                 { return f.desc }
func (f fakeCommand) Run(context.Context, []string) error { return nil }

func TestCatalogue_BuiltinsAndExt(t *testing.T) {
	cmds := []ext.Command{
		fakeCommand{name: "greet", desc: "Say hello"},
	}
	c := NewCatalogue(cmds)

	all := c.All()
	if len(all) != 22 {
		t.Fatalf("All() = %d actions, want 22 (21 builtins + 1 ext)", len(all))
	}

	first := all[0]
	if first.ID != SessionNew || first.Title != "New session" || first.Group != "Session" || first.Drill {
		t.Errorf("All()[0] = %+v, want session.new builtin", first)
	}

	last := all[len(all)-1]
	if last.ID != "ext.greet" || last.Title != "Say hello" || last.Group != "Extensions" || last.Command == nil {
		t.Errorf("All()[last] = %+v, want ext.greet", last)
	}

	tests := []struct {
		id    ID
		title string
		group string
		drill bool
	}{
		{SessionNew, "New session", "Session", false},
		{SessionOpen, "Open session…", "Session", true},
		{SessionRename, "Rename session…", "Session", true},
		{SessionCompact, "Compact session", "Session", false},
		{AgentSwitch, "Switch agent…", "Agent & model", true},
		{ModelSwitch, "Switch model…", "Agent & model", true},
		{EffortSwitch, "Switch effort…", "Agent & model", true},
		{PromptAttach, "Attach files…", "Prompt", true},
		{PromptEditor, "Edit prompt in $EDITOR", "Prompt", false},
		{TranscriptSearch, "Search transcript", "Transcript", false},
		{TranscriptYank, "Yank block", "Transcript", false},
		{TranscriptDetails, "Toggle details", "Transcript", false},
		{TranscriptFold, "Toggle group", "Transcript", false},
		{RunCancel, "Cancel run", "Transcript", false},
		{ViewSidebar, "Toggle sidebar", "View", false},
		{ViewTheme, "Switch theme…", "View", true},
		{ViewReasoning, "Streamed reasoning…", "View", true},
		{MCPServers, "MCP servers…", "MCP", true},
		{HelpKeys, "Keybindings", "App", true},
		{AppQuit, "Quit", "App", false},
		{PickerOpen, "Open picker", "App", false},
	}
	for _, tt := range tests {
		got, ok := c.Get(tt.id)
		if !ok {
			t.Errorf("Get(%q): not found", tt.id)
			continue
		}
		if got.Title != tt.title || got.Group != tt.group || got.Drill != tt.drill || got.Command != nil {
			t.Errorf("Get(%q) = %+v, want {Title: %q, Group: %q, Drill: %v, Command: nil}", tt.id, got, tt.title, tt.group, tt.drill)
		}
	}

	got, ok := c.Get("ext.greet")
	if !ok {
		t.Fatal("Get(ext.greet): not found")
	}
	if got.Command != cmds[0] {
		t.Errorf("Get(ext.greet).Command = %v, want the registered fakeCommand", got.Command)
	}

	if _, ok := c.Get("no.such.action"); ok {
		t.Error("Get(no.such.action): want not found")
	}
}

func TestCatalogue_NoExtCommands(t *testing.T) {
	c := NewCatalogue(nil)
	if len(c.All()) != 21 {
		t.Fatalf("All() = %d actions, want 21 builtins", len(c.All()))
	}
	if _, ok := c.Get(PickerOpen); !ok {
		t.Error("Get(picker.open): not found")
	}
}
