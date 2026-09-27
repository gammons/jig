package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func fakeGetenv(m map[string]string) func(string) string {
	return func(k string) string {
		return m[k]
	}
}

func TestSubst_Env(t *testing.T) {
	getenv := fakeGetenv(map[string]string{"FOO": "bar"})

	got, err := substituteString("x={env:FOO} y={env:MISSING}", "/cfgdir", "/home/u", getenv)
	if err != nil {
		t.Fatalf("substituteString: %v", err)
	}
	if want := "x=bar y="; got != want {
		t.Errorf("substituteString = %q, want %q", got, want)
	}
}

func TestSubst_FileRelativeToConfig(t *testing.T) {
	getenv := fakeGetenv(nil)

	got, err := substituteString("before {file:include.txt} after", "testdata/config", "/home/u", getenv)
	if err != nil {
		t.Fatalf("substituteString: %v", err)
	}
	if want := "before included content after"; got != want {
		t.Errorf("substituteString = %q, want %q", got, want)
	}
}

func TestSubst_FileHomeExpanded(t *testing.T) {
	getenv := fakeGetenv(nil)

	// testdata/config is used as $HOME here (as an absolute path, like a
	// real p.Home would be), so "~/include.txt" resolves to
	// testdata/config/include.txt regardless of dir.
	home, err := filepath.Abs("testdata/config")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	got, err := substituteString("{file:~/include.txt}", "/unrelated/dir", home, getenv)
	if err != nil {
		t.Fatalf("substituteString: %v", err)
	}
	if want := "included content"; got != want {
		t.Errorf("substituteString = %q, want %q", got, want)
	}
}

func TestSubst_MissingFileErrorNamesPaths(t *testing.T) {
	getenv := fakeGetenv(nil)

	_, err := substituteString("{file:does-not-exist.txt}", "testdata/config", "/home/u", getenv)
	if err == nil {
		t.Fatal("substituteString: want error for missing file, got nil")
	}
	const rawPath = "does-not-exist.txt"
	const resolved = "testdata/config/does-not-exist.txt"
	if !strings.Contains(err.Error(), rawPath) {
		t.Errorf("substituteString error = %q, want it to contain declared path %q", err.Error(), rawPath)
	}
	if !strings.Contains(err.Error(), resolved) {
		t.Errorf("substituteString error = %q, want it to contain resolved path %q", err.Error(), resolved)
	}
}

func TestSubst_RecursesThroughMapsAndSlices(t *testing.T) {
	getenv := fakeGetenv(map[string]string{"X": "42"})

	in := map[string]any{
		"a": "{env:X}",
		"b": []any{"{env:X}", int64(7)},
		"c": map[string]any{"nested": "{env:X}"},
	}
	out, err := substitute(in, "/cfgdir", "/home/u", getenv)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("substitute returned %T, want map[string]any", out)
	}
	if m["a"] != "42" {
		t.Errorf("a = %v, want 42", m["a"])
	}
	sl, ok := m["b"].([]any)
	if !ok || sl[0] != "42" || sl[1] != int64(7) {
		t.Errorf("b = %v, want [42 7]", m["b"])
	}
	nested, ok := m["c"].(map[string]any)
	if !ok || nested["nested"] != "42" {
		t.Errorf("c = %v, want map[nested:42]", m["c"])
	}
}
