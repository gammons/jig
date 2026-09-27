// Package skillfs discovers SKILL.md files on disk: directories directly
// under a "skills" directory that contain a SKILL.md, per spec §6.4.
package skillfs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/frontmatter"
	"github.com/gammons/jig/internal/data/fsroot"
	"github.com/gammons/jig/internal/data/paths"
)

// Warning describes a non-fatal issue found while discovering skills or
// (via agentfs, which reuses this type) markdown agents.
type Warning struct {
	Path string
	Msg  string
}

// skillMeta is the frontmatter shape of a SKILL.md file.
type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Discover scans dirs, in ascending precedence (a later dir's skill wins on
// a name clash), for skills: subdirectories that directly contain a
// SKILL.md. It returns the skills sorted by Name, plus any warnings
// encountered along the way. Missing dirs are silently skipped;
// non-directory entries are ignored.
func Discover(dirs []string) ([]core.Skill, []Warning) {
	found := make(map[string]core.Skill)
	var warnings []Warning

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			isDir, warn := resolveIsDir(entry, path)
			if warn != nil {
				warnings = append(warnings, *warn)
				continue
			}
			if !isDir {
				continue
			}
			skill, warns, ok := readSkill(dir, entry.Name())
			warnings = append(warnings, warns...)
			if !ok {
				continue
			}
			found[skill.Name] = skill
		}
	}

	result := make([]core.Skill, 0, len(found))
	for _, s := range found {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, warnings
}

// resolveIsDir reports whether entry (at path) is a directory. os.ReadDir
// reports a symlink's own type (never a directory, even when it points to
// one), so a symlink is resolved by following it with os.Stat; a dangling
// symlink returns a Warning instead of silently being treated as "not a
// directory".
func resolveIsDir(entry os.DirEntry, path string) (bool, *Warning) {
	if entry.Type()&os.ModeSymlink == 0 {
		return entry.IsDir(), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, &Warning{Path: path, Msg: fmt.Sprintf("broken symlink: %v", err)}
	}
	return info.IsDir(), nil
}

// readSkill reads dir/name/SKILL.md, if present, and builds a core.Skill
// from it. ok is false if there is no SKILL.md, or the skill is skipped
// (missing description).
func readSkill(dir, name string) (core.Skill, []Warning, bool) {
	skillDir := filepath.Join(dir, name)
	path := filepath.Join(skillDir, "SKILL.md")
	src, err := os.ReadFile(path)
	if err != nil {
		return core.Skill{}, nil, false
	}

	var meta skillMeta
	if _, err := frontmatter.Parse(src, &meta); err != nil {
		return core.Skill{}, []Warning{{Path: path, Msg: err.Error()}}, false
	}

	var warnings []Warning
	skillName := name
	if meta.Name != "" {
		skillName = meta.Name
		if meta.Name != name {
			warnings = append(warnings, Warning{
				Path: path,
				Msg:  fmt.Sprintf("frontmatter name %q differs from directory name %q; using frontmatter name", meta.Name, name),
			})
		}
	}

	if meta.Description == "" {
		warnings = append(warnings, Warning{Path: path, Msg: "missing description"})
		return core.Skill{}, warnings, false
	}

	return core.Skill{
		Name:        skillName,
		Description: meta.Description,
		Dir:         skillDir,
		Path:        path,
	}, warnings, true
}

// Dirs returns the skills directories to scan, in ascending precedence
// (Discover's later-dir-wins order): the global dirs (p.ConfigDir/skills,
// ~/.agents/skills, ~/.claude/skills), then extra (skills.paths from
// config), then, for each directory in the project chain from gitRoot down
// to workDir (or just workDir if gitRoot is ""), d/.jig/skills,
// d/.agents/skills, d/.claude/skills.
func Dirs(p paths.Paths, gitRoot, workDir string, extra []string) []string {
	dirs := []string{
		filepath.Join(p.ConfigDir, "skills"),
		filepath.Join(p.Home, ".agents", "skills"),
		filepath.Join(p.Home, ".claude", "skills"),
	}
	dirs = append(dirs, extra...)

	for _, d := range projectChain(gitRoot, workDir) {
		dirs = append(dirs,
			filepath.Join(d, ".jig", "skills"),
			filepath.Join(d, ".agents", "skills"),
			filepath.Join(d, ".claude", "skills"),
		)
	}
	return dirs
}

// projectChain returns the project directories from gitRoot to workDir,
// inclusive, or just []string{workDir} if gitRoot is "".
func projectChain(gitRoot, workDir string) []string {
	if gitRoot == "" {
		return []string{workDir}
	}
	return fsroot.Chain(gitRoot, workDir)
}
