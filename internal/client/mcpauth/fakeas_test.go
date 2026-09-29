package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// farFuture stands in for "never expires" in test data, avoiding
// time.Now() (disallowed in _test.go files; see AGENTS.md). It is kept
// well under year 9999 so it survives time.Time's RFC 3339 JSON
// marshaling, which mcptokens.Record.Token needs when a test round-trips
// it through the store.
var farFuture = time.Unix(1<<34, 0)

// fakeas is a minimal OAuth 2.1 authorization server (PKCE, dynamic client
// registration, authorization-code and refresh-token grants) combined with
// an MCP streamable HTTP resource that requires a bearer token from it. It
// is deliberately small: just enough of RFC 8414/7591/9728 for the
// AuthorizationCodeHandler's flow to complete end to end against a real
// HTTP round trip (loopback only, like httptest.NewServer everywhere else
// in this package).
type fakeas struct {
	mu sync.Mutex

	server *httptest.Server

	// clients maps a registered/preregistered client_id to its secret (""
	// for a public client, matching TokenEndpointAuthMethod "none").
	clients map[string]string
	// codes maps an issued authorization code to its PKCE challenge.
	codes map[string]string
	// refreshTokens maps a refresh token to whether it's still valid
	// (rotated out on use so a stale refresh token can't be replayed).
	refreshTokens map[string]bool
	// accessTokens is the set of access tokens this server has ever
	// issued and not since invalidated; the resource server checks
	// membership.
	accessTokens map[string]bool

	registrations int // count of successful /register calls

	// accessTokenTTL controls expires_in in every token response.
	accessTokenTTL time.Duration
}

// newFakeAS starts a fakeas. preClientID/preSecret, if id is non-empty,
// preregister one client (used by tests that configure Config.Pre).
func newFakeAS(t *testing.T, preClientID, preSecret string) *fakeas {
	f := &fakeas{
		clients:        map[string]string{},
		codes:          map[string]string{},
		refreshTokens:  map[string]bool{},
		accessTokens:   map[string]bool{},
		accessTokenTTL: time.Hour,
	}
	if preClientID != "" {
		f.clients[preClientID] = preSecret
	}

	mux := http.NewServeMux()
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	mux.HandleFunc("/.well-known/oauth-authorization-server", f.handleMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", f.handlePRM)
	mux.HandleFunc("/register", f.handleRegister)
	mux.HandleFunc("/authorize", f.handleAuthorize)
	mux.HandleFunc("/token", f.handleToken)
	mux.Handle("/mcp", f.protectedMCP())

	return f
}

func (f *fakeas) url() string          { return f.server.URL }
func (f *fakeas) mcpURL() string       { return f.server.URL + "/mcp" }
func (f *fakeas) client() *http.Client { return f.server.Client() }

func (f *fakeas) registrationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registrations
}

func (f *fakeas) handleMetadata(w http.ResponseWriter, r *http.Request) {
	meta := oauthex.AuthServerMeta{
		Issuer:                            f.url(),
		AuthorizationEndpoint:             f.url() + "/authorize",
		TokenEndpoint:                     f.url() + "/token",
		JWKSURI:                           f.url() + "/jwks",
		RegistrationEndpoint:              f.url() + "/register",
		ResponseTypesSupported:            []string{"code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		AuthorizationResponseIssParameterSupported: true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (f *fakeas) handlePRM(w http.ResponseWriter, r *http.Request) {
	prm := oauthex.ProtectedResourceMetadata{
		Resource:             f.mcpURL(),
		AuthorizationServers: []string{f.url()},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prm)
}

func (f *fakeas) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var meta oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := rand.Text()

	f.mu.Lock()
	f.clients[id] = ""
	f.registrations++
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&oauthex.ClientRegistrationResponse{
		ClientRegistrationMetadata: meta,
		ClientID:                   id,
	})
}

func (f *fakeas) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")

	f.mu.Lock()
	_, known := f.clients[clientID]
	f.mu.Unlock()
	if !known {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}
	redirectURI := q.Get("redirect_uri")
	challenge := q.Get("code_challenge")
	if redirectURI == "" || challenge == "" {
		http.Error(w, "missing redirect_uri or code_challenge", http.StatusBadRequest)
		return
	}

	code := rand.Text()
	f.mu.Lock()
	f.codes[code] = challenge
	f.mu.Unlock()

	loc := fmt.Sprintf("%s?code=%s&state=%s&iss=%s", redirectURI, code, q.Get("state"), f.url())
	http.Redirect(w, r, loc, http.StatusFound)
}

func (f *fakeas) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		f.handleAuthCodeGrant(w, r)
	case "refresh_token":
		f.handleRefreshGrant(w, r)
	default:
		http.Error(w, "unsupported_grant_type", http.StatusBadRequest)
	}
}

func (f *fakeas) handleAuthCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.Form.Get("code")
	f.mu.Lock()
	challenge, ok := f.codes[code]
	if ok {
		delete(f.codes, code)
	}
	f.mu.Unlock()
	if !ok {
		f.writeTokenError(w, "invalid_grant")
		return
	}
	verifier := r.Form.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		http.Error(w, "PKCE verification failed", http.StatusBadRequest)
		return
	}
	f.issueToken(w)
}

func (f *fakeas) handleRefreshGrant(w http.ResponseWriter, r *http.Request) {
	rt := r.Form.Get("refresh_token")
	f.mu.Lock()
	valid, known := f.refreshTokens[rt]
	if known {
		delete(f.refreshTokens, rt) // rotated: single use
	}
	f.mu.Unlock()
	if !known || !valid {
		f.writeTokenError(w, "invalid_grant")
		return
	}
	f.issueToken(w)
}

// writeTokenError responds with an RFC 6749 §5.2 error body, which
// oauth2.RetrieveError parses into ErrorCode.
func (f *fakeas) writeTokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// issueToken writes a fresh access and refresh token response. Each is a
// unique random string, so tests can tell a refreshed token apart from the
// one it replaced.
func (f *fakeas) issueToken(w http.ResponseWriter) {
	access := "access-" + rand.Text()
	refresh := "refresh-" + rand.Text()

	f.mu.Lock()
	f.accessTokens[access] = true
	f.refreshTokens[refresh] = true
	ttl := f.accessTokenTTL
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"refresh_token": refresh,
		"expires_in":    int(ttl.Seconds()),
	})
}

// mintToken issues a valid access/refresh token pair directly (bypassing
// the authorization-code dance), for tests that seed a store record
// without driving a full interactive sign-in first.
func (f *fakeas) mintToken() *oauth2.Token {
	access := "access-" + rand.Text()
	refresh := "refresh-" + rand.Text()
	f.mu.Lock()
	f.accessTokens[access] = true
	f.refreshTokens[refresh] = true
	f.mu.Unlock()
	return &oauth2.Token{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer"}
}

// protectedMCP returns the MCP resource handler: a streamable HTTP server
// requiring "Bearer <token issued by this server>". A stale, foreign, or
// missing token gets the RFC 9728 challenge that drives discovery.
func (f *fakeas) protectedMCP() http.Handler {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fakeas", Version: "v1"}, nil)
	server.AddTool(&sdkmcp.Tool{
		Name:        "ping",
		Description: "replies pong",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil
	})
	inner := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)

	verifier := func(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
		f.mu.Lock()
		ok := f.accessTokens[token]
		f.mu.Unlock()
		if !ok {
			return nil, sdkauth.ErrInvalidToken
		}
		return &sdkauth.TokenInfo{Expiration: farFuture}, nil
	}
	mw := sdkauth.RequireBearerToken(verifier, &sdkauth.RequireBearerTokenOptions{
		ResourceMetadataURL: f.url() + "/.well-known/oauth-protected-resource/mcp",
	})
	return mw(inner)
}
