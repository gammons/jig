package themefs

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestThemefs_LoadsTopLevelTOML(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "named.toml", "name = \"My Theme\"\n\n[colors]\nprimary = \"#123456\"\naccent = \"#654321\"\n")
	writeFile(t, dir, "unnamed.toml", "[colors]\nbackground = \"#000000\"\n")
	// A subdirectory's .toml file must be ignored (top level only).
	writeFile(t, dir, filepath.Join("nested", "ignored.toml"), "name = \"Should Not Load\"\n")
	// A non-.toml file is ignored.
	writeFile(t, dir, "notes.txt", "not a theme")
	// A malformed file becomes a warning, not a hard error.
	writeFile(t, dir, "bad.toml", "this is not [ valid toml")

	themes, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1 (for bad.toml)", warnings)
	}

	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	if len(themes) != 2 {
		t.Fatalf("themes = %+v, want 2 entries", themes)
	}

	named := themes[0]
	if named.Name != "My Theme" {
		t.Errorf("Name = %q, want %q", named.Name, "My Theme")
	}
	if named.Colors["primary"] != "#123456" || named.Colors["accent"] != "#654321" {
		t.Errorf("Colors = %v, want primary/accent set", named.Colors)
	}

	unnamed := themes[1]
	if unnamed.Name != "unnamed" {
		t.Errorf("Name = %q, want file stem %q", unnamed.Name, "unnamed")
	}
	if unnamed.Colors["background"] != "#000000" {
		t.Errorf("Colors = %v, want background set", unnamed.Colors)
	}
}

func TestThemefs_MissingDirReturnsNil(t *testing.T) {
	themes, warnings, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if themes != nil {
		t.Errorf("themes = %v, want nil", themes)
	}
	if warnings != nil {
		t.Errorf("warnings = %v, want nil", warnings)
	}
}
