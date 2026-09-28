package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/client/agentbrowser"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/config"
	"github.com/gammons/jig/internal/data/prefsfs"
	"github.com/gammons/jig/internal/service/permission"
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
// so, merges its permission preset into l.Global and resolves its skills
// dir. It must run before trust.Restrict: Restrict drops
// Project.AgentBrowser whole for an untrusted project, and the merged
// preset must already be part of the Global baseline Restrict tightens
// project permissions against.
func resolveBrowser(l trust.Layers, trusted bool, lookPath func(string) (string, error), prefsPath string, skillsPath func(context.Context, string) (string, error)) (trust.Layers, browserIntegration, []string) {
	toggle := browserToggle(l, trusted)
	if toggle == core.ToggleFalse {
		return l, browserIntegration{}, nil
	}

	bin, ok := agentbrowser.Detect(lookPath)
	if !ok {
		if toggle == core.ToggleTrue {
			return l, browserIntegration{}, []string{missingBrowserWarning}
		}
		return l, browserIntegration{}, nil
	}

	l.Global = config.Merge(core.Config{Permissions: permission.AgentBrowserPreset()}, l.Global)
	skillsDir, warn := browserSkillsDir(prefsPath, bin, skillsPath)
	bi := browserIntegration{enabled: true, bin: bin, skillsDir: skillsDir}
	if warn != "" {
		return l, bi, []string{warn}
	}
	return l, bi, nil
}

// browserToggle is Project.AgentBrowser if trusted and set, else
// Global.AgentBrowser.
func browserToggle(l trust.Layers, trusted bool) core.Toggle {
	if trusted && l.Project.AgentBrowser != "" {
		return l.Project.AgentBrowser
	}
	return l.Global.AgentBrowser
}

// browserSkillsDir resolves bin's skills dir from the prefs cache at
// prefsPath (a hit iff it names bin and its ModTime still matches
// os.Stat(bin)), or by calling skillsPath and caching the (normalized)
// result.
func browserSkillsDir(prefsPath, bin string, skillsPath func(context.Context, string) (string, error)) (dir, warn string) {
	info, err := os.Stat(bin)
	if err != nil {
		return "", fmt.Sprintf("warning: agent-browser skills path: %v", err)
	}
	store, err := prefsfs.Open(prefsPath)
	if err != nil {
		return "", fmt.Sprintf("warning: agent-browser skills path: %v", err)
	}

	mod := info.ModTime().UnixNano()
	prefs := store.Get()
	if prefs.AgentBrowser.Bin == bin && prefs.AgentBrowser.ModTime == mod {
		return prefs.AgentBrowser.SkillsDir, ""
	}

	raw, err := skillsPath(context.Background(), bin)
	if err != nil {
		return "", fmt.Sprintf("warning: agent-browser skills path: %v", err)
	}
	dir = normalizeSkillsDir(raw)
	prefs.AgentBrowser = core.AgentBrowserCache{Bin: bin, ModTime: mod, SkillsDir: dir}
	_ = store.Save(prefs)
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
