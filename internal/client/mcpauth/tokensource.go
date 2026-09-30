package mcpauth

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/oauth2"

	"github.com/gammons/jig/internal/data/mcptokens"
)

// savingSource wraps an oauth2.TokenSource so that every token it yields —
// whether from the original exchange, a routine refresh, or the
// invalid_grant retry below — is persisted to store when its access or
// refresh token changes. It also implements the invalid_grant race
// handling described on Config.RedirectPort: on that specific error it
// re-reads the stored record once, retries with the fresh refresh token if
// another jig process already rotated it, and otherwise clears the stored
// token so the server is driven to needs_auth. It never retries more than
// once per Token() call.
type savingSource struct {
	mu  sync.Mutex
	ctx context.Context
	cfg *oauth2.Config
	src oauth2.TokenSource

	store        TokenSaver
	serverURL    string
	registration string
	redirectPort int

	// lastAccess/lastRefresh are the values last written to store, so a
	// Token() call that returns the same pair (the common case: a cached,
	// unexpired token) does not re-save. usedRefreshToken is the refresh
	// token believed to have been submitted for the most recent refresh,
	// used to detect whether another process already rotated it.
	lastAccess       string
	lastRefresh      string
	usedRefreshToken string
}

// newSavingSource builds a savingSource. seed, if non-nil, is a token
// already known to match what's in the store (e.g. just loaded from it),
// so the first Token() call that returns it unchanged does not trigger a
// redundant save; pass nil for a brand-new token that must be saved as
// soon as it's produced (a fresh Authorize).
func newSavingSource(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token, store TokenSaver, serverURL, registration string, redirectPort int, seed *oauth2.Token) oauth2.TokenSource {
	s := &savingSource{
		ctx:          ctx,
		cfg:          cfg,
		store:        store,
		serverURL:    serverURL,
		registration: registration,
		redirectPort: redirectPort,
	}
	if seed != nil {
		s.lastAccess = seed.AccessToken
		s.lastRefresh = seed.RefreshToken
		s.usedRefreshToken = seed.RefreshToken
	} else if tok != nil {
		s.usedRefreshToken = tok.RefreshToken
	}
	s.src = cfg.TokenSource(ctx, tok)
	return s
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := s.src.Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if !errors.As(err, &retrieveErr) || retrieveErr.ErrorCode != "invalid_grant" {
			return nil, err
		}
		return s.handleInvalidGrant(err)
	}

	s.saveIfChanged(tok)
	return tok, nil
}

// handleInvalidGrant implements the re-read-once race handling: if another
// jig process already refreshed (the stored refresh token differs from the
// one this source used), retry once with a fresh TokenSource seeded from
// the stored token. Otherwise the token is genuinely invalid: clear it in
// the store (keeping the client registration) and return the original
// error so the caller is driven to needs_auth.
func (s *savingSource) handleInvalidGrant(orig error) (*oauth2.Token, error) {
	rec, ok, loadErr := s.store.Load(s.serverURL)
	if loadErr == nil && ok && rec.Token != nil && rec.Token.RefreshToken != "" && rec.Token.RefreshToken != s.usedRefreshToken {
		fresh := s.cfg.TokenSource(s.ctx, rec.Token)
		tok, err := fresh.Token()
		if err != nil {
			// Never loop: this is the one retry.
			return nil, err
		}
		s.src = fresh
		s.usedRefreshToken = tok.RefreshToken
		s.saveIfChanged(tok)
		return tok, nil
	}

	s.clearToken(rec)
	return nil, orig
}

// saveIfChanged persists tok, along with the static registration fields,
// when its access or refresh token differs from what was last saved.
func (s *savingSource) saveIfChanged(tok *oauth2.Token) {
	if tok.AccessToken == s.lastAccess && tok.RefreshToken == s.lastRefresh {
		return
	}
	rec := s.buildRecord()
	rec.Token = tok
	if err := s.store.Save(rec); err == nil {
		s.lastAccess = tok.AccessToken
		s.lastRefresh = tok.RefreshToken
		s.usedRefreshToken = tok.RefreshToken
	}
}

// clearToken saves base (the record most recently read from the store, if
// any) with Token set to nil, keeping the client registration so a later
// sign-in reuses it.
func (s *savingSource) clearToken(base mcptokens.Record) {
	rec := s.buildRecord()
	rec.Issuer = base.Issuer
	rec.Token = nil
	_ = s.store.Save(rec)
}

// buildRecord assembles a Record from this source's static configuration.
// Issuer is left empty: NewTokenSource is not given the authorization
// server's issuer, only its resolved oauth2.Config, so there is nothing
// authoritative to record here (callers that need it, e.g. clearToken,
// pass along the value from a freshly loaded record instead).
func (s *savingSource) buildRecord() mcptokens.Record {
	return mcptokens.Record{
		ServerURL:    s.serverURL,
		ClientID:     s.cfg.ClientID,
		ClientSecret: s.cfg.ClientSecret,
		Registration: s.registration,
		RedirectPort: s.redirectPort,
		AuthURL:      s.cfg.Endpoint.AuthURL,
		TokenURL:     s.cfg.Endpoint.TokenURL,
		Scopes:       s.cfg.Scopes,
	}
}
