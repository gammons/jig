package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

func mustSkillCall(t *testing.T, input any) core.ToolCall {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return core.ToolCall{ID: "c1", Name: "skill", Input: raw}
}

// realWalkFS implements FS by delegating WalkDir to filepath.WalkDir and
// serving a fixed body for ReadBody, regardless of path.
type realWalkFS struct {
	body string
}

func (f realWalkFS) ReadBody(string) (string, error) { return f.body, nil }
func (realWalkFS) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillTool_LoadsBodyAndFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: my-skill\n---\nbody")
	writeFile(t, filepath.Join(skillDir, "reference.md"), "ref")
	writeFile(t, filepath.Join(skillDir, "scripts", "run.sh"), "#!/bin/sh")

	sk := core.Skill{
		Name:        "my-skill",
		Description: "A test skill.",
		Dir:         skillDir,
		Path:        filepath.Join(skillDir, "SKILL.md"),
	}
	svc := New([]core.Skill{sk}, realWalkFS{body: "This is the skill body."})

	tool := svc.Tool()
	if tool.Name() != "skill" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "skill")
	}
	if !tool.Concurrent() {
		t.Error("Concurrent() = false, want true")
	}

	call := mustSkillCall(t, map[string]any{"id": "my-skill"})
	res, err := tool.Run(context.Background(), ext.RunContext{}, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	fileA := filepath.Join(skillDir, "reference.md")
	fileB := filepath.Join(skillDir, "scripts", "run.sh")
	sortedFiles := []string{fileA, fileB}
	if sortedFiles[0] > sortedFiles[1] {
		sortedFiles[0], sortedFiles[1] = sortedFiles[1], sortedFiles[0]
	}

	want := fmt.Sprintf(
		"<skill_content name=\"my-skill\">\n# Skill: my-skill\n\nThis is the skill body.\n\n"+
			"Base directory for this skill: %s\nRelative paths in this skill are relative to this base directory.\n\n"+
			"<skill_files>\n<file>%s</file>\n<file>%s</file>\n</skill_files>\n</skill_content>",
		skillDir, sortedFiles[0], sortedFiles[1],
	)
	if res.Output != want {
		t.Errorf("Output =\n%q\nwant\n%q", res.Output, want)
	}
}

func TestSkillTool_FileListCappedAt50(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "big-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "body")
	for i := 0; i < 60; i++ {
		writeFile(t, filepath.Join(skillDir, fmt.Sprintf("file%02d.txt", i)), "x")
	}

	sk := core.Skill{Name: "big-skill", Dir: skillDir, Path: filepath.Join(skillDir, "SKILL.md")}
	svc := New([]core.Skill{sk}, realWalkFS{body: "body"})

	call := mustSkillCall(t, map[string]any{"id": "big-skill"})
	res, err := svc.Tool().Run(context.Background(), ext.RunContext{}, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	if count := strings.Count(res.Output, "<file>"); count != 50 {
		t.Errorf("file count = %d, want 50", count)
	}

	// The 50 files present should be the lexicographically first 50.
	wantFirst := fmt.Sprintf("<file>%s</file>", filepath.Join(skillDir, "file00.txt"))
	wantLast := fmt.Sprintf("<file>%s</file>", filepath.Join(skillDir, "file49.txt"))
	wantAbsent := fmt.Sprintf("<file>%s</file>", filepath.Join(skillDir, "file50.txt"))
	if !strings.Contains(res.Output, wantFirst) {
		t.Errorf("Output missing %q", wantFirst)
	}
	if !strings.Contains(res.Output, wantLast) {
		t.Errorf("Output missing %q", wantLast)
	}
	if strings.Contains(res.Output, wantAbsent) {
		t.Errorf("Output should not contain %q (over the 50 cap)", wantAbsent)
	}
}

func TestSkillTool_Unknown(t *testing.T) {
	svc := New([]core.Skill{
		{Name: "b", Dir: "/skills/b", Path: "/skills/b/SKILL.md"},
		{Name: "a", Dir: "/skills/a", Path: "/skills/a/SKILL.md"},
	}, fakeFS{})

	call := mustSkillCall(t, map[string]any{"id": "x"})
	res, err := svc.Tool().Run(context.Background(), ext.RunContext{}, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	want := `unknown skill "x"; available: a, b`
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}
}
