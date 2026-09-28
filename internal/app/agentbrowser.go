package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/client/agentbrowser"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/trust"
)

// browserIntegration is agent-browser's resolved state for this run:
// whether it is enabled, and, if so, its binary and skills dir (empty if
// skills discovery failed). Task 21 uses it to wire screenshot handling.
type browserIntegration struct {
	enabled   bool
	bin       string
	skillsDir string
}

// missingBrowserWarning is printed when the config explicitly enables the
// integration but the binary cannot be found.
const missingBrowserWarning = "warning: integrations.agent_browser.enabled = true but agent-browser is not on PATH"

// resolveBrowser decides whether agent-browser is enabled for l (whose
// Project layer has not yet been trust.Restricted) and trusted, and, if
// so, resolves its skills dir. An untrusted project's toggle is ignored.
// Its permission preset is not a config layer: addHooks hands it to the
// permission hook (permission.WithPreset) when the integration is enabled.
func resolveBrowser(l trust.Layers, trusted bool, lookPath func(string) (string, error), prefs *prefsHandle, skillsPath func(context.Context, string) (string, error)) (browserIntegration, []string) {
	toggle := browserToggle(l, trusted)
	if toggle == core.ToggleFalse {
		return browserIntegration{}, nil
	}

	bin, ok := agentbrowser.Detect(lookPath)
	if !ok {
		if toggle == core.ToggleTrue {
			return browserIntegration{}, []string{missingBrowserWarning}
		}
		return browserIntegration{}, nil
	}

	skillsDir, warn := browserSkillsDir(prefs, bin, skillsPath)
	bi := browserIntegration{enabled: true, bin: bin, skillsDir: skillsDir}
	if warn != "" {
		return bi, []string{warn}
	}
	return bi, nil
}

// browserToggle is Project.AgentBrowser if trusted and set, else
// Global.AgentBrowser.
func browserToggle(l trust.Layers, trusted bool) core.Toggle {
	if trusted && l.Project.AgentBrowser != "" {
		return l.Project.AgentBrowser
	}
	return l.Global.AgentBrowser
}

// browserSkillsDir resolves bin's skills dir from the prefs cache in
// prefs (a hit iff it names bin and its ModTime still matches
// os.Stat(bin)), or by calling skillsPath and caching the (normalized)
// result.
func browserSkillsDir(prefs *prefsHandle, bin string, skillsPath func(context.Context, string) (string, error)) (dir, warn string) {
	info, err := os.Stat(bin)
	if err != nil {
		return "", fmt.Sprintf("warning: agent-browser skills path: %v", err)
	}
	store, err := prefs.open()
	if err != nil {
		return "", fmt.Sprintf("warning: agent-browser skills path: %v", err)
	}

	mod := info.ModTime().UnixNano()
	cached := store.Get().AgentBrowser
	if cached.Bin == bin && cached.ModTime == mod {
		return cached.SkillsDir, ""
	}

	raw, err := skillsPath(context.Background(), bin)
	if err != nil {
		return "", fmt.Sprintf("warning: agent-browser skills path: %v", err)
	}
	dir = normalizeSkillsDir(raw)
	_ = store.Update(func(p *core.Prefs) {
		p.AgentBrowser = core.AgentBrowserCache{Bin: bin, ModTime: mod, SkillsDir: dir}
	})
	return dir, ""
}

// normalizeSkillsDir applies R14: skillfs scans a directory's children for
// SKILL.md files, so if dir itself holds one, its parent is used instead.
func normalizeSkillsDir(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
		return filepath.Dir(dir)
	}
	return dir
}
