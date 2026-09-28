package agentbrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeScript writes a shell script at dir/name and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetect(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		bin, ok := Detect(func(name string) (string, error) {
			if name != "agent-browser" {
				t.Errorf("lookPath called with %q, want agent-browser", name)
			}
			return "/usr/local/bin/agent-browser", nil
		})
		if !ok || bin != "/usr/local/bin/agent-browser" {
			t.Errorf("Detect() = (%q, %v), want (/usr/local/bin/agent-browser, true)", bin, ok)
		}
	})

	t.Run("not found", func(t *testing.T) {
		bin, ok := Detect(func(string) (string, error) { return "", errors.New("not found") })
		if ok || bin != "" {
			t.Errorf("Detect() = (%q, %v), want (\"\", false)", bin, ok)
		}
	})
}

func TestSkillsPath_Timeout(t *testing.T) {
	dir := t.TempDir()
	bin := writeScript(t, dir, "agent-browser", "sleep 10\n")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := SkillsPath(ctx, bin)
	if err == nil {
		t.Fatal("SkillsPath: want an error from the short deadline, got nil")
	}
}

func TestSkillsPath_TrimsOutput(t *testing.T) {
	dir := t.TempDir()
	bin := writeScript(t, dir, "agent-browser", "printf '  /skills/dir \\n'\n")

	got, err := SkillsPath(context.Background(), bin)
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	if got != "/skills/dir" {
		t.Errorf("SkillsPath() = %q, want %q", got, "/skills/dir")
	}
}
