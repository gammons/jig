package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

// newSessionTitle is the sidebar's title for a session not yet created.
const newSessionTitle = "new session"

// sections builds the sidebar from the session state: Session (title,
// context gauge, cost, agent · model), Todos, Files, Subagents, and
// Browser. Every row's text is sanitized.
func (s *sessionState) sections(workDir string, aliases map[string]string) []sidebar.Section {
	blocks := s.proj.Blocks()
	return []sidebar.Section{
		s.sessionSection(aliases), todoSection(s.todos), fileSection(workDir, s.proj.ChangedFiles()),
		subagentSection(blocks), browserSection(s.proj.LastBrowserURL(), blocks),
	}
}

func (s *sessionState) sessionSection(aliases map[string]string) sidebar.Section {
	title := s.info.Title
	if title == "" {
		title = newSessionTitle
	}
	ref := s.modelRef()
	rows := []sidebar.Row{{Text: ansi.SanitizeLine(title)}}
	if limit := s.contextWindow(ref); limit > 0 {
		rows = append(rows, sidebar.Row{Gauge: &sidebar.Gauge{Used: s.usage.Input + s.usage.CacheRead, Limit: limit}})
	}
	rows = append(rows,
		sidebar.Row{Text: fmt.Sprintf("$%.2f", s.cost), Tone: sidebar.Muted},
		sidebar.Row{Text: ansi.SanitizeLine(s.info.Agent + " · " + displayModel(ref, aliases)), Tone: sidebar.Muted},
	)
	return sidebar.Section{Title: "Session", Rows: rows}
}

// todoSection lists todos: ✓ done, ● in progress, ○ pending.
func todoSection(todos []core.Todo) sidebar.Section {
	rows := make([]sidebar.Row, 0, len(todos))
	for _, t := range todos {
		row := sidebar.Row{Icon: "○", Text: ansi.SanitizeLine(t.Content), Tone: sidebar.Muted}
		switch t.Status {
		case "completed":
			row.Icon, row.Tone = "✓", sidebar.Success
		case "in_progress":
			row.Icon, row.Tone = "●", sidebar.Accent
		}
		rows = append(rows, row)
	}
	return sidebar.Section{Title: "Todos", Rows: rows}
}

// fileSection lists changed files, A(dded) or M(odified).
func fileSection(workDir string, changed []transcript.FileChange) sidebar.Section {
	changes := normalizeChanges(workDir, changed)
	rows := make([]sidebar.Row, 0, len(changes))
	for _, c := range changes {
		icon, tone := "M", sidebar.Warning
		if c.Kind == transcript.ChangeAdded {
			icon, tone = "A", sidebar.Success
		}
		rows = append(rows, sidebar.Row{Icon: icon, Text: ansi.SanitizeLine(c.Path), Tone: tone})
	}
	return sidebar.Section{Title: "Files", Rows: rows}
}

// normalizeChanges resolves each changed path (as the tool input gave it)
// against workDir and cleans it, so "a.go", "./a.go" and "<workDir>/a.go"
// are one file, listed with the kind it was first listed with. Paths
// inside workDir are shown relative to it; others stay absolute.
func normalizeChanges(workDir string, in []transcript.FileChange) []transcript.FileChange {
	seen := make(map[string]bool, len(in))
	out := make([]transcript.FileChange, 0, len(in))
	for _, c := range in {
		p := c.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(workDir, p)
		}
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		if rel, err := filepath.Rel(workDir, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			p = filepath.ToSlash(rel)
		}
		out = append(out, transcript.FileChange{Path: p, Kind: c.Kind})
	}
	return out
}

// subagentSection lists each Subagent block: its state icon, agent, and
// description.
func subagentSection(blocks []transcript.Block) sidebar.Section {
	var rows []sidebar.Row
	for _, b := range blocks {
		if b.Kind != transcript.KindSubagent || b.Sub == nil {
			continue
		}
		icon, tone := subagentIcon(b.State)
		text := ansi.SanitizeLine(b.Sub.Agent)
		if d := ansi.SanitizeLine(b.Sub.Description); d != "" {
			text += "  " + d
		}
		rows = append(rows, sidebar.Row{Icon: icon, Text: text, Tone: tone})
	}
	return sidebar.Section{Title: "Subagents", Rows: rows}
}

// subagentIcon is the sidebar icon and tone for a subagent's state.
func subagentIcon(st transcript.ToolState) (string, sidebar.Tone) {
	switch st {
	case transcript.StateOK:
		return "✓", sidebar.Success
	case transcript.StateError:
		return "✗", sidebar.Error
	case transcript.StateDenied, transcript.StateCancelled:
		return "⊘", sidebar.Muted
	case transcript.StateAwaiting:
		return "⚠", sidebar.Warning
	case transcript.StatePending:
		return "○", sidebar.Muted
	}
	return "●", sidebar.Accent
}

// browserSection shows the last agent-browser URL and the session of the
// latest root agent-browser command (its --session, else "default");
// nothing until a navigation has happened.
func browserSection(url string, blocks []transcript.Block) sidebar.Section {
	if url == "" {
		return sidebar.Section{Title: "Browser"}
	}
	name := "default"
	for i := len(blocks) - 1; i >= 0; i-- {
		if n, ok := browserSessionOf(blocks[i]); ok {
			name = n
			break
		}
	}
	return sidebar.Section{Title: "Browser", Rows: []sidebar.Row{
		{Icon: "🌐", Text: ansi.SanitizeLine(url)},
		{Text: "session " + ansi.SanitizeLine(name), Tone: sidebar.Muted},
	}}
}

// browserSessionOf is the agent-browser session a root bash block's
// command runs in, if it invokes agent-browser.
func browserSessionOf(b transcript.Block) (string, bool) {
	if b.Kind != transcript.KindTool || b.Call == nil || b.Call.Name != "bash" {
		return "", false
	}
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(b.Call.Input, &in) != nil {
		return "", false
	}
	return transcript.BrowserSession(in.Command)
}
