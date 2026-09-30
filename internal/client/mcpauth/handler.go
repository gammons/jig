package mcpauth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/mcptokens"
)

// TokenSaver is the storage a Config uses to load and persist OAuth sign-in
// state. mcptokens.Store satisfies it.
type TokenSaver interface {
	Load(url string) (mcptokens.Record, bool, error)
	Save(rec mcptokens.Record) error
}

// Config configures NewHandler.
type Config struct {
	// ServerURL is the MCP server's URL: both the resource identifier and
	// the key TokenSaver stores under.
	ServerURL string

	// Pre, if set with a non-empty ClientID, is used as the
	// PreregisteredClient and always wins over any stored or dynamic
	// registration.
	Pre *core.MCPOAuth

	// Store loads and persists OAuth sign-in state.
	Store TokenSaver

	// Gate gates the interactive authorization-code fetch: closed (the
	// default) fails fast with core.ErrMCPNeedsAuth; open, it delegates
	// to the installed fetch function.
	Gate *Gate

	// HTTP is the client used for every OAuth-related HTTP call
	// (metadata discovery, registration, token exchange, refresh). If
	// nil, SecureClient() is used.
	HTTP *http.Client

	// RedirectPort is the port the loopback callback listener is bound
	// to (or will be bound to). It must be known up front because it
	// determines RedirectURL, which must match what is registered with
	// the authorization server.
	//
	// For a closed-gate connect (no interactive sign-in will happen),
	// the caller may pass the stored record's port, or any fixed
	// placeholder including 0: it is never used to build a real
	// redirect. For an open-gate sign-in, the caller must call
	// Listen(storedPort) first and pass l.Port(), so RedirectURL matches
	// the listener that will actually receive the callback.
	RedirectPort int
}

// NewHandler builds the go-sdk auth.AuthorizationCodeHandler for
// cfg.ServerURL: a gated AuthorizationCodeFetcher, preregistered or dynamic
// client registration (reusing a stored dynamic registration when the
// redirect port hasn't changed), and token persistence through cfg.Store.
//
// If a stored record already holds a token, the returned handler's
// TokenSource is immediately usable (no Authorize round trip) via
// InitialTokenSource; refreshes made through it are saved the same way as
// tokens obtained through a fresh Authorize.
func NewHandler(ctx context.Context, cfg Config) (*auth.AuthorizationCodeHandler, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("mcpauth: Config.Store is required")
	}
	if cfg.Gate == nil {
		return nil, fmt.Errorf("mcpauth: Config.Gate is required")
	}
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("mcpauth: Config.ServerURL is required")
	}

	rec, hasRec, err := cfg.Store.Load(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: loading stored record: %w", err)
	}

	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", cfg.RedirectPort)

	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = SecureClient()
	}

	// Resolve client registration: an explicit Pre always wins; next, a
	// stored dynamic registration is reused as a PreregisteredClient only
	// when the redirect port hasn't changed, so a restart on the same
	// port never re-registers. Otherwise DynamicClientRegistrationConfig
	// (below) drives a fresh registration.
	var preClient *oauthex.ClientCredentials
	registration := "dynamic"
	switch {
	case cfg.Pre != nil && cfg.Pre.ClientID != "":
		preClient = credentials(cfg.Pre.ClientID, cfg.Pre.ClientSecret)
		registration = "preregistered"
	case hasRec && rec.Registration == "dynamic" && rec.RedirectPort == cfg.RedirectPort && rec.ClientID != "":
		preClient = credentials(rec.ClientID, rec.ClientSecret)
		registration = "dynamic"
	}

	// The base context for token sources that outlive this call (the
	// InitialTokenSource below, and any refresh it performs): a startup
	// or connect ctx is typically short-lived or carries a deadline, and
	// must not cancel a refresh minutes or hours later. NewTokenSource
	// doesn't need this: the SDK itself hands it a long-lived
	// context.Background()-derived ctx when it calls NewTokenSource after
	// a fresh Authorize.
	longLived := context.WithoutCancel(ctx)

	var initialTS oauth2.TokenSource
	if hasRec && rec.Token != nil {
		innerCfg := &oauth2.Config{
			ClientID:     rec.ClientID,
			ClientSecret: rec.ClientSecret,
			Endpoint:     oauth2.Endpoint{AuthURL: rec.AuthURL, TokenURL: rec.TokenURL},
			RedirectURL:  redirectURL,
			Scopes:       rec.Scopes,
		}
		refreshCtx := context.WithValue(longLived, oauth2.HTTPClient, httpClient)
		initialTS = newSavingSource(refreshCtx, innerCfg, rec.Token, cfg.Store, cfg.ServerURL, rec.Registration, cfg.RedirectPort, rec.Token)
	}

	handlerCfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:         redirectURL,
		PreregisteredClient: preClient,
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:              "jig",
				RedirectURIs:            []string{redirectURL},
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				TokenEndpointAuthMethod: "none",
			},
		},
		RequestRefreshToken:      true,
		Client:                   httpClient,
		AuthorizationCodeFetcher: gatedFetcher(cfg.Gate),
		NewTokenSource: func(tsCtx context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			return newSavingSource(tsCtx, oc, tok, cfg.Store, cfg.ServerURL, registration, cfg.RedirectPort, nil), nil
		},
		InitialTokenSource: initialTS,
	}

	return auth.NewAuthorizationCodeHandler(handlerCfg)
}

// credentials builds an oauthex.ClientCredentials for id and, if secret is
// non-empty, client-secret authentication.
func credentials(id, secret string) *oauthex.ClientCredentials {
	c := &oauthex.ClientCredentials{ClientID: id}
	if secret != "" {
		c.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: secret}
	}
	return c
}

// gatedFetcher returns the AuthorizationCodeFetcher NewHandler installs: a
// closed gate fails fast with core.ErrMCPNeedsAuth, so a background
// connect or tool call never blocks waiting for user interaction; an open
// gate (only during an explicit sign-in attempt) delegates to its
// installed fetch function.
func gatedFetcher(g *Gate) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		fetch := g.current()
		if fetch == nil {
			return nil, core.ErrMCPNeedsAuth
		}
		return fetch(ctx, args.URL)
	}
}
