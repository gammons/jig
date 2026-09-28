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

// Loaded is the result of discovering jig's TOML config files, kept as two
// separate layers rather than merged: Global (the single global file) and
// Project (every .jig/config.toml root-to-leaf, closer file winning within
// the layer). Callers combine them with Merge once they are ready to
// (Task 15 inserts trust filtering in between).
type Loaded struct {
	Global  core.Config // the global file only
	Project core.Config // every .jig/config.toml, root->leaf, closer wins

	GlobalFiles  []string // files actually read for Global (0 or 1 entries)
	ProjectFiles []string // files actually read for Project, in merge order
}

// Load discovers jig's TOML config files: p.ConfigDir/config.toml (the
// global file) and <dir>/.jig/config.toml for each dir returned by
// fsroot.Chain(gitRoot, workDir), root to leaf. When workDir has no git
// root, only workDir/.jig/config.toml is considered. Within the project
// layer, later files win; see the package doc and Task 8's brief for the
// merge rules per key, and Merge for combining the two layers. A missing
// file is skipped. A TOML syntax error returns an error naming the
// offending file and line.
func Load(p paths.Paths, workDir string, getenv func(string) string) (Loaded, error) {
	global, globalFiles, err := loadLayer(p, []string{globalConfigFile(p)}, getenv)
	if err != nil {
		return Loaded{}, err
	}
	project, projectFiles, err := loadLayer(p, projectConfigFiles(p, workDir), getenv)
	if err != nil {
		return Loaded{}, err
	}
	return Loaded{
		Global:       global,
		Project:      project,
		GlobalFiles:  globalFiles,
		ProjectFiles: projectFiles,
	}, nil
}

// loadLayer folds every file in files (skipping missing ones) into a
// single core.Config, in order, and reports which files it actually read.
func loadLayer(p paths.Paths, files []string, getenv func(string) string) (core.Config, []string, error) {
	var st state
	var read []string
	for _, path := range files {
		dto, md, ok, err := decodeFile(path, p.Home, getenv)
		if err != nil {
			return core.Config{}, nil, err
		}
		if !ok {
			continue
		}
		st.apply(dto, md, path, filepath.Dir(path), p.Home)
		read = append(read, path)
	}
	return st.cfg, read, nil
}

// globalConfigFile is the single global config file.
func globalConfigFile(p paths.Paths) string {
	return filepath.Join(p.ConfigDir, configFileName)
}

// projectConfigFiles returns the project config file paths Load considers,
// root to leaf.
func projectConfigFiles(p paths.Paths, workDir string) []string {
	dirs := []string{workDir}
	if root, ok := fsroot.GitRoot(workDir); ok {
		dirs = fsroot.Chain(root, workDir)
	}
	files := make([]string, 0, len(dirs))
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
