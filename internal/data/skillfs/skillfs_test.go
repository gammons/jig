package skillfs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/data/paths"
)

func writeSkill(t *testing.T, skillsDir, dirName, content string) {
	t.Helper()
	dir := filepath.Join(skillsDir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestSkillDiscover_LaterDirWins(t *testing.T) {
	root := t.TempDir()
	lowDir := filepath.Join(root, "low")
	highDir := filepath.Join(root, "high")

	writeSkill(t, lowDir, "foo", "---\ndescription: low priority\n---\nLow body\n")
	writeSkill(t, highDir, "foo", "---\ndescription: high priority\n---\nHigh body\n")

	skills, warnings := Discover([]string{lowDir, highDir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %v, want 1", skills)
	}
	if skills[0].Description != "high priority" {
		t.Errorf("Description = %q, want %q (later dir wins)", skills[0].Description, "high priority")
	}
	if skills[0].Dir != filepath.Join(highDir, "foo") {
		t.Errorf("Dir = %q, want %q", skills[0].Dir, filepath.Join(highDir, "foo"))
	}
}

func TestSkillDiscover_MissingDescriptionWarns(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "nodesc", "---\nname: nodesc\n---\nBody\n")

	skills, warnings := Discover([]string{dir})
	if len(skills) != 0 {
		t.Errorf("skills = %v, want none (missing description skips the skill)", skills)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}
	if warnings[0].Path != filepath.Join(dir, "nodesc", "SKILL.md") {
		t.Errorf("warning Path = %q, want the SKILL.md path", warnings[0].Path)
	}
}

func TestSkillDiscover_NameMismatchWarns(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "mydir", "---\nname: other-name\ndescription: does things\n---\nBody\n")

	skills, warnings := Discover([]string{dir})
	if len(skills) != 1 {
		t.Fatalf("skills = %v, want 1", skills)
	}
	if skills[0].Name != "other-name" {
		t.Errorf("Name = %q, want %q (frontmatter name used)", skills[0].Name, "other-name")
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}
}

func TestSkillDirs_Order(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	gitRoot := filepath.Join(root, "repo")
	workDir := filepath.Join(gitRoot, "sub")

	dirs := Dirs(p, gitRoot, workDir, []string{"/extra/skills"})

	want := []string{
		filepath.Join(p.ConfigDir, "skills"),
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".claude", "skills"),
		"/extra/skills",
		filepath.Join(gitRoot, ".jig", "skills"),
		filepath.Join(gitRoot, ".agents", "skills"),
		filepath.Join(gitRoot, ".claude", "skills"),
		filepath.Join(workDir, ".jig", "skills"),
		filepath.Join(workDir, ".agents", "skills"),
		filepath.Join(workDir, ".claude", "skills"),
	}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("Dirs = %v, want %v", dirs, want)
	}
}

func TestSkillDirs_NoGitRootUsesWorkDirOnly(t *testing.T) {
	home := t.TempDir()
	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	workDir := filepath.Join(home, "work")

	dirs := Dirs(p, "", workDir, nil)

	want := []string{
		filepath.Join(p.ConfigDir, "skills"),
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(workDir, ".jig", "skills"),
		filepath.Join(workDir, ".agents", "skills"),
		filepath.Join(workDir, ".claude", "skills"),
	}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("Dirs = %v, want %v", dirs, want)
	}
}

func TestSkillDiscover_MissingDirsSkipped(t *testing.T) {
	skills, warnings := Discover([]string{"/no/such/dir"})
	if len(skills) != 0 {
		t.Errorf("skills = %v, want none", skills)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

func TestSkillDiscover_SortedByName(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "zebra", "---\ndescription: z\n---\n")
	writeSkill(t, dir, "alpha", "---\ndescription: a\n---\n")

	skills, _ := Discover([]string{dir})
	got := []string{}
	for _, s := range skills {
		got = append(got, s.Name)
	}
	want := []string{"alpha", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestSkillDiscover_NonDirEntriesIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a skill"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	writeSkill(t, dir, "real", "---\ndescription: real skill\n---\n")

	skills, warnings := Discover([]string{dir})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(skills) != 1 || skills[0].Name != "real" {
		t.Errorf("skills = %v, want just [real]", skills)
	}
}
