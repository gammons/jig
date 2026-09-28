package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/ui/transcript"
)

// newSessionTitle is the sidebar's title for a session not yet created.
const newSessionTitle = "new session"

// sections builds the sidebar from the session state: Session (title,
// context gauge, cost, agent · model), Todos, and Files. Every row's text
// is sanitized.
func (s *sessionState) sections(workDir string, aliases map[string]string) []sidebar.Section {
	return []sidebar.Section{s.sessionSection(aliases), s.todoSection(), s.fileSection(workDir)}
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

func (s *sessionState) todoSection() sidebar.Section {
	rows := make([]sidebar.Row, 0, len(s.todos))
	for _, t := range s.todos {
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

func (s *sessionState) fileSection(workDir string) sidebar.Section {
	changes := normalizeChanges(workDir, s.proj.ChangedFiles())
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
