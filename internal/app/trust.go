package app

import (
	"path/filepath"
	"sort"

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
	trusted bool           // a grant for hash is stored (or was just given)
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

// projectFiles lists every file that shapes the project layer: the
// project config files Load read and each discovered project agent file.
func projectFiles(loaded config.Loaded, projectMD map[string]core.AgentConfig) []string {
	files := append([]string(nil), loaded.ProjectFiles...)
	for _, ac := range projectMD {
		files = append(files, ac.Source)
	}
	sort.Strings(files)
	return files
}

// readTrust computes st's project key, hash, and effects for l, and
// whether the stored grant matches the hash.
func readTrust(store *trustfs.Store, gitRoot, workDir string, files []string, l trust.Layers) (trustState, error) {
	st := trustState{project: pathid.Key(workDir), effects: trust.Effects(l)}
	if gitRoot != "" {
		st.project = pathid.Key(gitRoot)
	}
	hash, err := trustfs.Hash(files)
	if err != nil {
		return trustState{}, err
	}
	st.hash = hash
	if hash == "" {
		return st, nil
	}
	g, ok, err := store.Get(st.project)
	if err != nil {
		return trustState{}, err
	}
	st.trusted = ok && g.Hash == hash
	return st, nil
}

// decideTrust asks decide about an untrusted, non-empty project and
// persists a grant.
func decideTrust(st trustState, decide trustDecider, store *trustfs.Store, clk clock.Clock) (trustState, error) {
	if st.trusted || st.hash == "" {
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
