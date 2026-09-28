package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/gammons/jig/internal/client/agentbrowser"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/agentfs"
	"github.com/gammons/jig/internal/data/config"
	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/paths"
	"github.com/gammons/jig/internal/data/skillfs"
	"github.com/gammons/jig/internal/pathid"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/trust"
)

// env is everything resolved from the process environment and the
// config files before any service is built.
type env struct {
	paths        paths.Paths
	workDir      string // absolute
	gitRoot      string // "" outside a git repo
	trust        trustState
	layers       trust.Layers      // post-decision: the project layers are restricted unless trusted
	merged       core.Config       // config.Merge(layers.Global, layers.Project), computed once in loadEnv
	agentWarns   []skillfs.Warning // markdown agent discovery warnings, printed by discover
	browser      browserIntegration
	browserWarns []string // agent-browser warnings, printed by discover
	getenv       func(string) string
}

func (e env) cfg() core.Config { return e.merged }

// loadEnv resolves the XDG paths, the absolute work dir (cwd, or the
// process's working directory if empty), its git root, and the config and
// markdown agent layers. It decides project trust (asking decide when the
// project config is unknown or changed) before merging, so e.cfg() and
// e.layers are already the trusted or restricted result. Every error it
// returns is a configError.
func loadEnv(cwd string, getenv func(string) string, decide trustDecider) (env, error) {
	workDir, err := resolveWorkDir(cwd)
	if err != nil {
		return env{}, configError{err}
	}
	p, err := paths.Resolve(getenv)
	if err != nil {
		return env{}, configError{err}
	}
	gitRoot, _ := fsroot.GitRoot(workDir)
	e := env{paths: p, workDir: workDir, gitRoot: gitRoot, getenv: getenv}
	if err := e.resolveLayers(decide, clock.Real()); err != nil {
		return env{}, configError{err}
	}
	e.merged = config.Merge(e.layers.Global, e.layers.Project)
	if err := validateModels(e.merged); err != nil {
		return env{}, configError{err}
	}
	return e, nil
}

// resolveLayers loads the config and markdown agent layers, decides
// trust, and sets e.layers and e.trust. Project config files are loaded
// without "{env:}"/"{file:}" substitution first and re-loaded with it
// only once the project is trusted (see reloadTrusted).
func (e *env) resolveLayers(decide trustDecider, clk clock.Clock) error {
	loaded, err := config.Load(e.paths, e.workDir, e.getenv, config.Options{})
	if err != nil {
		return err
	}
	globalDirs, projectDirs := agentfs.Dirs(e.paths, e.gitRoot, e.workDir)
	globalMD, globalWarns := agentfs.Discover(globalDirs)
	projectMD, projectWarns := agentfs.Discover(projectDirs)
	e.agentWarns = append(globalWarns, projectWarns...)
	l := trust.Layers{
		Global: loaded.Global, Project: loaded.Project, GlobalMD: globalMD, ProjectMD: projectMD,
		ProjectTokens: tokenActions(loaded.ProjectTokenActions),
	}

	store := trustStore(e.paths.DataDir)
	st, err := readTrust(store, trustProject(e.gitRoot, e.workDir), loaded, l)
	if err != nil {
		return err
	}
	if st, err = decideTrust(st, decide, store, clk); err != nil {
		return err
	}
	if st.trusted {
		if l, st, err = e.reloadTrusted(l, st); err != nil {
			return err
		}
	}
	e.browser, e.browserWarns = resolveBrowser(l, st.trusted, exec.LookPath, e.prefsPath(), agentbrowser.SkillsPath)
	e.layers, e.trust = applyTrust(l, st)
	return nil
}

// reloadTrusted re-loads the config with project substitution for a
// trusted st and re-hashes the project files. If the hash no longer
// matches st.hash (a file changed since the decision), the project is
// untrusted for this run and l is returned unsubstituted.
func (e *env) reloadTrusted(l trust.Layers, st trustState) (trust.Layers, trustState, error) {
	loaded, err := config.Load(e.paths, e.workDir, e.getenv, config.Options{SubstituteProject: true})
	if err != nil {
		return l, st, err
	}
	hash, err := hashProject(st.project, loaded, l.ProjectMD)
	if err != nil {
		return l, st, err
	}
	if hash != st.hash {
		st.trusted = false
		return l, st, nil
	}
	l.Global, l.Project, l.ProjectTokens = loaded.Global, loaded.Project, nil
	return l, st, nil
}

// resolveWorkDir returns cwd as an absolute, canonical path (see
// pathid.Key), defaulting to the process's working directory, and checks
// that it is a directory.
func resolveWorkDir(cwd string) (string, error) {
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return pathid.Key(wd), nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("--cwd: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--cwd: %s is not a directory", abs)
	}
	return pathid.Key(abs), nil
}

// validateModels checks that default_model and small_model, when set,
// are "provider/model" refs or defined [model_aliases] names.
func validateModels(cfg core.Config) error {
	for _, m := range []struct{ key, val string }{
		{"default_model", cfg.DefaultModel},
		{"small_model", cfg.SmallModel},
	} {
		if m.val == "" {
			continue
		}
		if _, err := agents.ParseRef(m.val, cfg.ModelAliases); err != nil {
			return fmt.Errorf("config: %s: %w", m.key, err)
		}
	}
	return nil
}

// warnProviderOptions warns, in provider-ID order, about every provider
// whose options table is set: options are not wired to any client yet.
// The jigtest provider is exempt, since it reads its script from them.
func warnProviderOptions(w io.Writer, providers map[string]core.ProviderConfig) {
	ids := make([]string, 0, len(providers))
	for id, p := range providers {
		if len(p.Options) > 0 && p.Type != "jigtest" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(w, "warning: providers.%s.options is not supported yet and is ignored\n", id)
	}
}
