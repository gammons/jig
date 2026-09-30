package trust

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestRestrict_DropsProjectServers(t *testing.T) {
	l := Layers{Project: core.Config{MCP: core.MCPConfig{Servers: map[string]core.MCPServer{
		"playwright": {
			Name: "playwright", Source: ".mcp.json", Transport: core.MCPStdio,
			Command: "npx", Args: []string{"-y", "@playwright/mcp@latest"},
			Env: map[string]string{"DEBUG": "1"},
		},
	}}}}
	got, dropped := Restrict(l)
	if got.Project.MCP.Servers != nil {
		t.Errorf("kept Servers = %v, want nil", got.Project.MCP.Servers)
	}
	want := []string{"mcp.servers.playwright (from .mcp.json) → stdio npx -y @playwright/mcp@latest  env: DEBUG"}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, want) {
		t.Errorf("dropped = %q, want %q", g, want)
	}
}

func TestRestrict_KeepsDisables(t *testing.T) {
	enabled := false
	l := Layers{Project: core.Config{MCP: core.MCPConfig{
		Servers:  map[string]core.MCPServer{"github": {Name: "github", Source: ".mcp.json", Enabled: &enabled}},
		Disabled: []string{"other"},
	}}}
	got, dropped := Restrict(l)
	if !reflect.DeepEqual(got.Project.MCP.Disabled, []string{"other"}) {
		t.Errorf("kept Disabled = %v, want [other]", got.Project.MCP.Disabled)
	}
	gotServer, ok := got.Project.MCP.Servers["github"]
	if !ok || gotServer.Enabled == nil || *gotServer.Enabled {
		t.Errorf("kept Servers[github] = %#v, want the enabled=false toggle kept", gotServer)
	}
	if dropped != nil {
		t.Errorf("dropped = %v, want nil", effectStrings(dropped))
	}
}

func TestRestrict_DropsEnableToggle(t *testing.T) {
	enabled := true
	l := Layers{Project: core.Config{MCP: core.MCPConfig{
		Servers: map[string]core.MCPServer{"github": {Name: "github", Source: ".mcp.json", Enabled: &enabled}},
	}}}
	got, dropped := Restrict(l)
	if got.Project.MCP.Servers != nil {
		t.Errorf("kept Servers = %v, want nil", got.Project.MCP.Servers)
	}
	want := []string{"mcp.servers.github (from .mcp.json) → enabled = true"}
	if g := effectStrings(dropped); !reflect.DeepEqual(g, want) {
		t.Errorf("dropped = %q, want %q", g, want)
	}
}

func TestEffects_MCP(t *testing.T) {
	l := Layers{
		Global: core.Config{MCP: core.MCPConfig{Servers: map[string]core.MCPServer{
			"github": {Name: "github", Transport: core.MCPHTTP, URL: "https://api.githubcopilot.com/mcp/"},
		}}},
		Project: core.Config{MCP: core.MCPConfig{Servers: map[string]core.MCPServer{
			"playwright": {
				Name: "playwright", Source: ".mcp.json", Transport: core.MCPStdio,
				Command: "npx", Args: []string{"-y", "@playwright/mcp@latest"},
				Env: map[string]string{"DEBUG": "1"},
			},
			"github": {
				Name: "github", Source: ".jig/config.toml", Transport: core.MCPHTTP,
				URL:     "https://api.githubcopilot.com/mcp/",
				Headers: map[string]string{"Authorization": "Bearer tok"},
			},
		}}},
	}
	want := []string{
		"mcp.servers.github (from .jig/config.toml) → http https://api.githubcopilot.com/mcp/  headers: Authorization (set)",
		`mcp.servers.github → overrides your global server "github"`,
		"mcp.servers.playwright (from .mcp.json) → stdio npx -y @playwright/mcp@latest  env: DEBUG",
	}
	got := effectStrings(Effects(l))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effects =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEffects_MCPSecrets(t *testing.T) {
	l := Layers{Project: core.Config{MCP: core.MCPConfig{Servers: map[string]core.MCPServer{
		"a": {
			Name: "a", Source: ".mcp.json", Transport: core.MCPHTTP,
			URL:     "https://user:pw@example.com/v1?key=abc",
			Headers: map[string]string{"X-Literal": "sk-secret", "X-Env": "${GH_TOKEN}", "X-File": "{env:X}"},
		},
	}}}}
	effects := Effects(l)
	if len(effects) != 1 {
		t.Fatalf("got %d effects, want 1: %v", len(effects), effectStrings(effects))
	}
	v := effects[0].Value
	if !strings.Contains(v, "https://example.com/v1?…") {
		t.Errorf("value %q missing safeURL rendering", v)
	}
	if !strings.Contains(v, "X-Env ${GH_TOKEN}") {
		t.Errorf("value %q missing raw ${VAR} token", v)
	}
	if !strings.Contains(v, "X-File {env:X}") {
		t.Errorf("value %q missing raw {env:} token", v)
	}
	if !strings.Contains(v, "X-Literal (set)") {
		t.Errorf("value %q missing (set) for literal header", v)
	}
	if strings.Contains(v, "sk-secret") || strings.Contains(v, "pw") || strings.Contains(v, "key=abc") {
		t.Errorf("value leaks a secret: %q", v)
	}
}

func TestEffects_MCPOAuthClientSecret(t *testing.T) {
	l := Layers{Project: core.Config{MCP: core.MCPConfig{Servers: map[string]core.MCPServer{
		"a": {
			Name: "a", Source: ".mcp.json", Transport: core.MCPHTTP, URL: "https://example.com",
			OAuth: core.MCPOAuth{ClientSecret: "secret-literal"},
		},
	}}}}
	got := effectStrings(Effects(l))
	want := []string{"mcp.servers.a (from .mcp.json) → http https://example.com  oauth client_secret: (set)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effects = %q, want %q", got, want)
	}
}

func TestEffects_MCPSanitizable(t *testing.T) {
	l := Layers{Project: core.Config{MCP: core.MCPConfig{Servers: map[string]core.MCPServer{
		"a": {Name: "a", Source: ".mcp.json", Transport: core.MCPStdio, Command: "echo \x1b[31mred\x1b[0m"},
	}}}}
	effects := Effects(l)
	if len(effects) != 1 {
		t.Fatalf("got %d effects, want 1", len(effects))
	}
	if !strings.Contains(effects[0].Value, "\x1b[31m") {
		t.Errorf("Value = %q, want the raw ESC to survive", effects[0].Value)
	}
}

func TestEffects_MCPDisabled(t *testing.T) {
	l := Layers{Project: core.Config{MCP: core.MCPConfig{Disabled: []string{"b", "a"}}}}
	got := effectStrings(Effects(l))
	want := []string{"mcp.disabled → a", "mcp.disabled → b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Effects = %q, want %q", got, want)
	}
}
