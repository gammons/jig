package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"

	mcpclient "github.com/gammons/jig/internal/client/mcp"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/mcptokens"
)

func TestHandler_ClosedGateNeedsAuth(t *testing.T) {
	as := newFakeAS(t, "", "")
	store := mcptokens.New(t.TempDir())

	gate := &Gate{} // never Open()ed
	handler, err := NewHandler(context.Background(), Config{
		ServerURL:    as.mcpURL(),
		Store:        store,
		Gate:         gate,
		HTTP:         as.client(),
		RedirectPort: 0, // never dialed: the gate is closed
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	_, err = mcpclient.Dial(context.Background(), mcpclient.Spec{
		Name:       "fakeas",
		Transport:  core.MCPHTTP,
		URL:        as.mcpURL(),
		OAuth:      handler,
		HTTPClient: as.client(),
	}, nil)
	if err == nil {
		t.Fatal("Dial succeeded, want an error")
	}
	if !mcpclient.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(%v) = false, want true", err)
	}
}

// signIn drives a full open-gate sign-in against as, using a fresh loopback
// listener, and returns the resulting store record.
func signIn(t *testing.T, as *fakeas, store *mcptokens.Store, pre *core.MCPOAuth) mcptokens.Record {
	t.Helper()

	l, err := Listen(0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	gate := &Gate{}
	var fetchCalled int32
	gate.Open(func(ctx context.Context, authURL string) (*sdkauth.AuthorizationResult, error) {
		atomic.AddInt32(&fetchCalled, 1)
		go func() {
			// Simulate the user's browser: follow the authorization
			// server's redirect all the way to the loopback listener.
			resp, err := http.Get(authURL)
			if err == nil {
				resp.Body.Close()
			}
		}()
		code, state, iss, err := l.Wait(ctx)
		if err != nil {
			return nil, err
		}
		return &sdkauth.AuthorizationResult{Code: code, State: state, Iss: iss}, nil
	})

	handler, err := NewHandler(context.Background(), Config{
		ServerURL:    as.mcpURL(),
		Pre:          pre,
		Store:        store,
		Gate:         gate,
		HTTP:         as.client(),
		RedirectPort: l.Port(),
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	conn, err := mcpclient.Dial(context.Background(), mcpclient.Spec{
		Name:       "fakeas",
		Transport:  core.MCPHTTP,
		URL:        as.mcpURL(),
		OAuth:      handler,
		HTTPClient: as.client(),
	}, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if atomic.LoadInt32(&fetchCalled) == 0 {
		t.Fatal("fetch was never called")
	}

	rec, ok, err := store.Load(as.mcpURL())
	if err != nil || !ok {
		t.Fatalf("Load after sign-in: ok=%v err=%v", ok, err)
	}
	return rec
}

func TestHandler_OpenGateFullFlow(t *testing.T) {
	as := newFakeAS(t, "", "")
	store := mcptokens.New(t.TempDir())

	rec := signIn(t, as, store, nil)

	if rec.Token == nil || rec.Token.AccessToken == "" {
		t.Fatal("no token saved")
	}
	if rec.ClientID == "" {
		t.Error("no client id saved")
	}
	if rec.Registration != "dynamic" {
		t.Errorf("Registration = %q, want dynamic", rec.Registration)
	}
	if rec.AuthURL == "" || rec.TokenURL == "" {
		t.Errorf("endpoints not saved: AuthURL=%q TokenURL=%q", rec.AuthURL, rec.TokenURL)
	}
	if as.registrationCount() != 1 {
		t.Errorf("registrations = %d, want 1", as.registrationCount())
	}
}

func TestHandler_RestartReusesToken(t *testing.T) {
	as := newFakeAS(t, "", "")
	store := mcptokens.New(t.TempDir())

	rec := signIn(t, as, store, nil)
	if as.registrationCount() != 1 {
		t.Fatalf("registrations after sign-in = %d, want 1", as.registrationCount())
	}

	// A second handler, built fresh from the store (as after a restart),
	// with the gate closed: the stored token is valid, so no fetch and no
	// new registration should happen.
	gate := &Gate{}
	handler2, err := NewHandler(context.Background(), Config{
		ServerURL:    as.mcpURL(),
		Store:        store,
		Gate:         gate,
		HTTP:         as.client(),
		RedirectPort: rec.RedirectPort,
	})
	if err != nil {
		t.Fatalf("NewHandler (restart): %v", err)
	}

	conn, err := mcpclient.Dial(context.Background(), mcpclient.Spec{
		Name:       "fakeas",
		Transport:  core.MCPHTTP,
		URL:        as.mcpURL(),
		OAuth:      handler2,
		HTTPClient: as.client(),
	}, nil)
	if err != nil {
		t.Fatalf("Dial (restart): %v", err)
	}
	defer conn.Close()

	if as.registrationCount() != 1 {
		t.Errorf("registrations after restart = %d, want 1 (no re-registration)", as.registrationCount())
	}
}

func TestHandler_RefreshSaved(t *testing.T) {
	as := newFakeAS(t, "", "")
	store := mcptokens.New(t.TempDir())

	rec := signIn(t, as, store, nil)
	originalAccess := rec.Token.AccessToken

	// Force the stored access token to look expired, so the next use must
	// refresh via the still-valid refresh token.
	rec.Token.Expiry = time.Unix(1, 0)
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	gate := &Gate{} // closed: a refresh must not need interactive auth
	handler2, err := NewHandler(context.Background(), Config{
		ServerURL:    as.mcpURL(),
		Store:        store,
		Gate:         gate,
		HTTP:         as.client(),
		RedirectPort: rec.RedirectPort,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	conn, err := mcpclient.Dial(context.Background(), mcpclient.Spec{
		Name:       "fakeas",
		Transport:  core.MCPHTTP,
		URL:        as.mcpURL(),
		OAuth:      handler2,
		HTTPClient: as.client(),
	}, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	got, ok, err := store.Load(as.mcpURL())
	if err != nil || !ok {
		t.Fatalf("Load after refresh: ok=%v err=%v", ok, err)
	}
	if got.Token == nil || got.Token.AccessToken == originalAccess {
		t.Errorf("access token = %v, want a refreshed value different from %q", got.Token, originalAccess)
	}
}

func TestHandler_PortChangeReRegisters(t *testing.T) {
	as := newFakeAS(t, "", "")
	store := mcptokens.New(t.TempDir())

	rec1 := signIn(t, as, store, nil)
	if as.registrationCount() != 1 {
		t.Fatalf("registrations after first sign-in = %d, want 1", as.registrationCount())
	}

	// Delete the stored record entirely, so the second handler has no
	// InitialTokenSource and must drive a full sign-in through the gate
	// (and thus register) again, rather than reusing the still-known
	// client registration silently.
	if err := store.Delete(as.mcpURL()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// A second sign-in, driven through a fresh listener (a different
	// redirect port, as if the OS handed out a different loopback port
	// this run), must register again and persist the new port.
	rec2 := signIn(t, as, store, nil)
	if rec2.RedirectPort == rec1.RedirectPort {
		t.Skip("both sign-ins happened to get the same loopback port")
	}
	if as.registrationCount() != 2 {
		t.Errorf("registrations after second sign-in = %d, want 2", as.registrationCount())
	}

	got, ok, err := store.Load(as.mcpURL())
	if err != nil || !ok {
		t.Fatalf("Load after second sign-in: ok=%v err=%v", ok, err)
	}
	if got.RedirectPort != rec2.RedirectPort {
		t.Errorf("stored RedirectPort = %d, want %d", got.RedirectPort, rec2.RedirectPort)
	}
}

// refreshViaFakeas hits as's token endpoint directly with
// grant_type=refresh_token, as if another process performed the refresh.
func refreshViaFakeas(t *testing.T, as *fakeas, refreshToken string) *oauth2.Token {
	t.Helper()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"test-client"},
	}
	resp, err := as.client().PostForm(as.url()+"/token", form)
	if err != nil {
		t.Fatalf("refreshViaFakeas: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refreshViaFakeas: status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("refreshViaFakeas: decode: %v", err)
	}
	return &oauth2.Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       farFuture,
	}
}

func TestHandler_InvalidGrantReReads(t *testing.T) {
	t.Run("StoreUpdatedByAnotherProcess", func(t *testing.T) {
		as := newFakeAS(t, "", "")
		store := mcptokens.New(t.TempDir())
		serverURL := as.mcpURL()

		tok0 := as.mintToken()
		rec0 := mcptokens.Record{
			ServerURL:    serverURL,
			ClientID:     "test-client",
			Registration: "preregistered",
			RedirectPort: 9999,
			AuthURL:      as.url() + "/authorize",
			TokenURL:     as.url() + "/token",
			Token: &oauth2.Token{
				AccessToken:  tok0.AccessToken,
				RefreshToken: tok0.RefreshToken,
				Expiry:       time.Unix(1, 0), // already "expired": forces a refresh
			},
		}
		if err := store.Save(rec0); err != nil {
			t.Fatalf("Save: %v", err)
		}

		gate := &Gate{}
		handler, err := NewHandler(context.Background(), Config{
			ServerURL:    serverURL,
			Store:        store,
			Gate:         gate,
			HTTP:         as.client(),
			RedirectPort: rec0.RedirectPort,
		})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}

		// "Another process" refreshes first, rotating out R0 at fakeas and
		// persisting its own record with the new refresh token R1.
		tok1 := refreshViaFakeas(t, as, tok0.RefreshToken)
		rec1 := rec0
		rec1.Token = tok1
		if err := store.Save(rec1); err != nil {
			t.Fatalf("Save (other process): %v", err)
		}

		ts, err := handler.TokenSource(context.Background())
		if err != nil {
			t.Fatalf("TokenSource: %v", err)
		}
		tok, err := ts.Token()
		if err != nil {
			t.Fatalf("Token() after another process's refresh: %v", err)
		}
		if tok.AccessToken != tok1.AccessToken {
			t.Errorf("AccessToken = %q, want the retried value %q", tok.AccessToken, tok1.AccessToken)
		}

		got, ok, err := store.Load(serverURL)
		if err != nil || !ok {
			t.Fatalf("Load: ok=%v err=%v", ok, err)
		}
		if got.Token == nil || got.Token.AccessToken != tok1.AccessToken {
			t.Errorf("stored token = %+v, want access token %q", got.Token, tok1.AccessToken)
		}
	})

	t.Run("NotUpdated", func(t *testing.T) {
		as := newFakeAS(t, "", "")
		store := mcptokens.New(t.TempDir())
		serverURL := as.mcpURL()

		rec0 := mcptokens.Record{
			ServerURL:    serverURL,
			ClientID:     "test-client",
			Registration: "preregistered",
			RedirectPort: 9999,
			AuthURL:      as.url() + "/authorize",
			TokenURL:     as.url() + "/token",
			Token: &oauth2.Token{
				AccessToken:  "stale-access",
				RefreshToken: "stale-refresh-never-registered",
				Expiry:       time.Unix(1, 0),
			},
		}
		if err := store.Save(rec0); err != nil {
			t.Fatalf("Save: %v", err)
		}

		gate := &Gate{}
		handler, err := NewHandler(context.Background(), Config{
			ServerURL:    serverURL,
			Store:        store,
			Gate:         gate,
			HTTP:         as.client(),
			RedirectPort: rec0.RedirectPort,
		})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}

		ts, err := handler.TokenSource(context.Background())
		if err != nil {
			t.Fatalf("TokenSource: %v", err)
		}
		_, err = ts.Token()
		if err == nil {
			t.Fatal("Token() succeeded, want the original invalid_grant error")
		}
		if !strings.Contains(err.Error(), "invalid_grant") {
			t.Errorf("error = %v, want it to mention invalid_grant", err)
		}

		got, ok, err := store.Load(serverURL)
		if err != nil || !ok {
			t.Fatalf("Load: ok=%v err=%v", ok, err)
		}
		if got.Token != nil {
			t.Errorf("stored token = %+v, want nil (cleared)", got.Token)
		}
		if got.ClientID != rec0.ClientID {
			t.Errorf("ClientID = %q, want the registration kept (%q)", got.ClientID, rec0.ClientID)
		}
	})
}
