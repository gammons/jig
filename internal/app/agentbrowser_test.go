package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/data/prefsfs"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/permission"
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

	bi, warns := resolveBrowser(l, false, failingLookPath(t), prefsPath, failingSkillsPath(t))
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

	bi, warns := resolveBrowser(l, false, lookPath, prefsPath, failingSkillsPath(t))
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

	bi, warns := resolveBrowser(l, false, lookPath, prefsPath, failingSkillsPath(t))
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

	bi, warns := resolveBrowser(l, false, lookPath, prefsPath, failingSkillsPath(t))
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

	bi, warns := resolveBrowser(l, false, lookPath, prefsPath, skillsPath)
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

	bi, warns := resolveBrowser(l, false, lookPath, prefsPath, skillsPath)
	if !bi.enabled || bi.skillsDir != "" {
		t.Errorf("browserIntegration = %+v, want enabled with no skills dir", bi)
	}
	if len(warns) != 1 || warns[0] != "warning: agent-browser skills path: boom" {
		t.Errorf("warns = %v, want the skills path error wrapped", warns)
	}
}

// cmdTool is a stand-in bash tool whose permission subject is its
// "command" input.
type cmdTool struct{}

func (cmdTool) Name() string           { return "bash" }
func (cmdTool) Description() string    { return "" }
func (cmdTool) Schema() map[string]any { return nil }
func (cmdTool) Concurrent() bool       { return false }
func (cmdTool) Run(context.Context, ext.RunContext, core.ToolCall) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}
func (cmdTool) Subject(_ ext.RunContext, input json.RawMessage) string {
	var in struct{ Command string }
	_ = json.Unmarshal(input, &in)
	return in.Command
}

// browserEnv loads an env with a fake agent-browser on PATH (so the
// integration is enabled) and globalTOML as the global config.
func browserEnv(t *testing.T, globalTOML string) env {
	t.Helper()
	te := newTestEnv(t)
	binDir := t.TempDir()
	writeFakeBin(t, binDir)
	t.Setenv("PATH", binDir)
	te.writeConfig(t, globalTOML)
	e := mustLoadEnv(t, te, (&countingDecider{}).decide)
	if !e.browser.enabled {
		t.Fatal("browser integration not enabled")
	}
	return e
}

// browserVerdict runs the registered permission hook for a bash call of
// cmd under agent, answering any ask with a deny.
func browserVerdict(t *testing.T, e env, agent core.Agent, cmd string) ext.Verdict {
	t.Helper()
	r := ext.NewRegistry()
	if err := addHooks(r, registryDeps{env: e, asker: denyAsker{}}); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"command": cmd})
	rc := ext.RunContext{SessionID: "s", RootID: "s", Agent: agent}
	_, v, err := r.Freeze().ToolHooks()[0].Before(t.Context(), rc, cmdTool{}, core.ToolCall{ID: "c", Name: "bash", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type denyAsker struct{}

func (denyAsker) Ask(context.Context, permission.Request) (core.PermissionReply, error) {
	return core.PermissionReply{Kind: core.ReplyDeny, Message: "asked"}, nil
}

func TestBrowser_PresetRelaxesDefaultAsk(t *testing.T) {
	e := browserEnv(t, "")
	if _, ok := e.cfg().Permissions["bash"]; ok {
		t.Errorf("cfg().Permissions[bash] = %+v, want the preset kept out of config", e.cfg().Permissions["bash"])
	}
	if v := browserVerdict(t, e, core.Agent{}, "agent-browser snapshot -i"); v.Block {
		t.Errorf("snapshot verdict = %+v, want allowed", v)
	}
	for _, cmd := range []string{"agent-browser click @e1", "agent-browser screenshot ~/.bashrc", "agent-browser snapshot $HOME"} {
		if v := browserVerdict(t, e, core.Agent{}, cmd); v.Reason != "user denied: asked" {
			t.Errorf("%q verdict = %+v, want an ask", cmd, v)
		}
	}
}

func TestBrowser_GlobalBashDenyBeatsPreset(t *testing.T) {
	e := browserEnv(t, "[permissions]\nbash = \"deny\"\n")
	if v := browserVerdict(t, e, core.Agent{}, "agent-browser snapshot -i"); !v.Block || v.Reason == "user denied: asked" {
		t.Errorf("verdict = %+v, want denied by rule", v)
	}
}

func TestBrowser_AgentBashDenyBeatsPresetAndStaysHidden(t *testing.T) {
	e := browserEnv(t, "[agents.build.permissions]\nbash = \"deny\"\n")
	ag, err := agents.New(e.cfg(), agents.Sources{GlobalTOML: e.layers.Global.Agents})
	if err != nil {
		t.Fatal(err)
	}
	build, _ := ag.Get("build")
	if v := browserVerdict(t, e, build, "agent-browser snapshot -i"); !v.Block || v.Reason == "user denied: asked" {
		t.Errorf("verdict = %+v, want denied by rule", v)
	}
	for _, tool := range agents.ToolsFor(build, []ext.Tool{cmdTool{}}) {
		if tool.Name() == "bash" {
			t.Error("ToolsFor(build) includes bash, want it hidden (Default deny)")
		}
	}
}

func TestBrowser_TrustedProjectTogglesOverridesGlobal(t *testing.T) {
	l := trust.Layers{
		Global:  core.Config{AgentBrowser: core.ToggleTrue},
		Project: core.Config{AgentBrowser: core.ToggleFalse},
	}
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")

	bi, warns := resolveBrowser(l, true, failingLookPath(t), prefsPath, failingSkillsPath(t))
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

	bi, warns := resolveBrowser(l, false, failingLookPath(t), prefsPath, failingSkillsPath(t))
	if bi.enabled {
		t.Errorf("browserIntegration = %+v, want disabled (untrusted Project ignored)", bi)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

// TestNormalizeSkillsDir_R14 pins R14: when the printed dir itself holds a
// SKILL.md (rather than a subdirectory of it), skillfs would scan dir's
// children instead of finding it, so dir's parent is used instead.
func TestNormalizeSkillsDir_R14(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "agent-browser")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := normalizeSkillsDir(skillDir); got != dir {
		t.Errorf("normalizeSkillsDir(%q) = %q, want parent %q", skillDir, got, dir)
	}
}

// TestNormalizeSkillsDir_NoSkillMD is the normal case: dir holds skill
// subdirectories, not a SKILL.md of its own, so it is returned unchanged.
func TestNormalizeSkillsDir_NoSkillMD(t *testing.T) {
	dir := t.TempDir()
	if got := normalizeSkillsDir(dir); got != dir {
		t.Errorf("normalizeSkillsDir(%q) = %q, want it unchanged", dir, got)
	}
}

// TestNormalizeSkillsDir_RelativePath pins the safe behavior for a
// relative path: os.Stat resolves it against the process's cwd, and since
// that (almost certainly) does not hold "<relative>/SKILL.md", the path
// is returned unchanged rather than misinterpreted.
func TestNormalizeSkillsDir_RelativePath(t *testing.T) {
	rel := "relative/skills/dir/that/does/not/exist"
	if got := normalizeSkillsDir(rel); got != rel {
		t.Errorf("normalizeSkillsDir(%q) = %q, want it unchanged", rel, got)
	}
}

// TestNormalizeSkillsDir_EmbeddedNewline pins the safe behavior for
// garbage output containing an embedded newline: os.Stat fails to find
// "<dir>/SKILL.md" for such a path, so it passes through unchanged rather
// than being split or otherwise reinterpreted. Whatever later tries to use
// it as a skills dir (skillfs.Discover) will just find no such directory.
func TestNormalizeSkillsDir_EmbeddedNewline(t *testing.T) {
	dir := t.TempDir()
	garbage := dir + "\nunexpected extra line"
	if got := normalizeSkillsDir(garbage); got != garbage {
		t.Errorf("normalizeSkillsDir(%q) = %q, want it unchanged", garbage, got)
	}
}
