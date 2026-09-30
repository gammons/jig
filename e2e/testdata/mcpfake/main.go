// Command mcpfake is a small, scripted MCP stdio server used by
// internal/client/mcp's tests and by e2e tests. It reads its script from
// the file named by $MCPFAKE_SCRIPT and serves tools, replies, and
// failure modes described there.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// reply is the scripted response for one tool call.
type reply struct {
	Text      string `json:"text"`
	ImageB64  string `json:"image_b64"`
	ImageMIME string `json:"image_mime"`
	IsError   bool   `json:"is_error"`
}

// scriptedTool is one tool the fake server advertises.
type scriptedTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	ReadOnly    bool            `json:"read_only"`
	Reply       reply           `json:"reply"`
}

// script is the whole scenario read from $MCPFAKE_SCRIPT.
type script struct {
	Tools          []scriptedTool `json:"tools"`
	CrashOnCall    bool           `json:"crash_on_call"`
	HangInitialize bool           `json:"hang_initialize"`
	Stderr         string         `json:"stderr"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mcpfake:", err)
		os.Exit(1)
	}
}

func run() error {
	path := os.Getenv("MCPFAKE_SCRIPT")
	if path == "" {
		return fmt.Errorf("MCPFAKE_SCRIPT not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s script
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("parsing script: %w", err)
	}

	if s.Stderr != "" {
		fmt.Fprintln(os.Stderr, s.Stderr)
	}
	if s.HangInitialize {
		select {}
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "mcpfake", Version: "dev"}, nil)
	for _, t := range s.Tools {
		addTool(server, t, s.CrashOnCall)
	}

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// addTool registers one scripted tool on server. Its handler replies with
// the scripted content, or (if crashOnCall) exits the process instead of
// answering.
func addTool(server *mcp.Server, t scriptedTool, crashOnCall bool) {
	schema := t.Schema
	if len(schema) == 0 {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	tool := &mcp.Tool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: schema,
	}
	if t.ReadOnly {
		tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	server.AddTool(tool, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if crashOnCall {
			os.Exit(3)
		}
		return replyResult(t.Reply), nil
	})
}

// replyResult builds a CallToolResult from a scripted reply.
func replyResult(r reply) *mcp.CallToolResult {
	var content []mcp.Content
	if r.Text != "" {
		content = append(content, &mcp.TextContent{Text: r.Text})
	}
	if r.ImageB64 != "" {
		data, err := base64.StdEncoding.DecodeString(r.ImageB64)
		if err != nil {
			data = nil
		}
		content = append(content, &mcp.ImageContent{Data: data, MIMEType: r.ImageMIME})
	}
	return &mcp.CallToolResult{Content: content, IsError: r.IsError}
}
