package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// maxSkillFiles caps the number of files the skill tool lists.
const maxSkillFiles = 50

// skillInput is the JSON input the skill tool accepts.
type skillInput struct {
	ID string `json:"id"`
}

// skillTool implements ext.Tool for the "skill" tool.
type skillTool struct{ svc *Service }

// Tool returns the "skill" tool, backed by s.
func (s *Service) Tool() ext.Tool { return skillTool{svc: s} }

func (skillTool) Name() string { return "skill" }

func (skillTool) Description() string {
	return "Load a skill's full instructions and file list by id. " +
		"Use this when a task matches a skill's description from <available_skills>."
}

func (skillTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "The skill's name, from <available_skills>."},
		},
		"required": []string{"id"},
	}
}

func (skillTool) Concurrent() bool { return true }

// Run implements ext.Tool.
func (t skillTool) Run(ctx context.Context, _ ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in skillInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return core.ToolError(call, fmt.Sprintf("invalid input: %v", err)), nil
	}

	sk, ok := t.svc.byName[in.ID]
	if !ok {
		return core.ToolError(call, fmt.Sprintf("unknown skill %q; available: %s", in.ID, strings.Join(t.svc.names, ", "))), nil
	}

	body, err := t.svc.fsys.ReadBody(sk.Path)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}

	files, err := t.listFiles(sk)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}

	return core.ToolOK(call, formatSkillOutput(sk, body, files)), nil
}

// listFiles returns the absolute paths of every regular file under
// sk.Dir, excluding sk.Dir's top-level SKILL.md, sorted and capped at
// maxSkillFiles.
func (t skillTool) listFiles(sk core.Skill) ([]string, error) {
	topLevelSkillMD := filepath.Join(sk.Dir, "SKILL.md")

	var files []string
	err := t.svc.fsys.WalkDir(sk.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path == topLevelSkillMD {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	if len(files) > maxSkillFiles {
		files = files[:maxSkillFiles]
	}
	return files, nil
}

// formatSkillOutput renders the skill tool's output: the skill's body,
// base directory, and file list.
func formatSkillOutput(sk core.Skill, body string, files []string) string {
	fileTags := make([]string, len(files))
	for i, f := range files {
		fileTags[i] = fmt.Sprintf("<file>%s</file>", f)
	}

	return fmt.Sprintf(
		"<skill_content name=%q>\n# Skill: %s\n\n%s\n\n"+
			"Base directory for this skill: %s\nRelative paths in this skill are relative to this base directory.\n\n"+
			"<skill_files>\n%s\n</skill_files>\n</skill_content>",
		sk.Name, sk.Name, body, sk.Dir, strings.Join(fileTags, "\n"),
	)
}
