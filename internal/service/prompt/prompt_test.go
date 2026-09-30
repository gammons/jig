package prompt

import (
	"context"
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/service/skills"
)

// fakeFS implements skills.FS for tests that need a skills.Service but
// never read skill bodies or file trees.
type fakeFS struct{}

func (fakeFS) ReadBody(string) (string, error) { return "", nil }
func (fakeFS) WalkDir(_ string, _ fs.WalkDirFunc) error {
	return nil
}

func TestTransforms_OrderAndText(t *testing.T) {
	clk := clock.NewFake(time.Date(2024, time.January, 15, 9, 0, 0, 0, time.UTC))

	rc := ext.RunContext{
		Agent:   core.Agent{Prompt: "You are a helpful test agent."},
		WorkDir: "/work",
	}

	instrFiles := []File{{Path: "/work/instructions.md", Content: "Follow style."}}
	agentsFiles := []File{{Path: "/work/AGENTS.md", Content: "Repo notes."}}

	skillList := []core.Skill{
		{Name: "b", Description: "Second skill.", Dir: "/skills/b", Path: "/skills/b/SKILL.md"},
		{Name: "a", Description: "First skill.", Dir: "/skills/a", Path: "/skills/a/SKILL.md"},
	}
	svc := skills.New(skillList, fakeFS{})

	r := ext.NewRegistry()
	// Register in shuffled (non-priority) order.
	if err := r.AddTransform(svc.Transform()); err != nil {
		t.Fatalf("AddTransform(skills): %v", err)
	}
	if err := r.AddTransform(AgentsMD(agentsFiles)); err != nil {
		t.Fatalf("AddTransform(AgentsMD): %v", err)
	}
	if err := r.AddTransform(AgentPrompt()); err != nil {
		t.Fatalf("AddTransform(AgentPrompt): %v", err)
	}
	if err := r.AddTransform(Env(clk, "linux", func(string) bool { return true })); err != nil {
		t.Fatalf("AddTransform(Env): %v", err)
	}
	if err := r.AddTransform(Instructions(instrFiles)); err != nil {
		t.Fatalf("AddTransform(Instructions): %v", err)
	}
	if err := r.AddTransform(ToolUse()); err != nil {
		t.Fatalf("AddTransform(ToolUse): %v", err)
	}

	view := r.Freeze()

	req := &core.LLMRequest{
		Tools: []core.ToolSpec{{Name: "skill"}},
	}
	for _, tr := range view.Transforms() {
		if err := tr.Transform(context.Background(), rc, req); err != nil {
			t.Fatalf("Transform: %v", err)
		}
	}

	want := []string{
		"You are a helpful test agent.",
		"When several tool calls don't depend on each other, make them all in the same response " +
			"rather than one per turn — for example, read several files or run several searches at once. " +
			"Only wait for a result when a later call needs it.",
		"<env>\n  Working directory: /work\n  Platform: linux\n  Is git repo: yes\n  Today's date: Mon Jan 15 2024\n</env>",
		"Instructions from: /work/instructions.md\nFollow style.",
		"Instructions from: /work/AGENTS.md\nRepo notes.",
		"Skills provide specialized instructions and workflows for specific tasks.\n" +
			"Use the skill tool to load a skill when a task matches its description.\n" +
			"<available_skills>\n" +
			"  <skill>\n    <id>a</id>\n    <description>First skill.</description>\n  </skill>\n" +
			"  <skill>\n    <id>b</id>\n    <description>Second skill.</description>\n  </skill>\n" +
			"</available_skills>",
	}

	if !reflect.DeepEqual(req.System, want) {
		t.Errorf("req.System =\n%q\nwant\n%q", req.System, want)
	}
}

func TestEnv_NotGit(t *testing.T) {
	clk := clock.NewFake(time.Date(2024, time.January, 15, 9, 0, 0, 0, time.UTC))
	rc := ext.RunContext{WorkDir: "/work"}
	req := &core.LLMRequest{}

	tr := Env(clk, "darwin", func(string) bool { return false })
	if got := tr.Priority(); got != 10 {
		t.Errorf("Priority() = %d, want 10", got)
	}
	if err := tr.Transform(context.Background(), rc, req); err != nil {
		t.Fatalf("Transform: %v", err)
	}

	want := "<env>\n  Working directory: /work\n  Platform: darwin\n  Is git repo: no\n  Today's date: Mon Jan 15 2024\n</env>"
	if len(req.System) != 1 || req.System[0] != want {
		t.Errorf("req.System = %q, want [%q]", req.System, want)
	}
}

func TestAgentPrompt_SkipsEmpty(t *testing.T) {
	rc := ext.RunContext{Agent: core.Agent{Prompt: ""}}
	req := &core.LLMRequest{}

	tr := AgentPrompt()
	if got := tr.Priority(); got != 0 {
		t.Errorf("Priority() = %d, want 0", got)
	}
	if err := tr.Transform(context.Background(), rc, req); err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System = %v, want empty", req.System)
	}
}

func TestToolUse_NoToolsAppendsNothing(t *testing.T) {
	req := &core.LLMRequest{}
	if err := ToolUse().Transform(context.Background(), ext.RunContext{}, req); err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System = %q, want empty", req.System)
	}
}

func TestInstructionsAndAgentsMD_ZeroFilesAppendNothing(t *testing.T) {
	req := &core.LLMRequest{}
	rc := ext.RunContext{}

	if got := Instructions(nil).Priority(); got != 20 {
		t.Errorf("Instructions Priority() = %d, want 20", got)
	}
	if got := AgentsMD(nil).Priority(); got != 30 {
		t.Errorf("AgentsMD Priority() = %d, want 30", got)
	}

	if err := Instructions(nil).Transform(context.Background(), rc, req); err != nil {
		t.Fatalf("Instructions Transform: %v", err)
	}
	if err := AgentsMD(nil).Transform(context.Background(), rc, req); err != nil {
		t.Fatalf("AgentsMD Transform: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System = %v, want empty", req.System)
	}
}
