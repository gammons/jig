package trust

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// restrictMCP splits l.Project.MCP into what an untrusted project keeps
// and what it drops (spec §5.3): Disabled entries and toggle-only entries
// (Transport == "") that set enabled = false both only tighten, so they
// are kept whole. Everything else -- a full server entry (new or
// replacing a global one) and an enabled = true toggle -- could start a
// process or reach a URL the user never approved, so it is dropped.
func restrictMCP(l Layers) (kept, dropped core.MCPConfig) {
	kept.Disabled = append([]string(nil), l.Project.MCP.Disabled...)
	for name, s := range l.Project.MCP.Servers {
		if s.Transport == "" && s.Enabled != nil && !*s.Enabled {
			kept.Servers = putServer(kept.Servers, name, s)
			continue
		}
		dropped.Servers = putServer(dropped.Servers, name, s)
	}
	return kept, dropped
}

// putServer sets m[name] = s, allocating m if it is nil.
func putServer(m map[string]core.MCPServer, name string, s core.MCPServer) map[string]core.MCPServer {
	if m == nil {
		m = make(map[string]core.MCPServer)
	}
	m[name] = s
	return m
}

// mcpEffects describes l.Project.MCP: one effect per server (its Source
// is the defining file), one per Disabled entry (Key "mcp.disabled"), and
// an extra effect for a full server entry whose name also exists in
// l.Global.MCP.Servers, noting the override (spec §5.3). A toggle-only
// entry (Transport == "") is described by its "enabled = " value alone.
func mcpEffects(out []Effect, l Layers) []Effect {
	names := make([]string, 0, len(l.Project.MCP.Servers))
	for name := range l.Project.MCP.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := l.Project.MCP.Servers[name]
		key := "mcp.servers." + seg(name)
		if s.Transport == "" {
			out = append(out, Effect{Key: key, Value: mcpToggleValue(s), Source: s.Source})
			continue
		}
		out = append(out, Effect{Key: key, Value: mcpServerValue(s), Source: s.Source})
		if _, ok := l.Global.MCP.Servers[name]; ok {
			out = append(out, Effect{Key: key, Value: fmt.Sprintf("overrides your global server %q", name)})
		}
	}
	for _, name := range l.Project.MCP.Disabled {
		out = append(out, Effect{Key: "mcp.disabled", Value: name})
	}
	return out
}

// mcpToggleValue renders a toggle-only [mcp.servers.<name>] entry
// (Enabled defaults to true when nil, per core.MCPServer).
func mcpToggleValue(s core.MCPServer) string {
	if s.Enabled != nil && !*s.Enabled {
		return "enabled = false"
	}
	return "enabled = true"
}

// mcpServerValue renders a full MCP server entry: its transport and
// target, plus non-secret details. Env and header keys are shown by name
// only; header values and the OAuth client secret follow secretValue, and
// the URL follows safeURL, so nothing here ever prints a literal secret.
func mcpServerValue(s core.MCPServer) string {
	switch s.Transport {
	case core.MCPStdio:
		v := "stdio " + s.Command
		if len(s.Args) > 0 {
			v += " " + strings.Join(s.Args, " ")
		}
		if len(s.Env) > 0 {
			v += "  env: " + strings.Join(sortedKeys(s.Env), ", ")
		}
		return v
	default: // core.MCPHTTP, core.MCPSSE
		v := string(s.Transport) + " " + safeURL(s.URL)
		if len(s.Headers) > 0 {
			v += "  headers: " + mcpHeaderList(s.Headers)
		}
		if s.OAuth.ClientSecret != "" {
			v += "  oauth client_secret: " + secretValue(tokensIn(nil, s.OAuth.ClientSecret))
		}
		return v
	}
}

// mcpHeaderList renders h as "K1 <secretValue>, K2 <secretValue>",
// sorted by key: secretValue already renders a literal header as
// "(set)" and a token header as the raw token (e.g. "Authorization
// (set)", "X-Env {env:TOKEN}").
func mcpHeaderList(h map[string]string) string {
	keys := sortedKeys(h)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " " + secretValue(tokensIn(nil, h[k]))
	}
	return strings.Join(parts, ", ")
}

// sortedKeys returns m's keys, sorted.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
