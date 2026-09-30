package actions

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core/ext"
)

func TestResolve_DefaultsAndOverride(t *testing.T) {
	c := NewCatalogue(nil)
	config := map[string]string{
		"normal.ctrl+b": "view.sidebar",
		"insert.ctrl+x": "prompt.editor",
	}
	km, warnings := Resolve(DefaultBindings(), config, c)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	tests := []struct {
		mode, key string
		want      ID
	}{
		{"insert", "ctrl+p", PickerOpen},
		{"insert", "ctrl+t", PickerOpen},
		{"normal", "ctrl+p", PickerOpen},
		{"normal", "ctrl+t", PickerOpen},
		{"insert", "ctrl+b", ViewSidebar},
		{"normal", "ctrl+b", ViewSidebar},
		{"insert", "ctrl+e", PromptEditor},
		{"insert", "ctrl+x", PromptEditor}, // added by config
		{"normal", "enter", TranscriptDetails},
		{"normal", "/", TranscriptSearch},
		{"normal", "y", TranscriptYank},
		{"normal", "o", TranscriptFold},
		{"normal", "?", HelpKeys},
		{"normal", "ctrl+c", RunCancel},
	}
	for _, tt := range tests {
		got, ok := km.Lookup(tt.mode, tt.key)
		if !ok || got != tt.want {
			t.Errorf("Lookup(%q, %q) = %q, %v, want %q, true", tt.mode, tt.key, got, ok, tt.want)
		}
	}

	if _, ok := km.Lookup("normal", "ctrl+z"); ok {
		t.Error("Lookup(normal, ctrl+z): want not found")
	}

	wantKeys := []string{"ctrl+b"}
	if got := km.Keys("normal", ViewSidebar); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("Keys(normal, view.sidebar) = %v, want %v", got, wantKeys)
	}
	wantInsertKeys := []string{"ctrl+e", "ctrl+x"}
	if got := km.Keys("insert", PromptEditor); !reflect.DeepEqual(got, wantInsertKeys) {
		t.Errorf("Keys(insert, prompt.editor) = %v, want %v", got, wantInsertKeys)
	}
}

func TestResolve_Warnings(t *testing.T) {
	c := NewCatalogue(nil)
	config := map[string]string{
		"bogus.ctrl+z":  "view.sidebar",
		"normal.ctrl+q": "not.an.action",
	}
	km, warnings := Resolve(nil, config, c)

	want := []string{
		`warning: keybinds."bogus.ctrl+z": unknown mode "bogus"`,
		`warning: keybinds."normal.ctrl+q": unknown action "not.an.action"`,
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings = %v, want %v", warnings, want)
	}

	if _, ok := km.Lookup("bogus", "ctrl+z"); ok {
		t.Error("Lookup(bogus, ctrl+z): want not applied")
	}
	if _, ok := km.Lookup("normal", "ctrl+q"); ok {
		t.Error("Lookup(normal, ctrl+q): want not applied")
	}
}

func TestResolve_FixedKeysWarnAndSkip(t *testing.T) {
	c := NewCatalogue(nil)
	config := map[string]string{
		"insert.enter":  "app.quit",
		"insert.ctrl+c": "view.sidebar",
		"normal.j":      "app.quit",
		"normal.gp":     "app.quit",
		"normal.a":      "view.sidebar",
		"normal.ctrl+z": "app.quit",
		"insert.ctrl+e": "view.sidebar", // remappable in INSERT
		"normal.x":      "view.sidebar",
		"normal.h":      "app.quit",
		"normal.l":      "app.quit",
	}
	km, warnings := Resolve(DefaultBindings(), config, c)
	want := []string{
		`warning: keybinds."insert.ctrl+c": ctrl+c is fixed in insert mode and cannot be remapped`,
		`warning: keybinds."insert.enter": enter is fixed in insert mode and cannot be remapped`,
		`warning: keybinds."normal.a": a is fixed in normal mode and cannot be remapped`,
		`warning: keybinds."normal.ctrl+z": ctrl+z is fixed in normal mode and cannot be remapped`,
		`warning: keybinds."normal.gp": gp is fixed in normal mode and cannot be remapped`,
		`warning: keybinds."normal.h": h is fixed in normal mode and cannot be remapped`,
		`warning: keybinds."normal.j": j is fixed in normal mode and cannot be remapped`,
		`warning: keybinds."normal.l": l is fixed in normal mode and cannot be remapped`,
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings =\n%v\nwant\n%v", strings.Join(warnings, "\n"), strings.Join(want, "\n"))
	}
	for _, k := range [][2]string{{"insert", "enter"}, {"insert", "ctrl+c"}, {"normal", "j"}, {"normal", "gp"}, {"normal", "a"}, {"normal", "h"}, {"normal", "l"}} {
		if id, ok := km.Lookup(k[0], k[1]); ok {
			t.Errorf("Lookup(%s, %s) = %q, want the config entry skipped", k[0], k[1], id)
		}
	}
	if id, _ := km.Lookup("insert", "ctrl+e"); id != ViewSidebar {
		t.Errorf("insert.ctrl+e = %q, want remapped to view.sidebar", id)
	}
	if id, _ := km.Lookup("normal", "x"); id != ViewSidebar {
		t.Errorf("normal.x = %q, want view.sidebar", id)
	}
	if id, _ := km.Lookup("normal", "ctrl+c"); id != RunCancel {
		t.Errorf("default normal.ctrl+c = %q, want run.cancel kept", id)
	}
}

func TestResolve_RejectsUnprintableKey(t *testing.T) {
	c := NewCatalogue(nil)
	config := map[string]string{
		"normal.\x1b]52;c;x\x07": "view.sidebar",
	}
	km, warnings := Resolve(nil, config, c)

	want := []string{
		`warning: keybinds."normal.\x1b]52;c;x\a": key contains a non-printable character`,
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings = %v, want %v", warnings, want)
	}
	if _, ok := km.Lookup("normal", "\x1b]52;c;x\x07"); ok {
		t.Error("Lookup: want the unprintable key not applied")
	}
}

func TestResolve_BindsLaterWins(t *testing.T) {
	binds := []ext.Keybind{
		{Mode: "normal", Key: "g", Command: "goto-top"},
		{Mode: "normal", Key: "g", Command: "goto-bottom"},
	}
	km, warnings := Resolve(binds, nil, NewCatalogue(nil))
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if got, ok := km.Lookup("normal", "g"); !ok || got != ID("goto-bottom") {
		t.Errorf("Lookup(normal, g) = %q, %v, want goto-bottom, true", got, ok)
	}
}
