package session

import (
	"context"
	"errors"
	"strings"

	"github.com/gammons/jig/internal/core"
)

const (
	// titleAgent is the hidden built-in agent whose prompt drives
	// GenerateTitle.
	titleAgent = "title"
	// maxTitleRunes caps a title's length, ellipsis included.
	maxTitleRunes = 50
	// maxTitlePromptRunes caps the user prompt sent to the title model.
	maxTitlePromptRunes = 2000
	// titleQuotes are stripped from both ends of a generated title.
	titleQuotes = "\"'`“”‘’"
)

// GenerateTitle asks the session's small model for a title for
// firstPrompt and saves it. On any error the current (placeholder) title
// is left unchanged.
func (s *Service) GenerateTitle(ctx context.Context, id core.SessionID, firstPrompt string) error {
	sess, err := s.d.Store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	out, _, err := s.complete(ctx, titleAgent, sess, truncateRunes(firstPrompt, maxTitlePromptRunes))
	if err != nil {
		return err
	}
	title := cleanTitle(out)
	if title == "" {
		return errors.New("session: title model returned no title")
	}
	return s.modify(ctx, id, func(sess *core.Session) { sess.Title = title })
}

// PlaceholderTitle is the title a session gets until GenerateTitle
// replaces it: text's first non-empty line, capped at 50 runes.
func PlaceholderTitle(text string) string {
	return capTitle(firstLine(text))
}

// cleanTitle is out's first non-empty line with surrounding quotes
// trimmed, capped at 50 runes.
func cleanTitle(out string) string {
	for line := range strings.Lines(out) {
		if t := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), titleQuotes)); t != "" {
			return capTitle(t)
		}
	}
	return ""
}

// firstLine returns s's first line that is not blank, trimmed of
// surrounding whitespace, or "".
func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// capTitle shortens s to at most maxTitleRunes runes, ending in "…" when
// it was cut.
func capTitle(s string) string {
	r := []rune(s)
	if len(r) <= maxTitleRunes {
		return s
	}
	return string(r[:maxTitleRunes-1]) + "…"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
