package core

import (
	"context"
	"errors"
	"time"
)

// MCPTransport is the wire protocol an MCP server speaks.
type MCPTransport string

const (
	MCPStdio MCPTransport = "stdio"
	MCPHTTP  MCPTransport = "http"
	MCPSSE   MCPTransport = "sse"
)

// MCPOAuth configures OAuth for a server that needs it but offers no
// registration support.
type MCPOAuth struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// MCPServer configures one MCP server, from [mcp.servers.<name>] TOML or a
// .mcp.json layer.
type MCPServer struct {
	Name      string
	Source    string
	Transport MCPTransport

	// Command and Args start a stdio server. Env overlays jig's own
	// environment; Cwd defaults per §5.2.
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string

	// URL and Headers configure an http or sse server.
	URL     string
	Headers map[string]string

	OAuth MCPOAuth

	StartupTimeout time.Duration
	ToolTimeout    time.Duration

	// Enabled defaults to true when nil.
	Enabled *bool
}

// MCPConfig is the [mcp] TOML section.
type MCPConfig struct {
	Servers  map[string]MCPServer
	Disabled []string
}

// MCPState is a server's connection lifecycle state.
type MCPState string

const (
	MCPConnecting     MCPState = "connecting"
	MCPReady          MCPState = "ready"
	MCPNeedsAuth      MCPState = "needs_auth"
	MCPAuthenticating MCPState = "authenticating"
	MCPFailed         MCPState = "failed"
	MCPDisabled       MCPState = "disabled"
)

// MCPServerStatus reports one server's current state, for the sidebar,
// status bar, picker, and `jig mcp list`.
type MCPServerStatus struct {
	Name      string
	Source    string
	Transport MCPTransport
	State     MCPState
	Tools     int
	// ToolNames is "<remote name> — <description>", sorted.
	ToolNames []string
	Err       string
	AuthURL   string
	HasToken  bool
}

// ErrMCPNeedsAuth is returned (or wrapped) by a call that hit the closed
// OAuth gate: the server needs sign-in before it can be used.
var ErrMCPNeedsAuth = errors.New("mcp: server needs sign-in")

// MCPService is the port the UIs call to inspect and manage MCP servers.
type MCPService interface {
	Servers(ctx context.Context) ([]MCPServerStatus, error)
	Authenticate(ctx context.Context, name string) error
	CancelAuth(ctx context.Context, name string) error
	Logout(ctx context.Context, name string) error
	Reconnect(ctx context.Context, name string) error
}
