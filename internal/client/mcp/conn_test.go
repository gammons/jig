package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gammons/jig/internal/core"
)

// objectSchema is a minimal valid JSON Schema accepted by Server.AddTool.
var objectSchema = json.RawMessage(`{"type":"object"}`)

// noopHandler returns an empty, successful result.
func noopHandler(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	return &sdkmcp.CallToolResult{}, nil
}

func TestConn_InMemoryListAndCall(t *testing.T) {
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v1"}, nil)
	server.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "echoes back",
		InputSchema: objectSchema,
	}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "hello"},
				&sdkmcp.ImageContent{Data: []byte("imgdata"), MIMEType: "image/png"},
				&sdkmcp.ResourceLink{URI: "file:///a.txt", Name: "a.txt"},
				&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{
					URI: "file:///b.txt", MIMEType: "text/plain", Text: "embedded text",
				}},
			},
			StructuredContent: map[string]any{"ok": true},
		}, nil
	})
	server.AddTool(&sdkmcp.Tool{
		Name:        "ro",
		Description: "read only",
		InputSchema: objectSchema,
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, noopHandler)

	t1, t2 := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	conn, err := dialTransport(ctx, t2, Spec{}, nil)
	if err != nil {
		t.Fatalf("dialTransport: %v", err)
	}
	defer conn.Close()

	tools, err := conn.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2: %+v", len(tools), tools)
	}
	byName := map[string]Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	if byName["echo"].Description != "echoes back" {
		t.Errorf("echo.Description = %q, want %q", byName["echo"].Description, "echoes back")
	}
	if byName["echo"].ReadOnly {
		t.Errorf("echo.ReadOnly = true, want false")
	}
	if !byName["ro"].ReadOnly {
		t.Errorf("ro.ReadOnly = false, want true")
	}

	res, err := conn.CallTool(ctx, "echo", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false")
	}
	wantKinds := []string{"text", "image", "resource_link", "resource_text"}
	gotKinds := make([]string, len(res.Content))
	for i, c := range res.Content {
		gotKinds[i] = c.Kind
	}
	if !slices.Equal(gotKinds, wantKinds) {
		t.Fatalf("content kinds = %v, want %v (%+v)", gotKinds, wantKinds, res.Content)
	}
	if res.Content[0].Text != "hello" {
		t.Errorf("text content = %q, want %q", res.Content[0].Text, "hello")
	}
	if string(res.Content[1].Data) != "imgdata" || res.Content[1].MIME != "image/png" {
		t.Errorf("image content = %+v, want data %q mime %q", res.Content[1], "imgdata", "image/png")
	}
	if res.Content[2].URI != "file:///a.txt" || res.Content[2].Name != "a.txt" {
		t.Errorf("resource_link = %+v, want uri file:///a.txt name a.txt", res.Content[2])
	}
	if res.Content[3].URI != "file:///b.txt" || res.Content[3].Text != "embedded text" {
		t.Errorf("resource_text = %+v, want uri file:///b.txt text %q", res.Content[3], "embedded text")
	}
	if string(res.Structured) != `{"ok":true}` {
		t.Errorf("Structured = %s, want %s", res.Structured, `{"ok":true}`)
	}
}

func TestConn_ToolsChangedCallback(t *testing.T) {
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v1"}, nil)
	// Register one tool before connecting so the server advertises the
	// tools/listChanged capability from the start of the session.
	server.AddTool(&sdkmcp.Tool{Name: "initial", InputSchema: objectSchema}, noopHandler)

	t1, t2 := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	changed := make(chan struct{}, 1)
	conn, err := dialTransport(ctx, t2, Spec{}, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("dialTransport: %v", err)
	}
	defer conn.Close()

	server.AddTool(&sdkmcp.Tool{Name: "new", InputSchema: objectSchema}, noopHandler)

	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("onToolsChanged was not called within 2s of the server adding a tool")
	}
}

func TestConn_ListToolsPages(t *testing.T) {
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v1"}, &sdkmcp.ServerOptions{PageSize: 1})
	for _, name := range []string{"a", "b", "c"} {
		server.AddTool(&sdkmcp.Tool{Name: name, InputSchema: objectSchema}, noopHandler)
	}

	t1, t2 := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	conn, err := dialTransport(ctx, t2, Spec{}, nil)
	if err != nil {
		t.Fatalf("dialTransport: %v", err)
	}
	defer conn.Close()

	tools, err := conn.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3 (paged through with page size 1): %+v", len(tools), tools)
	}
}

func TestHTTP_HeadersSent(t *testing.T) {
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "ping", InputSchema: objectSchema}, noopHandler)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)

	var mu sync.Mutex
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-Test"); v != "" {
			mu.Lock()
			got = v
			mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	defer ts.Close()

	conn, err := Dial(ctx, Spec{
		Transport: core.MCPHTTP,
		URL:       ts.URL,
		Headers:   map[string]string{"X-Test": "hello"},
	}, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	mu.Lock()
	gotVal := got
	mu.Unlock()
	if gotVal != "hello" {
		t.Errorf("X-Test header seen by the server = %q, want %q", gotVal, "hello")
	}
}
