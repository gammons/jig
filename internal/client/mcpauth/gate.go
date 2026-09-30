// Package mcpauth builds a go-sdk auth.AuthorizationCodeHandler for a
// remote MCP server: a gated authorization-code fetcher (closed by
// default, so a background connect never pops a browser), a loopback
// callback listener, a browser opener, and token persistence through
// mcptokens.
package mcpauth

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// Gate is a per-server switch that decides whether the handler's
// AuthorizationCodeFetcher may run an interactive sign-in. Closed (the
// zero value) is the default: any authorization attempt fails immediately
// with core.ErrMCPNeedsAuth, so a plain connect or tool call never blocks
// on user interaction. Open, only for the duration of an explicit sign-in
// attempt, it delegates to fetch.
type Gate struct {
	mu    sync.Mutex
	fetch func(ctx context.Context, authURL string) (*auth.AuthorizationResult, error)
}

// Open installs fetch, so the next authorization attempt (and every one
// until Close) calls it instead of failing.
func (g *Gate) Open(fetch func(ctx context.Context, authURL string) (*auth.AuthorizationResult, error)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fetch = fetch
}

// Close removes any installed fetch, returning the gate to closed.
func (g *Gate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fetch = nil
}

// current returns the installed fetch function, or nil if the gate is
// closed.
func (g *Gate) current() func(ctx context.Context, authURL string) (*auth.AuthorizationResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.fetch
}
