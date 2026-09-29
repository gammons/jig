// Package mcp wraps the official go-sdk client behind jig-owned types
// (Spec, Tool, Content, CallResult): no go-sdk type ever leaves this
// package. It supports stdio (via client/shell's process-group helpers),
// streamable HTTP (with optional OAuth), and legacy SSE transports.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gammons/jig/internal/core"
)

// implName is the client's advertised implementation name (§4, Spec.4).
const implName = "jig"

// Spec describes one MCP server to connect to.
type Spec struct {
	Name      string
	Transport core.MCPTransport

	// Command and Args start a stdio server. Env is the full environment
	// for the child, as "KEY=VAL"; if nil, os.Environ() is used. Dir is
	// the child's working directory.
	Command string
	Args    []string
	Env     []string
	Dir     string

	// URL and Headers configure an http or sse server. OAuth, if set,
	// authorizes a streamable HTTP connection; it is ignored for sse.
	// HTTPClient is the base client Headers and OAuth build on; if nil,
	// http.DefaultClient is used. The caller's client is never mutated.
	URL        string
	Headers    map[string]string
	OAuth      sdkauth.OAuthHandler
	HTTPClient *http.Client

	// Version is the client's advertised implementation version. It
	// defaults to "dev".
	Version string
}

// Tool is one tool a server advertises.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	ReadOnly    bool
}

// Content is one piece of a tool call result's content. Kind is one of
// "text", "image", "audio", "resource_link", "resource_text", or
// "resource_blob".
type Content struct {
	Kind string
	Text string
	MIME string
	URI  string
	Name string
	Data []byte
}

// CallResult is the outcome of a tool call.
type CallResult struct {
	Content    []Content
	Structured json.RawMessage
	IsError    bool
}

// Conn is a live connection to one MCP server.
type Conn struct {
	session *sdkmcp.ClientSession
	proc    *stdioProc // nil for http/sse

	endErr endErrBox
	done   chan struct{}
}

// endErrBox holds a session's end error: set once, from the goroutine
// that waits on the session, and read from Err, possibly concurrently.
type endErrBox struct {
	mu  sync.Mutex
	err error
}

func (b *endErrBox) set(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

func (b *endErrBox) get() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// Dial connects to the server described by s. onToolsChanged, if non-nil,
// is called whenever the server reports its tool list changed. Client
// capabilities advertise nothing (no roots, sampling, or elicitation).
func Dial(ctx context.Context, s Spec, onToolsChanged func()) (*Conn, error) {
	switch s.Transport {
	case core.MCPStdio:
		return dialStdio(ctx, s, onToolsChanged)
	case core.MCPHTTP:
		return dialTransport(ctx, streamableTransport(s), s, onToolsChanged)
	case core.MCPSSE:
		return dialTransport(ctx, sseTransport(s), s, onToolsChanged)
	default:
		return nil, fmt.Errorf("mcp: unknown transport %q", s.Transport)
	}
}

// dialTransport connects using an already-built SDK transport. It is the
// seam tests use directly with mcp.NewInMemoryTransports, and is also
// what Dial calls for the http and sse cases.
func dialTransport(ctx context.Context, t sdkmcp.Transport, s Spec, onToolsChanged func()) (*Conn, error) {
	client := newClient(s, onToolsChanged)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	return newConn(session, nil), nil
}

// newClient builds the SDK client jig always uses: implementation name
// "jig", the given version (default "dev"), and no advertised
// capabilities beyond the SDK's built-in requirements.
func newClient(s Spec, onToolsChanged func()) *sdkmcp.Client {
	version := s.Version
	if version == "" {
		version = "dev"
	}
	return sdkmcp.NewClient(&sdkmcp.Implementation{Name: implName, Version: version}, &sdkmcp.ClientOptions{
		Capabilities: &sdkmcp.ClientCapabilities{},
		ToolListChangedHandler: func(context.Context, *sdkmcp.ToolListChangedRequest) {
			if onToolsChanged != nil {
				onToolsChanged()
			}
		},
	})
}

// newConn wraps session (and, for stdio, proc) into a Conn, and starts the
// goroutine that closes done when the session ends.
func newConn(session *sdkmcp.ClientSession, proc *stdioProc) *Conn {
	c := &Conn{session: session, proc: proc, done: make(chan struct{})}
	go func() {
		err := session.Wait()
		c.endErr.set(err)
		close(c.done)
	}()
	return c
}

// ListTools lists every tool the server currently offers, paging until
// the server reports no further cursor.
func (c *Conn) ListTools(ctx context.Context) ([]Tool, error) {
	var tools []Tool
	cursor := ""
	for {
		res, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, t := range res.Tools {
			tools = append(tools, convertTool(t))
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return tools, nil
}

// CallTool calls the named tool with args, which must marshal to a JSON
// object (or be nil).
func (c *Conn) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	var arguments any
	if len(args) > 0 {
		arguments = args
	}
	res, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallResult{}, err
	}
	return convertResult(res), nil
}

// Close closes the session and, for stdio, kills the child's whole
// process group so grandchildren (e.g. npx -> node) die with it too.
func (c *Conn) Close() error {
	err := c.session.Close()
	if c.proc != nil {
		c.proc.close()
	}
	return err
}

// Done returns a channel that is closed when the session ends, whether
// through Close or because the server or transport went away.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

// Err returns the session's end error (nil if it ended cleanly, or hasn't
// ended). For stdio, a non-nil error gets ": stderr: <tail>" appended
// when the child wrote anything to stderr.
func (c *Conn) Err() error {
	err := c.endErr.get()
	if err == nil {
		return nil
	}
	if c.proc == nil {
		return err
	}
	if tail := c.proc.stderrTail(); tail != "" {
		return fmt.Errorf("%w: stderr: %s", err, tail)
	}
	return err
}

// IsUnauthorized reports whether err's chain holds core.ErrMCPNeedsAuth,
// which the SDK wraps around an OAuth handler's Authorize error.
func IsUnauthorized(err error) bool {
	return errors.Is(err, core.ErrMCPNeedsAuth)
}
