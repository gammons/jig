package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
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
// firstPrompt and saves it, but only if the session's title still holds
// the placeholder for firstPrompt at save time: a rename that lands while
// the model is generating a title is never clobbered. On any error, or
// when the title was already renamed, the current title is left
// unchanged and no event is published.
func (s *Service) GenerateTitle(ctx context.Context, id core.SessionID, firstPrompt string) error {
	sess, err := s.d.Store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	// The tags mark the prompt as text to label, not a request to the
	// title model (which otherwise tends to answer it).
	user := "<message>\n" + truncateRunes(firstPrompt, maxTitlePromptRunes) + "\n</message>"
	out, _, err := s.complete(ctx, titleAgent, sess, user)
	if err != nil {
		return err
	}
	line := titleLine(out)
	if line == "" {
		return errors.New("session: title model returned no title")
	}
	// Checked before the cap, which would hide a sentence's end.
	if !looksLikeTitle(line) {
		return fmt.Errorf("session: title model replied instead of titling: %q", capTitle(line))
	}
	title := capTitle(line)
	placeholder := PlaceholderTitle(firstPrompt)
	var saved core.Session
	changed := false
	if err := s.modify(ctx, id, func(sess *core.Session) {
		if sess.Title != placeholder {
			return
		}
		sess.Title = title
		saved = *sess
		changed = true
	}); err != nil {
		return err
	}
	if changed {
		s.d.Bus.Publish(event.SessionUpdated{Base: event.Base{SessionID: id, RootID: id}, Info: saved})
	}
	return nil
}

// PlaceholderTitle is the title a session gets until GenerateTitle
// replaces it: text's first non-empty line, capped at 50 runes.
func PlaceholderTitle(text string) string {
	return capTitle(firstLine(text))
}

// titleLine is out's first non-empty line with surrounding quotes
// trimmed, uncapped.
func titleLine(out string) string {
	for line := range strings.Lines(out) {
		if t := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), titleQuotes)); t != "" {
			return t
		}
	}
	return ""
}

// maxTitleWords is the most words a generated title may have; a longer
// line is a reply, not a title.
const maxTitleWords = 10

// looksLikeTitle reports whether t (a cleaned title) reads as a title,
// not as the model answering the prompt: at most maxTitleWords words,
// and not a sentence (ending in, or containing a break after, '.', '?',
// or '!'). A title like "Bump v1.2" still passes: its '.' is mid-word.
func looksLikeTitle(t string) bool {
	if len(strings.Fields(t)) > maxTitleWords {
		return false
	}
	if strings.ContainsAny(t[len(t)-1:], ".?!") {
		return false
	}
	for _, brk := range []string{". ", "? ", "! "} {
		if strings.Contains(t, brk) {
			return false
		}
	}
	return true
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
