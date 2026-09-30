package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// mcpJSONFileName is the file name Claude Code's .mcp.json format uses,
// looked for directly in each project directory (never in the global
// config dir).
const mcpJSONFileName = ".mcp.json"

// jsonKnownServerFields lists the .mcp.json server fields jig understands
// (spec §5.2); anything else produces a warning. "enabled" is
// deliberately absent: it is not part of Claude Code's format, so a full
// entry that sets it gets the same "unknown field" warning as any other
// stray key (isToggleOnlyJSON handles the one exception, an entry of
// exactly {"enabled": true|false}).
func jsonKnownServerFields() map[string]bool {
	return map[string]bool{
		"command": true, "args": true, "env": true,
		"type": true, "url": true, "headers": true,
	}
}

// applyMCPJSON reads and folds one .mcp.json file into s.cfg.MCP, per
// §5.2's format and merge rules. It returns ok=false (with a nil error) if
// path does not exist. subst gates "${VAR}"/"${VAR:-default}" expansion,
// exactly like decodeFile's subst parameter for "{env:}"/"{file:}".
func (s *state) applyMCPJSON(path string, subst bool, getenv func(string) string) (warnings []string, ok bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, false, fmt.Errorf("config: %s: %w", path, err)
	}

	var warns []string
	warns = append(warns, unknownTopLevelWarnings(top, path)...)

	var serversRaw map[string]json.RawMessage
	if m, ok := top["mcpServers"]; ok {
		if err := json.Unmarshal(m, &serversRaw); err != nil {
			return nil, false, fmt.Errorf("config: %s: mcpServers: %w", path, err)
		}
	}

	names := make([]string, 0, len(serversRaw))
	for name := range serversRaw {
		names = append(names, name)
	}
	sort.Strings(names)

	dir := filepath.Dir(path)
	for _, name := range names {
		server, toggleOnly, w, err := mcpServerFromJSON(name, serversRaw[name], path, dir, subst, getenv)
		if err != nil {
			return nil, false, err
		}
		warns = append(warns, w...)
		s.cfg.MCP.Servers = mergeMCPServer(s.cfg.MCP.Servers, name, server, toggleOnly)
	}
	return warns, true, nil
}

// unknownTopLevelWarnings warns (sorted, for deterministic output) about
// every key of top other than "mcpServers".
func unknownTopLevelWarnings(top map[string]json.RawMessage, path string) []string {
	var unknown []string
	for k := range top {
		if k != "mcpServers" {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	warns := make([]string, 0, len(unknown))
	for _, k := range unknown {
		warns = append(warns, fmt.Sprintf(".mcp.json %s: unknown field %q ignored", path, k))
	}
	return warns
}

// isToggleOnlyJSON reports whether generic is exactly {"enabled": <bool>}
// (R2's exception to Claude Code's format, needed so a .mcp.json layer can
// disable an inherited server).
func isToggleOnlyJSON(generic map[string]json.RawMessage) bool {
	if len(generic) != 1 {
		return false
	}
	raw, ok := generic["enabled"]
	if !ok {
		return false
	}
	var b bool
	return json.Unmarshal(raw, &b) == nil
}

// mcpServerFromJSON converts one mcpServers.<name> entry into a
// core.MCPServer. dir is the .mcp.json's own directory, used as the
// server's Cwd (there is no cwd field in Claude Code's format) and as the
// base for §5.2's default-cwd rule.
func mcpServerFromJSON(name string, raw json.RawMessage, path, dir string, subst bool, getenv func(string) string) (core.MCPServer, bool, []string, error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return core.MCPServer{}, false, nil, fmt.Errorf("config: %s: server %q: %w", path, name, err)
	}
	if err := validateMCPName(name, path); err != nil {
		return core.MCPServer{}, false, nil, err
	}
	if isToggleOnlyJSON(generic) {
		var b bool
		_ = json.Unmarshal(generic["enabled"], &b)
		return core.MCPServer{Name: name, Source: path, Enabled: &b}, true, nil, nil
	}

	known := jsonKnownServerFields()
	var unknown []string
	for k := range generic {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	warns := make([]string, 0, len(unknown))
	for _, f := range unknown {
		warns = append(warns, fmt.Sprintf(".mcp.json %s: server %q: unknown field %q ignored", path, name, f))
	}

	var command, url, typ string
	var args []string
	var env, headers map[string]string
	unmarshalJSONField(generic, "command", &command)
	unmarshalJSONField(generic, "url", &url)
	unmarshalJSONField(generic, "type", &typ)
	unmarshalJSONField(generic, "args", &args)
	unmarshalJSONField(generic, "env", &env)
	unmarshalJSONField(generic, "headers", &headers)

	transport, err := inferMCPTransport(path, name, typ, command, url, mcpTransportTypes(true))
	if err != nil {
		return core.MCPServer{}, false, nil, err
	}

	if subst {
		var w []string
		command, w = expandVar(command, getenv, path, name)
		warns = append(warns, w...)
		for i := range args {
			args[i], w = expandVar(args[i], getenv, path, name)
			warns = append(warns, w...)
		}
		url, w = expandVar(url, getenv, path, name)
		warns = append(warns, w...)
		env = expandStringMap(env, getenv, path, name, &warns)
		headers = expandStringMap(headers, getenv, path, name, &warns)
	}

	return core.MCPServer{
		Name:      name,
		Source:    path,
		Transport: transport,
		Command:   command,
		Args:      args,
		Env:       env,
		Cwd:       dir,
		URL:       url,
		Headers:   headers,
	}, false, warns, nil
}

// unmarshalJSONField decodes generic[key] into dst if present, ignoring a
// malformed value (dst is simply left at its zero value): jig treats
// .mcp.json leniently, the same way toml.Decode leaves an unset field at
// its zero value.
func unmarshalJSONField(generic map[string]json.RawMessage, key string, dst any) {
	if raw, ok := generic[key]; ok {
		_ = json.Unmarshal(raw, dst)
	}
}

// varPattern matches "${VAR}" and "${VAR:-default}" substitution tokens.
// Compiled per call, not held in a package var, per subst.go's
// tokenPattern convention.
func varPattern() *regexp.Regexp {
	return regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)
}

// expandVar replaces every "${VAR}"/"${VAR:-default}" token in s. An unset
// (or empty) VAR with no default becomes "", and is reported as a warning
// naming path and server name; one with a default silently uses it.
func expandVar(s string, getenv func(string) string, path, name string) (string, []string) {
	matches := varPattern().FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s, nil
	}
	var warns []string
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(s[last:m[0]])
		varName := s[m[2]:m[3]]
		val := getenv(varName)
		if val == "" {
			if m[4] != -1 {
				val = s[m[6]:m[7]]
			} else {
				warns = append(warns, fmt.Sprintf(".mcp.json %s: server %q: ${%s} is not set", path, name, varName))
			}
		}
		b.WriteString(val)
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String(), warns
}

// expandStringMap applies expandVar to every value of m, visiting keys in
// sorted order so the warnings it appends to *warns stay deterministic. A
// nil m returns nil.
func expandStringMap(m map[string]string, getenv func(string) string, path, name string, warns *[]string) map[string]string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(m))
	for _, k := range keys {
		v, w := expandVar(m[k], getenv, path, name)
		out[k] = v
		*warns = append(*warns, w...)
	}
	return out
}
