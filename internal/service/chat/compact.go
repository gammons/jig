package chat

import (
	"context"
	"errors"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/session"
)

// Compact compacts id's history, behind the Runner's busy exclusion: it
// returns core.ErrBusy while a run is in progress on id, and
// session.ErrNothingToCompact unchanged when there is nothing new to
// summarize. A missing session is a *ConfigError, same as Send's resume.
// After Close it returns ErrClosed.
func (s *Service) Compact(ctx context.Context, id core.SessionID) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if _, err := s.d.Sessions.Get(ctx, id); err != nil {
		if errors.Is(err, session.ErrNotFound) {
			return configErr("session %q: %w", id, err)
		}
		return err
	}
	return s.d.Runner.Exclusive(ctx, id, func(c context.Context) error {
		return s.d.Sessions.Compact(c, id)
	})
}
