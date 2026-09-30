package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/config"
	"github.com/gammons/jig/internal/data/trustfs"
	"github.com/gammons/jig/internal/pathid"
	"github.com/gammons/jig/internal/service/trust"
)

// untrustedWarning is printed (once, to stderr) by headless runs whose
// untrusted project config had loosening settings dropped.
const untrustedWarning = "warning: project config not trusted; loosening settings ignored (use --trust-project)"

// trustState is the trust decision for the project loadEnv loaded.
type trustState struct {
	project string         // pathid.Key(gitRoot or workDir)
	hash    string         // trustfs.Hash(project config files + project agent files); "" = no project config
	trusted bool           // hash is among the project's stored grants (or was just granted)
	effects []trust.Effect // everything the project layer would change
	dropped []trust.Effect // what Restrict removed (empty when trusted)
}

// trustDecider decides for an untrusted, non-empty project whether to
// trust it; a grant is persisted. Headless: the --trust-project flag. TUI
// (Plan 2b): the trust dialog.
type trustDecider func(st trustState) (grant bool, err error)

// staticTrust is a trustDecider that always answers grant.
func staticTrust(grant bool) trustDecider {
	return func(trustState) (bool, error) { return grant, nil }
}

// warn reports whether st is untrusted and Restrict dropped something,
// so the user should see untrustedWarning.
func (st trustState) warn() bool {
	return !st.trusted && len(st.dropped) > 0
}

// trustStore is the trust grant store at <DataDir>/trust.json.
func trustStore(dataDir string) *trustfs.Store {
	return trustfs.New(filepath.Join(dataDir, "trust.json"))
}

// trustProject is the key a project's grant is stored under: its git
// root, or workDir outside a git repo.
func trustProject(gitRoot, workDir string) string {
	if gitRoot != "" {
		return pathid.Key(gitRoot)
	}
	return pathid.Key(workDir)
}

// projectFiles lists every file that shapes the project layer: the
// project config files Load read and each discovered project agent file
// (files), plus the "{file:}" includes under project and every
// loaded.ProjectMCPFiles path that currently exists (optional). A
// candidate .mcp.json that does not exist is left out entirely rather
// than added as an "absent" optional record: loaded.ProjectMCPFiles lists
// one candidate path per directory in the project chain regardless of
// project config, so unconditionally hashing every candidate would give a
// project with zero config files (and so zero trust effects) a non-empty
// hash, and the trust dialog is never supposed to appear for one (see
// decideTrust). Once a .mcp.json exists, adding, editing, or removing it
// still changes the hash, since its presence or content changes which
// paths/content are hashed. Includes outside project are not hashed.
func projectFiles(project string, loaded config.Loaded, projectMD map[string]core.AgentConfig) (files, optional []string) {
	files = append([]string(nil), loaded.ProjectFiles...)
	for _, ac := range projectMD {
		files = append(files, ac.Source)
	}
	for _, ref := range loaded.ProjectFileRefs {
		if within(project, ref) || within(project, pathid.Key(ref)) {
			optional = append(optional, ref)
		}
	}
	for _, mcpFile := range loaded.ProjectMCPFiles {
		if info, err := os.Stat(mcpFile); err == nil && info.Mode().IsRegular() {
			optional = append(optional, mcpFile)
		}
	}
	return files, optional
}

// within reports whether path is dir or lies under it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hashProject hashes projectFiles(project, loaded, projectMD).
func hashProject(project string, loaded config.Loaded, projectMD map[string]core.AgentConfig) (string, error) {
	return trustfs.HashOptional(projectFiles(project, loaded, projectMD))
}

// readTrust computes st for l: project key, hash, effects, and whether
// the stored grant matches the hash.
func readTrust(store *trustfs.Store, project string, loaded config.Loaded, l trust.Layers) (trustState, error) {
	st := trustState{project: project, effects: trust.Effects(l)}
	hash, err := hashProject(project, loaded, l.ProjectMD)
	if err != nil {
		return trustState{}, err
	}
	st.hash = hash
	if hash == "" {
		return st, nil
	}
	if st.trusted, err = store.Get(project, hash); err != nil {
		return trustState{}, err
	}
	return st, nil
}

// tokenActions converts config's token-valued permission actions for
// trust.Layers.
func tokenActions(in []config.TokenAction) []trust.TokenAction {
	out := make([]trust.TokenAction, 0, len(in))
	for _, t := range in {
		out = append(out, trust.TokenAction(t))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decideTrust asks decide about an untrusted project with effects and
// persists a grant. A project with zero effects (e.g. an empty
// .jig/config.toml) has nothing to trust: decide is not asked and nothing
// is granted.
func decideTrust(st trustState, decide trustDecider, store *trustfs.Store, clk clock.Clock) (trustState, error) {
	if st.trusted || st.hash == "" || len(st.effects) == 0 {
		return st, nil
	}
	grant, err := decide(st)
	if err != nil || !grant {
		return st, err
	}
	if err := store.Put(st.project, trustfs.Grant{Hash: st.hash, GrantedAt: clk.Now()}); err != nil {
		return st, err
	}
	st.trusted = true
	return st, nil
}

// applyTrust restricts l's project layers unless st is trusted, recording
// what was dropped in st.
func applyTrust(l trust.Layers, st trustState) (trust.Layers, trustState) {
	if st.trusted {
		return l, st
	}
	l, st.dropped = trust.Restrict(l)
	return l, st
}
