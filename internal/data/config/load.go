package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"

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
// Project (every .jig/config.toml and .mcp.json root-to-leaf, closer file
// winning within the layer). Callers combine them with Merge once they are
// ready to (internal/app filters an untrusted Project through
// trust.Restrict first).
type Loaded struct {
	Global  core.Config // the global file only
	Project core.Config // every .jig/config.toml and .mcp.json, root->leaf, closer wins

	GlobalFiles  []string // files actually read for Global (0 or 1 entries)
	ProjectFiles []string // .jig/config.toml files actually read for Project, in merge order (TOML only, per R1)

	// ProjectMCPFiles is the .mcp.json path of every directory in the
	// project chain, root to leaf, whether or not the file exists. A
	// later task hashes these as optional files.
	ProjectMCPFiles []string

	// ProjectFileRefs is the resolved absolute path of every "{file:...}"
	// token in the project files, sorted and deduplicated, collected
	// whether or not they were substituted. The files are not read.
	ProjectFileRefs []string

	// ProjectTokenActions are the project permission actions whose value
	// is a "{env:}"/"{file:}" token, when project substitution is off:
	// they are left out of Project (a literal token isn't a valid action)
	// and listed here, sorted by Agent, Tool, Pattern, then Token, so
	// callers can show them. Empty when SubstituteProject is set.
	ProjectTokenActions []TokenAction

	// Warnings holds every non-fatal issue found while loading the
	// project layer's .mcp.json files: an unknown field (per server
	// entry, or an unknown top-level key), and an unset "${VAR}" with no
	// default. Order is deterministic: file order (root to leaf), then
	// sorted within a file. A later task prints them.
	Warnings []string
}

// Options controls Load.
type Options struct {
	// SubstituteProject expands "{env:}"/"{file:}" tokens in project
	// files. Set it only once the project is trusted.
	SubstituteProject bool
}

// Load discovers jig's TOML config files: p.ConfigDir/config.toml (the
// global file) and, for each dir returned by fsroot.Chain(gitRoot,
// workDir), root to leaf, that dir's ".mcp.json" followed by its
// ".jig/config.toml" (spec §5.2's layer order). When workDir has no git
// root, only workDir's files are considered. Within the project layer,
// later files win; see the package doc and Task 8's brief for the merge
// rules per key, and Merge for combining the two layers. A missing file is
// skipped. A TOML syntax error returns an error naming the offending file
// and line.
//
// The global file's "{env:}"/"{file:}" tokens are always substituted; the
// project files' only when o.SubstituteProject is set, so an untrusted
// project can't pull a secret into its layer. The same gate controls
// ".mcp.json"'s "${VAR}"/"${VAR:-default}" expansion. Leaving them literal
// does not change the files' bytes, so a trust hash over them is the same
// either way.
func Load(p paths.Paths, workDir string, getenv func(string) string, o Options) (Loaded, error) {
	global, err := loadLayer(p, []configEntry{{path: globalConfigFile(p), kind: kindTOML}}, getenv, true)
	if err != nil {
		return Loaded{}, err
	}
	entries, mcpFiles := projectConfigFiles(p, workDir)
	project, err := loadLayer(p, entries, getenv, o.SubstituteProject)
	if err != nil {
		return Loaded{}, err
	}
	slices.Sort(project.refs)
	slices.SortFunc(project.tokens, compareTokenActions)
	return Loaded{
		Global:              global.cfg,
		Project:             project.cfg,
		GlobalFiles:         global.read,
		ProjectFiles:        project.readTOML,
		ProjectMCPFiles:     mcpFiles,
		ProjectFileRefs:     slices.Compact(project.refs),
		ProjectTokenActions: project.tokens,
		Warnings:            project.warnings,
	}, nil
}

// layer is one loadLayer result.
type layer struct {
	cfg      core.Config
	read     []string // files actually read (toml and .mcp.json)
	readTOML []string // files actually read, toml only (Loaded.ProjectFiles, per R1)
	refs     []string // resolved "{file:...}" token paths, unsorted
	tokens   []TokenAction
	warnings []string
}

// fileKind distinguishes a config.toml entry from a .mcp.json one within
// projectConfigFiles' ordered list, so loadLayer can dispatch on it.
type fileKind int

const (
	kindTOML fileKind = iota
	kindMCPJSON
)

// configEntry is a fileKind-tagged path, the unit projectConfigFiles and
// loadLayer work with. isProject is false only for the single global
// config.toml entry: it selects the global vs. project default-cwd rule
// for [mcp.servers.*] (§5.2).
type configEntry struct {
	path      string
	kind      fileKind
	isProject bool
}

// loadLayer folds every file in entries (skipping missing ones) into a
// single core.Config, in order, and reports which files it actually read
// and the "{file:...}" paths they reference. subst says whether to
// substitute "{env:}"/"{file:}" tokens in TOML files and "${VAR}" in
// .mcp.json files.
func loadLayer(p paths.Paths, entries []configEntry, getenv func(string) string, subst bool) (layer, error) {
	var st state
	var out layer
	for _, e := range entries {
		switch e.kind {
		case kindMCPJSON:
			warnings, ok, err := st.applyMCPJSON(e.path, subst, getenv)
			if err != nil {
				return layer{}, err
			}
			if !ok {
				continue
			}
			out.read = append(out.read, e.path)
			out.warnings = append(out.warnings, warnings...)
		default:
			d, ok, err := decodeFile(e.path, p.Home, getenv, subst)
			if err != nil {
				return layer{}, err
			}
			if !ok {
				continue
			}
			if err := st.applyMCP(d.dto, d.md, e.path, filepath.Dir(e.path), p.Home, e.isProject); err != nil {
				return layer{}, err
			}
			st.apply(d.dto, d.md, e.path, filepath.Dir(e.path), p.Home)
			out.read = append(out.read, e.path)
			out.readTOML = append(out.readTOML, e.path)
			out.refs = append(out.refs, d.refs...)
			out.tokens = append(out.tokens, d.tokens...)
		}
	}
	out.cfg = st.cfg
	return out, nil
}

// decoded is one decodeFile result.
type decoded struct {
	dto    tomlFile
	md     toml.MetaData
	refs   []string      // resolved "{file:...}" token paths
	tokens []TokenAction // permission actions left as literal tokens
}

// globalConfigFile is the single global config file.
func globalConfigFile(p paths.Paths) string {
	return filepath.Join(p.ConfigDir, configFileName)
}

// projectConfigFiles returns the project config entries Load considers, in
// merge order (root to leaf; within a directory, .mcp.json before
// config.toml, per §5.2), plus every .mcp.json path considered whether or
// not it exists (Loaded.ProjectMCPFiles, per R1).
func projectConfigFiles(p paths.Paths, workDir string) ([]configEntry, []string) {
	dirs := []string{workDir}
	if root, ok := fsroot.GitRoot(workDir); ok {
		dirs = fsroot.Chain(root, workDir)
	}
	entries := make([]configEntry, 0, len(dirs)*2)
	mcpFiles := make([]string, 0, len(dirs))
	for _, d := range dirs {
		mcpFile := filepath.Join(d, mcpJSONFileName)
		mcpFiles = append(mcpFiles, mcpFile)
		entries = append(entries, configEntry{path: mcpFile, kind: kindMCPJSON, isProject: true})
		entries = append(entries, configEntry{path: filepath.Join(d, ".jig", configFileName), kind: kindTOML, isProject: true})
	}
	return entries, mcpFiles
}

// decodeFile reads path, substitutes "{env:}"/"{file:}" tokens in every
// string (only if subst; otherwise they stay literal, except that a
// permission action holding one is taken out, see takeTokenActions), and decodes
// the result into a tomlFile, also collecting its "{file:...}" paths. ok
// is false (with a nil error) if path does not exist.
func decodeFile(path, home string, getenv func(string) string, subst bool) (decoded, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return decoded{}, false, nil
		}
		return decoded{}, false, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var generic map[string]any
	if _, err := toml.Decode(string(raw), &generic); err != nil {
		return decoded{}, false, fmt.Errorf("config: %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	refs := fileRefs(nil, generic, dir, home)
	var tokens []TokenAction
	var substituted any = generic
	if subst {
		substituted, err = substitute(generic, dir, home, getenv)
		if err != nil {
			return decoded{}, false, err
		}
	} else {
		tokens = takeTokenActions(generic)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(substituted); err != nil {
		return decoded{}, false, fmt.Errorf("config: %s: re-encoding after substitution: %w", path, err)
	}

	var dto tomlFile
	md, err := toml.Decode(buf.String(), &dto)
	if err != nil {
		return decoded{}, false, fmt.Errorf("config: %s: %w", path, err)
	}
	return decoded{dto: dto, md: md, refs: refs, tokens: tokens}, true, nil
}
