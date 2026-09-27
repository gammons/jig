// Package agentfs discovers markdown agent definitions on disk: one
// core.AgentConfig per ".md" file directly under an agents directory, per
// spec §6.4.
package agentfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/frontmatter"
	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/paths"
	"github.com/gammons/jig/internal/data/skillfs"
)

// agentMeta is the frontmatter shape of a markdown agent file.
type agentMeta struct {
	Description string         `yaml:"description"`
	Mode        string         `yaml:"mode"`
	Model       string         `yaml:"model"`
	MaxSteps    int            `yaml:"max_steps"`
	CanSpawn    *bool          `yaml:"can_spawn"`
	Hidden      *bool          `yaml:"hidden"`
	Tools       any            `yaml:"tools"`
	Permissions map[string]any `yaml:"permissions"`
}

// Discover scans dirs, in ascending precedence (a later dir's agent wins on
// a name-stem clash), for markdown agent files: ".md" files directly under
// an agents directory. The map key is the file stem (frontmatter "name", if
// present, is ignored). Missing dirs are silently skipped; non-".md" files
// are ignored. Bad frontmatter (invalid YAML, or an invalid permission
// action) produces a warning and that agent is skipped.
func Discover(dirs []string) (map[string]core.AgentConfig, []skillfs.Warning) {
	agents := make(map[string]core.AgentConfig)
	var warnings []skillfs.Warning

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			isFile, warn := resolveIsFile(entry, path)
			if warn != nil {
				warnings = append(warnings, *warn)
				continue
			}
			if !isFile {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".md")
			cfg, warn, ok := readAgent(path)
			if warn != nil {
				warnings = append(warnings, *warn)
			}
			if !ok {
				continue
			}
			agents[name] = cfg
		}
	}
	return agents, warnings
}

// resolveIsFile reports whether entry (at path) is a regular file.
// os.ReadDir reports a symlink's own type (never IsDir, regardless of its
// target), so a symlink is resolved by following it with os.Stat; a
// dangling symlink returns a Warning instead of silently being treated as
// "not a file".
func resolveIsFile(entry os.DirEntry, path string) (bool, *skillfs.Warning) {
	if entry.Type()&os.ModeSymlink == 0 {
		return !entry.IsDir(), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, &skillfs.Warning{Path: path, Msg: fmt.Sprintf("broken symlink: %v", err)}
	}
	return info.Mode().IsRegular(), nil
}

// readAgent reads and parses path into a core.AgentConfig. ok is false if
// the file could not be read or its frontmatter is invalid.
func readAgent(path string) (core.AgentConfig, *skillfs.Warning, bool) {
	src, err := os.ReadFile(path)
	if err != nil {
		return core.AgentConfig{}, nil, false
	}

	var meta agentMeta
	body, err := frontmatter.Parse(src, &meta)
	if err != nil {
		return core.AgentConfig{}, &skillfs.Warning{Path: path, Msg: err.Error()}, false
	}

	perms, err := convertPermissions(meta.Permissions)
	if err != nil {
		return core.AgentConfig{}, &skillfs.Warning{Path: path, Msg: "invalid permissions: " + err.Error()}, false
	}

	return core.AgentConfig{
		Description: meta.Description,
		Mode:        meta.Mode,
		Model:       meta.Model,
		Prompt:      body,
		MaxSteps:    meta.MaxSteps,
		CanSpawn:    meta.CanSpawn,
		Hidden:      meta.Hidden,
		Tools:       parseTools(meta.Tools),
		Permissions: perms,
		Source:      path,
	}, nil, true
}

// parseTools normalizes the "tools" frontmatter field, which may be a YAML
// list or a Claude Code-style comma-separated string, into jig tool names.
func parseTools(raw any) []string {
	switch v := raw.(type) {
	case string:
		var tools []string
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			tools = append(tools, normalizeToolName(part))
		}
		return tools
	case []any:
		var tools []string
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			tools = append(tools, normalizeToolName(s))
		}
		return tools
	default:
		return nil
	}
}

// normalizeToolName lowercases name and maps known Claude Code tool names
// to jig's tool names; unknown names are kept as-is, lowercased.
func normalizeToolName(name string) string {
	switch lower := strings.ToLower(strings.TrimSpace(name)); lower {
	case "multiedit":
		return "edit"
	case "todowrite":
		return "todo"
	default:
		return lower
	}
}

// convertPermissions converts the raw "permissions" frontmatter map (decoded
// generically by yaml.v3 since core.Rule has no YAML unmarshaler) into
// core.PermissionRules.
func convertPermissions(raw map[string]any) (core.PermissionRules, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	rules := make(core.PermissionRules, len(raw))
	for tool, v := range raw {
		rule, err := convertRule(v)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", tool, err)
		}
		rules[tool] = rule
	}
	return rules, nil
}

// convertRule converts one tool's raw permission value, either a bare
// action string ("deny") or a map of glob pattern to action, into a
// core.Rule.
func convertRule(v any) (core.Rule, error) {
	switch val := v.(type) {
	case string:
		action := core.Action(val)
		if err := validateAction(action); err != nil {
			return core.Rule{}, err
		}
		return core.Rule{Default: action}, nil
	case map[string]any:
		patterns := make(map[string]core.Action, len(val))
		for pattern, rawAction := range val {
			s, ok := rawAction.(string)
			if !ok {
				return core.Rule{}, fmt.Errorf("pattern %q: want string action, got %T", pattern, rawAction)
			}
			action := core.Action(s)
			if err := validateAction(action); err != nil {
				return core.Rule{}, err
			}
			patterns[pattern] = action
		}
		return core.Rule{Patterns: patterns}, nil
	default:
		return core.Rule{}, fmt.Errorf("want string or map, got %T", v)
	}
}

func validateAction(a core.Action) error {
	switch a {
	case core.Allow, core.Ask, core.Deny:
		return nil
	default:
		return fmt.Errorf("invalid permission action %q", a)
	}
}

// Dirs returns the agent directories to scan, split into global and
// project scope. global is p.ConfigDir/agents. project is, for each
// directory in the chain from gitRoot down to workDir (or just workDir if
// gitRoot is ""), d/.jig/agents then d/.claude/agents.
func Dirs(p paths.Paths, gitRoot, workDir string) (global, project []string) {
	global = []string{filepath.Join(p.ConfigDir, "agents")}

	for _, d := range projectChain(gitRoot, workDir) {
		project = append(project,
			filepath.Join(d, ".jig", "agents"),
			filepath.Join(d, ".claude", "agents"),
		)
	}
	return global, project
}

// projectChain returns the project directories from gitRoot to workDir,
// inclusive, or just []string{workDir} if gitRoot is "".
func projectChain(gitRoot, workDir string) []string {
	if gitRoot == "" {
		return []string{workDir}
	}
	return fsroot.Chain(gitRoot, workDir)
}
