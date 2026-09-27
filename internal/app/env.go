package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/config"
	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/paths"
	"github.com/gammons/jig/internal/service/agents"
)

// env is everything resolved from the process environment and the
// config files before any service is built.
type env struct {
	paths   paths.Paths
	workDir string // absolute
	gitRoot string // "" outside a git repo
	loaded  config.Loaded
	getenv  func(string) string
}

func (e env) cfg() core.Config { return e.loaded.Config }

// loadEnv resolves the XDG paths, the absolute work dir (cwd, or the
// process's working directory if empty), its git root, and the merged
// config. Every error it returns is a configError.
func loadEnv(cwd string, getenv func(string) string) (env, error) {
	workDir, err := resolveWorkDir(cwd)
	if err != nil {
		return env{}, configError{err}
	}
	p, err := paths.Resolve(getenv)
	if err != nil {
		return env{}, configError{err}
	}
	loaded, err := config.Load(p, workDir, getenv)
	if err != nil {
		return env{}, configError{err}
	}
	if err := validateModels(loaded.Config); err != nil {
		return env{}, configError{err}
	}
	gitRoot, _ := fsroot.GitRoot(workDir)
	return env{paths: p, workDir: workDir, gitRoot: gitRoot, loaded: loaded, getenv: getenv}, nil
}

// resolveWorkDir returns cwd as an absolute path, defaulting to the
// process's working directory, and checks that it is a directory.
func resolveWorkDir(cwd string) (string, error) {
	if cwd == "" {
		return os.Getwd()
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
	return abs, nil
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
