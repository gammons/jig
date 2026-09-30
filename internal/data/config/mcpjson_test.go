package config

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestExpandVar_DefaultsAndWarnings(t *testing.T) {
	getenv := fakeGetenv(map[string]string{"FOO": "bar"})

	got, warns := expandVar("x=${FOO} y=${MISSING:-def} z=${MISSING}", getenv, "/p/.mcp.json", "srv")
	if want := "x=bar y=def z="; got != want {
		t.Errorf("expandVar = %q, want %q", got, want)
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want exactly 1", warns)
	}
	if warns[0] != `.mcp.json /p/.mcp.json: server "srv": ${MISSING} is not set` {
		t.Errorf("warns[0] = %q", warns[0])
	}
}

func TestExpandVar_NoTokensUnchanged(t *testing.T) {
	got, warns := expandVar("plain string", fakeGetenv(nil), "/p", "s")
	if got != "plain string" {
		t.Errorf("got = %q", got)
	}
	if warns != nil {
		t.Errorf("warns = %v, want nil", warns)
	}
}

func TestIsToggleOnlyJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"enabled false only", `{"enabled": false}`, true},
		{"enabled true only", `{"enabled": true}`, true},
		{"enabled plus command", `{"enabled": false, "command": "x"}`, false},
		{"command only", `{"command": "x"}`, false},
		{"enabled non-bool", `{"enabled": "false"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var generic map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tt.raw), &generic); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got := isToggleOnlyJSON(generic); got != tt.want {
				t.Errorf("isToggleOnlyJSON(%s) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMCPServerFromJSON_UnknownFieldWarns(t *testing.T) {
	raw := json.RawMessage(`{"command": "npx", "disabled": true, "cwd": "/tmp"}`)
	server, toggleOnly, warns, err := mcpServerFromJSON("srv", raw, "/p/.mcp.json", "/p", false, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("mcpServerFromJSON: %v", err)
	}
	if toggleOnly {
		t.Error("toggleOnly = true, want false")
	}
	if server.Command != "npx" {
		t.Errorf("Command = %q", server.Command)
	}
	if len(warns) != 2 {
		t.Fatalf("warns = %v, want 2 (disabled, cwd)", warns)
	}
}

func TestMCPServerFromJSON_StreamableHTTPAccepted(t *testing.T) {
	raw := json.RawMessage(`{"type": "streamable-http", "url": "https://example.com/mcp"}`)
	server, _, warns, err := mcpServerFromJSON("srv", raw, "/p/.mcp.json", "/p", false, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("mcpServerFromJSON: %v", err)
	}
	if server.Transport != "http" {
		t.Errorf("Transport = %q, want http", server.Transport)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestApplyMCPJSON_UnknownTopLevelKeyWarns(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.mcp.json"
	writeConfigFile(t, path, `{"mcpServers": {}, "otherKey": true}`)

	var s state
	warns, ok, err := s.applyMCPJSON(path, false, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("applyMCPJSON: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want 1", warns)
	}
	if warns[0] != `.mcp.json `+path+`: unknown field "otherKey" ignored` {
		t.Errorf("warns[0] = %q", warns[0])
	}
}

func TestApplyMCPJSON_MissingFileIsOKFalse(t *testing.T) {
	var s state
	warns, ok, err := s.applyMCPJSON("/does/not/exist/.mcp.json", false, fakeGetenv(nil))
	if err != nil {
		t.Fatalf("applyMCPJSON: %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
	if warns != nil {
		t.Errorf("warns = %v, want nil", warns)
	}
}

func TestMergeMCPServer_ToggleOnlyAppliesToFullEntry(t *testing.T) {
	b := false
	dst := map[string]core.MCPServer{
		"a": {Name: "a", Transport: core.MCPStdio, Command: "cmd"},
	}
	dst = mergeMCPServer(dst, "a", core.MCPServer{Name: "a", Enabled: &b}, true)
	got := dst["a"]
	if got.Command != "cmd" || got.Transport != core.MCPStdio {
		t.Errorf("full entry replaced: %+v", got)
	}
	if got.Enabled == nil || *got.Enabled {
		t.Errorf("Enabled = %v, want pointer to false", got.Enabled)
	}
}

func TestMergeMCPServer_ToggleOnlySurvivesWithNoBase(t *testing.T) {
	b := false
	dst := mergeMCPServer(nil, "a", core.MCPServer{Name: "a", Enabled: &b}, true)
	got, ok := dst["a"]
	if !ok {
		t.Fatal("a missing")
	}
	if got.Transport != "" {
		t.Errorf("Transport = %q, want empty (toggle-only kept as-is)", got.Transport)
	}
	if !reflect.DeepEqual(got.Enabled, &b) {
		t.Errorf("Enabled = %v, want pointer to false", got.Enabled)
	}
}
