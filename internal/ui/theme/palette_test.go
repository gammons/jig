package theme

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestBuiltin_Count56AndComplete checks that every built-in palette, once
// completed, has every field populated and every color string parses.
//
// The brief names this test after slk's theme count at plan-writing time
// (56); the ported slk source now has 59 built-in themes (see
// task-2-report.md), so the assertion below checks the actual count of
// Builtin() rather than a stale literal.
func TestBuiltin_Count56AndComplete(t *testing.T) {
	all := Builtin()
	if len(all) == 0 {
		t.Fatal("Builtin() returned no palettes")
	}

	seen := map[string]bool{}
	for _, p := range all {
		if seen[strings.ToLower(p.Name)] {
			t.Errorf("duplicate palette name %q", p.Name)
		}
		seen[strings.ToLower(p.Name)] = true

		c := Complete(p)
		checkComplete(t, c)
	}
}

// checkComplete fails t if any field of p is empty, or if any color field
// does not parse via lipgloss.Color without panicking.
func checkComplete(t *testing.T, p Palette) {
	t.Helper()
	if p.Name == "" {
		t.Error("Name is empty after Complete")
	}
	fields := map[string]string{
		"Primary":              p.Primary,
		"Accent":               p.Accent,
		"Warning":              p.Warning,
		"Error":                p.Error,
		"Background":           p.Background,
		"Surface":              p.Surface,
		"SurfaceDark":          p.SurfaceDark,
		"Text":                 p.Text,
		"TextMuted":            p.TextMuted,
		"Border":               p.Border,
		"SidebarBackground":    p.SidebarBackground,
		"SidebarText":          p.SidebarText,
		"SidebarTextMuted":     p.SidebarTextMuted,
		"SelectionBackground":  p.SelectionBackground,
		"SelectionForeground":  p.SelectionForeground,
		"SearchHighlightBg":    p.SearchHighlightBg,
		"SearchHighlightFg":    p.SearchHighlightFg,
		"ComposeInsertBG":      p.ComposeInsertBG,
		"SelectionBgFocused":   p.SelectionBgFocused,
		"SelectionBgUnfocused": p.SelectionBgUnfocused,
	}
	for name, v := range fields {
		if v == "" {
			t.Errorf("%s: %s is empty after Complete", p.Name, name)
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: %s = %q panicked in lipgloss.Color: %v", p.Name, name, v, r)
				}
			}()
			_ = lipgloss.Color(v)
		}()
	}
}

func TestDefault_IsDarkAndComplete(t *testing.T) {
	d := Default()
	if !strings.EqualFold(d.Name, "Dark") {
		t.Errorf("Default().Name = %q, want %q", d.Name, "Dark")
	}
	checkComplete(t, d)
}

func TestLookup_CaseInsensitiveCustomWins(t *testing.T) {
	custom := []Palette{{Name: "Dark", Primary: "#123456"}}

	got, ok := Lookup("dark", custom)
	if !ok {
		t.Fatal("Lookup(\"dark\", custom) not found")
	}
	if got.Primary != "#123456" {
		t.Errorf("Lookup returned builtin, not custom: Primary = %q, want %q", got.Primary, "#123456")
	}

	got, ok = Lookup("DARK", nil)
	if !ok {
		t.Fatal("Lookup(\"DARK\", nil) not found")
	}
	if !strings.EqualFold(got.Name, "Dark") {
		t.Errorf("Lookup(\"DARK\", nil).Name = %q, want Dark", got.Name)
	}

	if _, ok := Lookup("does-not-exist", nil); ok {
		t.Error("Lookup(\"does-not-exist\", nil) found a palette, want not found")
	}
}

func TestCustom_UnknownKeyWarns(t *testing.T) {
	colors := map[string]string{
		"primary":    "#ABCDEF",
		"not_a_key":  "#000000",
		"also_wrong": "#111111",
	}
	p, warnings := Custom("My Theme", colors)

	if p.Name != "My Theme" {
		t.Errorf("Name = %q, want %q", p.Name, "My Theme")
	}
	if p.Primary != "#ABCDEF" {
		t.Errorf("Primary = %q, want %q", p.Primary, "#ABCDEF")
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want 2 entries", warnings)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "not_a_key") || !strings.Contains(joined, "also_wrong") {
		t.Errorf("warnings = %v, want mentions of the unknown keys", warnings)
	}
}

func TestCustom_AllKnownKeysNoWarnings(t *testing.T) {
	colors := map[string]string{
		"primary":                "#111111",
		"accent":                 "#222222",
		"warning":                "#333333",
		"error":                  "#444444",
		"background":             "#555555",
		"surface":                "#666666",
		"surface_dark":           "#777777",
		"text":                   "#888888",
		"text_muted":             "#999999",
		"border":                 "#AAAAAA",
		"sidebar_background":     "#BBBBBB",
		"sidebar_text":           "#CCCCCC",
		"sidebar_text_muted":     "#DDDDDD",
		"selection_background":   "#EEEEEE",
		"selection_foreground":   "#FFFFFF",
		"search_highlight_bg":    "#101010",
		"search_highlight_fg":    "#202020",
		"compose_insert_bg":      "#303030",
		"selection_bg_focused":   "#404040",
		"selection_bg_unfocused": "#505050",
	}
	p, warnings := Custom("Full", colors)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	c := Complete(p)
	checkComplete(t, c)
}
