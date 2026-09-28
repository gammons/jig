// Package paths resolves jig's XDG base directories from environment
// variables. It performs no filesystem I/O: it only builds path strings.
package paths

import (
	"fmt"
	"strings"
)

// Paths holds jig's resolved XDG base directories.
type Paths struct {
	Home      string
	ConfigDir string // $XDG_CONFIG_HOME/jig, default $HOME/.config/jig
	DataDir   string // $XDG_DATA_HOME/jig, default $HOME/.local/share/jig
	CacheDir  string // $XDG_CACHE_HOME/jig, default $HOME/.cache/jig
	StateDir  string // $XDG_STATE_HOME/jig, default $HOME/.local/state/jig
}

// Resolve builds Paths from getenv, the way (*os.Environ)/os.Getenv would
// supply it. It returns an error if any of ConfigDir, DataDir, or CacheDir
// cannot be resolved: that happens when both HOME and the corresponding XDG
// variable are unset. Resolve does no filesystem I/O.
func Resolve(getenv func(string) string) (Paths, error) {
	home := getenv("HOME")

	configDir, err := resolveDir(getenv("XDG_CONFIG_HOME"), home, ".config", "XDG_CONFIG_HOME")
	if err != nil {
		return Paths{}, err
	}
	dataDir, err := resolveDir(getenv("XDG_DATA_HOME"), home, ".local/share", "XDG_DATA_HOME")
	if err != nil {
		return Paths{}, err
	}
	cacheDir, err := resolveDir(getenv("XDG_CACHE_HOME"), home, ".cache", "XDG_CACHE_HOME")
	if err != nil {
		return Paths{}, err
	}
	stateDir, err := resolveDir(getenv("XDG_STATE_HOME"), home, ".local/state", "XDG_STATE_HOME")
	if err != nil {
		return Paths{}, err
	}

	return Paths{
		Home:      home,
		ConfigDir: configDir,
		DataDir:   dataDir,
		CacheDir:  cacheDir,
		StateDir:  stateDir,
	}, nil
}

// resolveDir builds "<base>/jig", where base is xdg if set, or
// "<home>/<defaultSuffix>" otherwise. It errors if xdg is unset and home is
// also unset, since then there is no way to build the default.
func resolveDir(xdg, home, defaultSuffix, xdgName string) (string, error) {
	base := xdg
	if base == "" {
		if home == "" {
			return "", fmt.Errorf("paths: cannot resolve jig directory: HOME and %s are both unset", xdgName)
		}
		base = home + "/" + defaultSuffix
	}
	return base + "/jig", nil
}

// ExpandHome expands a leading "~" or "~/" in p to home. Any other form,
// including "~user/...", is returned unchanged.
func ExpandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return home + p[1:]
	}
	return p
}
