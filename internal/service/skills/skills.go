// Package skills discovers and serves SKILL.md-based skills: a context
// transform that lists them for the model, and a "skill" tool that loads
// one's full content on demand.
package skills

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// FS is the filesystem capability skills needs: reading a skill's SKILL.md
// body (with frontmatter already stripped by the caller) and walking a
// skill's directory tree.
type FS interface {
	ReadBody(path string) (string, error)
	WalkDir(root string, fn fs.WalkDirFunc) error
}

// Service holds the discovered skills and serves the list transform and
// skill tool.
type Service struct {
	byName map[string]core.Skill
	names  []string // sorted
	fsys   FS
}

// New returns a Service backed by list and fsys.
func New(list []core.Skill, fsys FS) *Service {
	byName := make(map[string]core.Skill, len(list))
	names := make([]string, 0, len(list))
	for _, sk := range list {
		byName[sk.Name] = sk
		names = append(names, sk.Name)
	}
	sort.Strings(names)
	return &Service{byName: byName, names: names, fsys: fsys}
}

// listTransform implements ext.ContextTransform, listing s's skills for
// the model.
type listTransform struct{ svc *Service }

// Transform returns the priority-40 ContextTransform that lists s's
// skills, when the model has a "skill" tool available.
func (s *Service) Transform() ext.ContextTransform {
	return listTransform{svc: s}
}

func (listTransform) Priority() int { return 40 }

// Transform appends nothing when there are no skills, or when req.Tools
// does not include a tool named "skill". Otherwise it appends an
// <available_skills> listing, sorted by name.
func (t listTransform) Transform(_ context.Context, _ ext.RunContext, req *core.LLMRequest) error {
	if len(t.svc.names) == 0 || !hasSkillTool(req.Tools) {
		return nil
	}

	var b strings.Builder
	b.WriteString("Skills provide specialized instructions and workflows for specific tasks.\n")
	b.WriteString("Use the skill tool to load a skill when a task matches its description.\n")
	b.WriteString("<available_skills>\n")
	for _, name := range t.svc.names {
		sk := t.svc.byName[name]
		fmt.Fprintf(&b, "  <skill>\n    <id>%s</id>\n    <description>%s</description>\n  </skill>\n", sk.Name, sk.Description)
	}
	b.WriteString("</available_skills>")

	req.System = append(req.System, b.String())
	return nil
}

// hasSkillTool reports whether tools includes one named "skill".
func hasSkillTool(tools []core.ToolSpec) bool {
	for _, t := range tools {
		if t.Name == "skill" {
			return true
		}
	}
	return false
}
