package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/paths"
)

// configFileName is the name every config file uses, both the global one
// (p.ConfigDir/config.toml) and each project one (<dir>/.jig/config.toml).
const configFileName = "config.toml"

// Loaded is the result of discovering and merging jig's TOML config files.
type Loaded struct {
	Config core.Config // everything merged

	Files         []string                    // files actually read, in merge order
	GlobalAgents  map[string]core.AgentConfig // [agents] from the global file only
	ProjectAgents map[string]core.AgentConfig // [agents] from project files, merged closer-wins
}

// Load discovers and merges jig's TOML config files: p.ConfigDir/config.toml
// (the global file), then <dir>/.jig/config.toml for each dir returned by
// fsroot.Chain(gitRoot, workDir), root to leaf. When workDir has no git
// root, only workDir/.jig/config.toml is considered. Later files win; see
// the package doc and Task 8's brief for the merge rules per key. A missing
// file is skipped. A TOML syntax error returns an error naming the
// offending file and line.
func Load(p paths.Paths, workDir string, getenv func(string) string) (Loaded, error) {
	files := configFiles(p, workDir)

	var st state
	var read []string
	for i, path := range files {
		dto, md, ok, err := decodeFile(path, p.Home, getenv)
		if err != nil {
			return Loaded{}, err
		}
		if !ok {
			continue
		}
		st.apply(dto, md, path, filepath.Dir(path), p.Home, i == 0)
		read = append(read, path)
	}

	return Loaded{
		Config:        st.cfg,
		Files:         read,
		GlobalAgents:  st.globalAgents,
		ProjectAgents: st.projectAgents,
	}, nil
}

// configFiles returns the config file paths Load considers, in merge order:
// the global file first, then one per directory in the project chain.
func configFiles(p paths.Paths, workDir string) []string {
	files := []string{filepath.Join(p.ConfigDir, configFileName)}

	dirs := []string{workDir}
	if root, ok := fsroot.GitRoot(workDir); ok {
		dirs = fsroot.Chain(root, workDir)
	}
	for _, d := range dirs {
		files = append(files, filepath.Join(d, ".jig", configFileName))
	}
	return files
}

// decodeFile reads path, substitutes "{env:}"/"{file:}" tokens in every
// string, and decodes the result into a tomlFile. ok is false (with a nil
// error) if path does not exist.
func decodeFile(path, home string, getenv func(string) string) (tomlFile, toml.MetaData, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tomlFile{}, toml.MetaData{}, false, nil
		}
		return tomlFile{}, toml.MetaData{}, false, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var generic map[string]any
	if _, err := toml.Decode(string(raw), &generic); err != nil {
		return tomlFile{}, toml.MetaData{}, false, fmt.Errorf("config: %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	substituted, err := substitute(generic, dir, home, getenv)
	if err != nil {
		return tomlFile{}, toml.MetaData{}, false, err
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(substituted); err != nil {
		return tomlFile{}, toml.MetaData{}, false, fmt.Errorf("config: %s: re-encoding after substitution: %w", path, err)
	}

	var dto tomlFile
	md, err := toml.Decode(buf.String(), &dto)
	if err != nil {
		return tomlFile{}, toml.MetaData{}, false, fmt.Errorf("config: %s: %w", path, err)
	}
	return dto, md, true, nil
}
