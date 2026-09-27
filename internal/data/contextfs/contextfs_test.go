package contextfs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/data/paths"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAgentsFiles_ClaudeFallbackPerLevel(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	sub := filepath.Join(repo, "sub")

	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	write(t, filepath.Join(repo, "AGENTS.md"), "root agents\n")
	write(t, filepath.Join(sub, "CLAUDE.md"), "sub claude\n")

	files := AgentsFiles(p, repo, sub)

	want := []File{
		{Path: filepath.Join(repo, "AGENTS.md"), Content: "root agents\n"},
		{Path: filepath.Join(sub, "CLAUDE.md"), Content: "sub claude\n"},
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("AgentsFiles = %+v, want %+v", files, want)
	}
}

func TestAgentsFiles_GlobalConfigDirFirst(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workDir := filepath.Join(root, "work")

	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	write(t, filepath.Join(p.ConfigDir, "AGENTS.md"), "global\n")
	write(t, filepath.Join(workDir, "AGENTS.md"), "local\n")

	files := AgentsFiles(p, "", workDir)

	want := []File{
		{Path: filepath.Join(p.ConfigDir, "AGENTS.md"), Content: "global\n"},
		{Path: filepath.Join(workDir, "AGENTS.md"), Content: "local\n"},
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("AgentsFiles = %+v, want %+v", files, want)
	}
}

func TestAgentsFiles_PrefersAgentsOverClaude(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workDir := filepath.Join(root, "work")

	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}
	write(t, filepath.Join(workDir, "AGENTS.md"), "agents wins\n")
	write(t, filepath.Join(workDir, "CLAUDE.md"), "claude loses\n")

	files := AgentsFiles(p, "", workDir)
	if len(files) != 1 {
		t.Fatalf("files = %+v, want 1", files)
	}
	if files[0].Content != "agents wins\n" {
		t.Errorf("Content = %q, want %q", files[0].Content, "agents wins\n")
	}
}

func TestAgentsFiles_MissingFilesSkipped(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workDir := filepath.Join(root, "work")
	p := paths.Paths{Home: home, ConfigDir: filepath.Join(home, ".config", "jig")}

	files := AgentsFiles(p, "", workDir)
	if len(files) != 0 {
		t.Errorf("files = %+v, want none", files)
	}
}

func TestInstructions_GlobAndHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	baseDir := filepath.Join(root, "project")

	write(t, filepath.Join(home, "shared", "one.md"), "home one\n")
	write(t, filepath.Join(baseDir, "docs", "a.md"), "doc a\n")
	write(t, filepath.Join(baseDir, "docs", "b.md"), "doc b\n")

	patterns := []string{"~/shared/one.md", "docs/*.md"}
	files, err := Instructions(patterns, baseDir, home)
	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("files = %+v, want 3", files)
	}
	if files[0].Content != "home one\n" {
		t.Errorf("files[0].Content = %q, want %q", files[0].Content, "home one\n")
	}
	// docs/*.md matches are sorted within the pattern: a.md before b.md.
	if !strings.HasSuffix(files[1].Path, "a.md") || files[1].Content != "doc a\n" {
		t.Errorf("files[1] = %+v, want a.md", files[1])
	}
	if !strings.HasSuffix(files[2].Path, "b.md") || files[2].Content != "doc b\n" {
		t.Errorf("files[2] = %+v, want b.md", files[2])
	}
}

func TestInstructions_MissingLiteralPathIsError(t *testing.T) {
	baseDir := t.TempDir()

	if _, err := Instructions([]string{"missing.md"}, baseDir, baseDir); err == nil {
		t.Error("Instructions: want error for missing literal path, got nil")
	}

	files, err := Instructions([]string{"nomatch-*.md"}, baseDir, baseDir)
	if err != nil {
		t.Errorf("Instructions: unmatched glob should not error, got %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %+v, want none for unmatched glob", files)
	}
}

func TestInstructions_DeduplicatedByAbsolutePath(t *testing.T) {
	baseDir := t.TempDir()
	write(t, filepath.Join(baseDir, "one.md"), "content\n")

	patterns := []string{"one.md", filepath.Join(baseDir, "one.md")}
	files, err := Instructions(patterns, baseDir, baseDir)
	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("files = %+v, want 1 (de-duplicated by absolute path)", files)
	}
}

func TestInstructions_DedupByAbsolutePathWithRelativeBaseDir(t *testing.T) {
	tmp := t.TempDir()
	write(t, filepath.Join(tmp, "sub", "one.md"), "content\n")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("Chdir back: %v", err)
		}
	}()

	// baseDir is relative ("."), so the two patterns resolve to lexically
	// different strings ("sub/one.md" vs the absolute path) that are only
	// provably the same file once resolved to an absolute path from the
	// current working directory (tmp).
	patterns := []string{"sub/one.md", filepath.Join(tmp, "sub", "one.md")}
	files, err := Instructions(patterns, ".", tmp)
	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want 1 (de-duplicated by absolute path even with a relative baseDir)", files)
	}
	if !filepath.IsAbs(files[0].Path) {
		t.Errorf("Path = %q, want an absolute path", files[0].Path)
	}
	want := filepath.Join(tmp, "sub", "one.md")
	if files[0].Path != want {
		t.Errorf("Path = %q, want %q", files[0].Path, want)
	}
}

func TestInstructions_PatternOrderPreserved(t *testing.T) {
	baseDir := t.TempDir()
	write(t, filepath.Join(baseDir, "z.md"), "z\n")
	write(t, filepath.Join(baseDir, "a.md"), "a\n")

	patterns := []string{"z.md", "a.md"}
	files, err := Instructions(patterns, baseDir, baseDir)
	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if len(files) != 2 || files[0].Content != "z\n" || files[1].Content != "a\n" {
		t.Errorf("files = %+v, want z then a (pattern order, not alpha order)", files)
	}
}
