package skills

import (
	"context"
	"io/fs"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// fakeFS implements FS for tests that don't need real file I/O.
type fakeFS struct{}

func (fakeFS) ReadBody(string) (string, error) { return "", nil }
func (fakeFS) WalkDir(_ string, _ fs.WalkDirFunc) error {
	return nil
}

func TestSkillsTransform_OmittedWithoutSkillTool(t *testing.T) {
	svc := New([]core.Skill{
		{Name: "a", Description: "First skill.", Dir: "/skills/a", Path: "/skills/a/SKILL.md"},
	}, fakeFS{})

	req := &core.LLMRequest{} // no "skill" tool
	tr := svc.Transform()
	if got := tr.Priority(); got != 40 {
		t.Errorf("Priority() = %d, want 40", got)
	}
	if err := tr.Transform(context.Background(), ext.RunContext{}, req); err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System = %v, want empty", req.System)
	}
}

func TestSkillsTransform_OmittedWhenNoSkills(t *testing.T) {
	svc := New(nil, fakeFS{})

	req := &core.LLMRequest{Tools: []core.ToolSpec{{Name: "skill"}}}
	if err := svc.Transform().Transform(context.Background(), ext.RunContext{}, req); err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if len(req.System) != 0 {
		t.Errorf("req.System = %v, want empty", req.System)
	}
}

func TestSkillsTransform_Lists(t *testing.T) {
	svc := New([]core.Skill{
		{Name: "b", Description: "Second skill.", Dir: "/skills/b", Path: "/skills/b/SKILL.md"},
		{Name: "a", Description: "First skill.", Dir: "/skills/a", Path: "/skills/a/SKILL.md"},
	}, fakeFS{})

	req := &core.LLMRequest{Tools: []core.ToolSpec{{Name: "skill"}}}
	if err := svc.Transform().Transform(context.Background(), ext.RunContext{}, req); err != nil {
		t.Fatalf("Transform: %v", err)
	}

	want := "Skills provide specialized instructions and workflows for specific tasks.\n" +
		"Use the skill tool to load a skill when a task matches its description.\n" +
		"<available_skills>\n" +
		"  <skill>\n    <id>a</id>\n    <description>First skill.</description>\n  </skill>\n" +
		"  <skill>\n    <id>b</id>\n    <description>Second skill.</description>\n  </skill>\n" +
		"</available_skills>"

	if len(req.System) != 1 || req.System[0] != want {
		t.Errorf("req.System =\n%q\nwant\n[%q]", req.System, want)
	}
}
