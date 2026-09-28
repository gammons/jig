package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/prefsfs"
	"github.com/gammons/jig/internal/service/trust"
)

func failingLookPath(t *testing.T) func(string) (string, error) {
	return func(string) (string, error) {
		t.Helper()
		t.Fatal("lookPath called, want no detection attempt")
		return "", nil
	}
}

func failingSkillsPath(t *testing.T) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) {
		t.Helper()
		t.Fatal("skillsPath called, want a cache hit")
		return "", nil
	}
}

func TestBrowser_ToggleFalse_NoExec(t *testing.T) {
	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleFalse}}
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")

	_, bi, warns := resolveBrowser(l, false, failingLookPath(t), prefsPath, failingSkillsPath(t))
	if bi.enabled {
		t.Errorf("browserIntegration = %+v, want disabled", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestBrowser_MissingBinaryWithTrue_Warns(t *testing.T) {
	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleTrue}}
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")
	lookPath := func(string) (string, error) { return "", errors.New("not found") }

	_, bi, warns := resolveBrowser(l, false, lookPath, prefsPath, failingSkillsPath(t))
	if bi.enabled {
		t.Errorf("browserIntegration = %+v, want disabled", bi)
	}
	if len(warns) != 1 || warns[0] != missingBrowserWarning {
		t.Errorf("warns = %v, want [%q]", warns, missingBrowserWarning)
	}
}

func TestBrowser_AutoWithoutBinary_NoWarning(t *testing.T) {
	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleAuto}}
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")
	lookPath := func(string) (string, error) { return "", errors.New("not found") }

	_, bi, warns := resolveBrowser(l, false, lookPath, prefsPath, failingSkillsPath(t))
	if bi.enabled {
		t.Errorf("browserIntegration = %+v, want disabled", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

// writeFakeBin writes an executable placeholder at dir/agent-browser.
func writeFakeBin(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "agent-browser")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestBrowser_CachedPrefsSkipsSkillsPath(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir)
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}

	prefsPath := filepath.Join(dir, "prefs.json")
	store, err := prefsfs.Open(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(core.Prefs{AgentBrowser: core.AgentBrowserCache{
		Bin: bin, ModTime: info.ModTime().UnixNano(), SkillsDir: "/cached/skills",
	}}); err != nil {
		t.Fatal(err)
	}

	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleAuto}}
	lookPath := func(string) (string, error) { return bin, nil }

	_, bi, warns := resolveBrowser(l, false, lookPath, prefsPath, failingSkillsPath(t))
	if !bi.enabled || bi.bin != bin || bi.skillsDir != "/cached/skills" {
		t.Errorf("browserIntegration = %+v, want enabled with cached skills dir", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestBrowser_CacheMissRunsSkillsPathAndSaves(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir)
	prefsPath := filepath.Join(dir, "prefs.json")
	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleAuto}}
	lookPath := func(string) (string, error) { return bin, nil }

	calls := 0
	skillsPath := func(_ context.Context, gotBin string) (string, error) {
		calls++
		if gotBin != bin {
			t.Errorf("skillsPath called with %q, want %q", gotBin, bin)
		}
		return "/fresh/skills", nil
	}

	_, bi, warns := resolveBrowser(l, false, lookPath, prefsPath, skillsPath)
	if calls != 1 {
		t.Errorf("skillsPath called %d times, want 1", calls)
	}
	if !bi.enabled || bi.skillsDir != "/fresh/skills" {
		t.Errorf("browserIntegration = %+v, want enabled with /fresh/skills", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}

	store, err := prefsfs.Open(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := store.Get().AgentBrowser
	if got.Bin != bin || got.SkillsDir != "/fresh/skills" {
		t.Errorf("saved prefs = %+v, want Bin=%q SkillsDir=/fresh/skills", got, bin)
	}
}

func TestBrowser_SkillsPathErrorWarnsButStaysEnabled(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir)
	prefsPath := filepath.Join(dir, "prefs.json")
	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleAuto}}
	lookPath := func(string) (string, error) { return bin, nil }
	skillsPath := func(context.Context, string) (string, error) { return "", errors.New("boom") }

	_, bi, warns := resolveBrowser(l, false, lookPath, prefsPath, skillsPath)
	if !bi.enabled || bi.skillsDir != "" {
		t.Errorf("browserIntegration = %+v, want enabled with no skills dir", bi)
	}
	if len(warns) != 1 || warns[0] != "warning: agent-browser skills path: boom" {
		t.Errorf("warns = %v, want the skills path error wrapped", warns)
	}
}

func TestBrowser_PresetMergedIntoGlobal(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir)
	prefsPath := filepath.Join(dir, "prefs.json")
	l := trust.Layers{Global: core.Config{AgentBrowser: core.ToggleAuto}}
	lookPath := func(string) (string, error) { return bin, nil }
	skillsPath := func(context.Context, string) (string, error) { return "", errors.New("no skills") }

	out, _, _ := resolveBrowser(l, false, lookPath, prefsPath, skillsPath)
	if _, ok := out.Global.Permissions["bash"].Patterns["agent-browser snapshot*"]; !ok {
		t.Errorf("Global.Permissions = %+v, want the agent-browser preset merged in", out.Global.Permissions)
	}
}

func TestBrowser_TrustedProjectTogglesOverridesGlobal(t *testing.T) {
	l := trust.Layers{
		Global:  core.Config{AgentBrowser: core.ToggleTrue},
		Project: core.Config{AgentBrowser: core.ToggleFalse},
	}
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")

	_, bi, warns := resolveBrowser(l, true, failingLookPath(t), prefsPath, failingSkillsPath(t))
	if bi.enabled {
		t.Errorf("browserIntegration = %+v, want disabled (Project overrides trusted)", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestBrowser_UntrustedProjectIgnored(t *testing.T) {
	l := trust.Layers{
		Global:  core.Config{AgentBrowser: core.ToggleFalse},
		Project: core.Config{AgentBrowser: core.ToggleTrue},
	}
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")

	_, bi, warns := resolveBrowser(l, false, failingLookPath(t), prefsPath, failingSkillsPath(t))
	if bi.enabled {
		t.Errorf("browserIntegration = %+v, want disabled (untrusted Project ignored)", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}
