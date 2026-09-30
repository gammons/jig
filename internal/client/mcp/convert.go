package mcp

import (
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// convertTool maps an SDK tool into jig's Tool.
func convertTool(t *sdkmcp.Tool) Tool {
	tool := Tool{
		Name:        t.Name,
		Description: t.Description,
		Schema:      marshalSchema(t.InputSchema),
	}
	if t.Annotations != nil {
		tool.ReadOnly = t.Annotations.ReadOnlyHint
	}
	return tool
}

// marshalSchema re-marshals an SDK tool's InputSchema (which may be a
// map[string]any, a json.RawMessage, or any JSON-marshalable value) into
// a json.RawMessage, jig's wire-agnostic representation.
func marshalSchema(schema any) json.RawMessage {
	if schema == nil {
		return nil
	}
	if raw, ok := schema.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	return data
}

// convertResult maps an SDK CallToolResult into jig's CallResult.
func convertResult(res *sdkmcp.CallToolResult) CallResult {
	out := CallResult{IsError: res.IsError}
	for _, c := range res.Content {
		if content, ok := convertContent(c); ok {
			out.Content = append(out.Content, content)
		}
	}
	if res.StructuredContent != nil {
		if data, err := json.Marshal(res.StructuredContent); err == nil {
			out.Structured = data
		}
	}
	return out
}

// convertContent maps one SDK content block into jig's Content. It
// returns ok=false for content kinds jig doesn't render (currently none
// reachable from a tool result, but future SDK additions fall through
// safely).
func convertContent(c sdkmcp.Content) (Content, bool) {
	switch v := c.(type) {
	case *sdkmcp.TextContent:
		return Content{Kind: "text", Text: v.Text}, true
	case *sdkmcp.ImageContent:
		return Content{Kind: "image", MIME: v.MIMEType, Data: v.Data}, true
	case *sdkmcp.AudioContent:
		return Content{Kind: "audio", MIME: v.MIMEType, Data: v.Data}, true
	case *sdkmcp.ResourceLink:
		return Content{Kind: "resource_link", URI: v.URI, Name: v.Name}, true
	case *sdkmcp.EmbeddedResource:
		return convertResource(v)
	default:
		return Content{}, false
	}
}

// convertResource maps an embedded resource to a text or blob Content,
// depending on which the server populated.
func convertResource(r *sdkmcp.EmbeddedResource) (Content, bool) {
	if r.Resource == nil {
		return Content{}, false
	}
	if r.Resource.Text != "" {
		return Content{Kind: "resource_text", URI: r.Resource.URI, MIME: r.Resource.MIMEType, Text: r.Resource.Text}, true
	}
	return Content{Kind: "resource_blob", URI: r.Resource.URI, MIME: r.Resource.MIMEType, Data: r.Resource.Blob}, true
}
