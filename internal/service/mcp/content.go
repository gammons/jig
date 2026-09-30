package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// mapResult turns a remote tool call's result into a core.ToolResult,
// implementing the §7.3 content mapping.
func mapResult(call core.ToolCall, server, remote string, r RemoteResult, img Imager) core.ToolResult {
	var lines []string
	var media []core.Media
	hasText := false

	for _, c := range r.Content {
		switch c.Kind {
		case "text":
			lines = append(lines, c.Text)
			hasText = true
		case "image":
			if img == nil {
				lines = append(lines, "[image omitted: no image support]")
				continue
			}
			m, _, err := img.Process(c.Data)
			if err != nil {
				lines = append(lines, fmt.Sprintf("[image omitted: %s]", err))
				continue
			}
			media = append(media, m)
		case "audio":
			lines = append(lines, fmt.Sprintf("[audio omitted: %s, %d bytes]", c.MIME, len(c.Data)))
		case "resource_link":
			lines = append(lines, fmt.Sprintf("[resource: %s %s]", c.URI, c.Name))
		case "resource_text":
			lines = append(lines, fmt.Sprintf("--- %s ---\n%s", c.URI, c.Text))
		case "resource_blob":
			lines = append(lines, fmt.Sprintf("[resource omitted: %s, %s]", c.URI, c.MIME))
		}
	}

	if !hasText && len(r.Structured) > 0 {
		var buf bytes.Buffer
		if err := json.Indent(&buf, r.Structured, "", "  "); err == nil {
			lines = append(lines, buf.String())
		} else {
			lines = append(lines, string(r.Structured))
		}
	}

	out := core.ToolResult{
		CallID:  call.ID,
		Name:    call.Name,
		Output:  strings.Join(lines, "\n"),
		IsError: r.IsError,
		Media:   media,
		Metadata: map[string]string{
			"mcp.server": server,
			"mcp.tool":   remote,
		},
	}
	return out
}
