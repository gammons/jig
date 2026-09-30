package app

import (
	"context"
	"encoding/json"
	"testing"

	mcpclient "github.com/gammons/jig/internal/client/mcp"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	mcpsvc "github.com/gammons/jig/internal/service/mcp"
)

// fakeDialer implements mcpsvc.Dialer over a fixed connect function.
type fakeDialer struct {
	dial func(ctx context.Context, srv core.MCPServer) (mcpsvc.Conn, error)
}

func (d fakeDialer) Dial(ctx context.Context, srv core.MCPServer, _ mcpsvc.AuthMode, _ func()) (mcpsvc.Conn, error) {
	return d.dial(ctx, srv)
}

// fakeConn is a minimal mcpsvc.Conn.
type fakeConn struct {
	tools []mcpsvc.RemoteTool
	err   error
	done  chan struct{}
}

func newFakeConn(err error) *fakeConn { return &fakeConn{err: err, done: make(chan struct{})} }

func (c *fakeConn) ListTools(context.Context) ([]mcpsvc.RemoteTool, error) { return c.tools, nil }
func (c *fakeConn) CallTool(context.Context, string, json.RawMessage) (mcpsvc.RemoteResult, error) {
	return mcpsvc.RemoteResult{}, nil
}
func (c *fakeConn) Close() error          { return nil }
func (c *fakeConn) Done() <-chan struct{} { return c.done }
func (c *fakeConn) Err() error            { return c.err }

func TestBuildRegistry_ToolSourceSet(t *testing.T) {
	mgr := mcpsvc.New(mcpsvc.Deps{
		Servers: []core.MCPServer{{Name: "fake", Transport: core.MCPStdio, Command: "true"}},
		Dialer: fakeDialer{dial: func(context.Context, core.MCPServer) (mcpsvc.Conn, error) {
			return newFakeConn(nil), nil
		}},
	})
	view, err := buildRegistry(registryDeps{mcp: mcpToolSource(mgr)})
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	if view.ToolSource() == nil {
		t.Fatal("ToolSource() = nil, want the MCP manager")
	}
}

func TestNoMCPServers_NoManager(t *testing.T) {
	env := newTestEnv(t)
	e, err := loadEnv(env.workDir, env.getenv, staticTrust(false))
	if err != nil {
		t.Fatalf("loadEnv: %v", err)
	}
	if mgr := newMCPManager(e, nil, nil, nil); mgr != nil {
		t.Errorf("newMCPManager = %v, want nil with no configured servers", mgr)
	}
}

func TestHeadless_MCPWarnings(t *testing.T) {
	got := mcpWarnings([]core.MCPServerStatus{
		{Name: "ready1", State: core.MCPReady},
		{Name: "disabled1", State: core.MCPDisabled},
		{Name: "gh", State: core.MCPNeedsAuth},
		{Name: "broken", State: core.MCPFailed, Err: "connection refused"},
	})
	want := []string{
		`mcp: gh needs sign-in; run "jig mcp auth gh"`,
		"mcp: broken failed: connection refused",
	}
	if len(got) != len(want) {
		t.Fatalf("mcpWarnings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDialerAdapter_StdioEnvAndDir(t *testing.T) {
	srv := core.MCPServer{
		Name: "fake", Transport: core.MCPStdio, Command: "fake-cmd", Args: []string{"a"},
		Env: map[string]string{"HOME": "/overridden", "EXTRA": "1"},
	}
	spec := stdioSpec(srv, "/work", []string{"HOME=/orig", "PATH=/bin"})
	wantEnv := map[string]string{"HOME": "/overridden", "PATH": "/bin", "EXTRA": "1"}
	got := map[string]bool{}
	for _, kv := range spec.Env {
		got[kv] = true
	}
	for k, v := range wantEnv {
		if !got[k+"="+v] {
			t.Errorf("Env missing %s=%s; got %v", k, v, spec.Env)
		}
	}
	if spec.Dir != "/work" {
		t.Errorf("Dir (no cwd) = %q, want /work", spec.Dir)
	}
	if spec.Command != "fake-cmd" || len(spec.Args) != 1 || spec.Args[0] != "a" {
		t.Errorf("Command/Args not carried: %+v", spec)
	}

	srv.Cwd = "sub"
	spec = stdioSpec(srv, "/work", nil)
	if spec.Dir != "/work/sub" {
		t.Errorf("Dir (relative cwd) = %q, want /work/sub", spec.Dir)
	}

	srv.Cwd = "/abs/dir"
	spec = stdioSpec(srv, "/work", nil)
	if spec.Dir != "/abs/dir" {
		t.Errorf("Dir (absolute cwd) = %q, want /abs/dir", spec.Dir)
	}
}

func TestConvertRemote(t *testing.T) {
	res := mcpclient.CallResult{
		IsError:    true,
		Structured: json.RawMessage(`{"a":1}`),
		Content: []mcpclient.Content{
			{Kind: "text", Text: "hi"},
			{Kind: "image", MIME: "image/png", Data: []byte{1, 2}},
			{Kind: "audio", MIME: "audio/wav", Data: []byte{3}},
			{Kind: "resource_link", URI: "file:///a", Name: "a"},
			{Kind: "resource_text", URI: "file:///b", MIME: "text/plain", Text: "body"},
			{Kind: "resource_blob", URI: "file:///c", MIME: "application/octet-stream", Data: []byte{9}},
		},
	}
	out := convertRemote(res)
	if !out.IsError || string(out.Structured) != `{"a":1}` {
		t.Fatalf("IsError/Structured not carried: %+v", out)
	}
	if len(out.Content) != len(res.Content) {
		t.Fatalf("Content len = %d, want %d", len(out.Content), len(res.Content))
	}
	for i, c := range res.Content {
		got := out.Content[i]
		if got.Kind != c.Kind || got.Text != c.Text || got.MIME != c.MIME || got.URI != c.URI || got.Name != c.Name || string(got.Data) != string(c.Data) {
			t.Errorf("Content[%d] = %+v, want %+v", i, got, c)
		}
	}
}

var _ ext.ToolSource = (*mcpsvc.Manager)(nil)
