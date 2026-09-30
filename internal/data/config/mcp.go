package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/paths"
)

// mcpNamePattern matches a valid MCP server name (spec §5.1). Compiled per
// call, not held in a package var, per the repo's no-package-mutable-vars
// rule (see subst.go's tokenPattern for the same convention).
func mcpNamePattern() *regexp.Regexp {
	return regexp.MustCompile(`^[a-z0-9_-]+$`)
}

// validateMCPName errors, naming path, if name does not match
// mcpNamePattern.
func validateMCPName(name, path string) error {
	if !mcpNamePattern().MatchString(name) {
		return fmt.Errorf("config: %s: mcp server %q: invalid name, must match ^[a-z0-9_-]+$", path, name)
	}
	return nil
}

// mcpTransportTypes maps a TOML/.mcp.json "type" value to its transport,
// for servers whose transport is inferred from "url" rather than
// "command". allowStreamableHTTP additionally accepts "streamable-http"
// as an alias for http, which some .mcp.json files use even though it is
// not part of Claude Code's documented format.
func mcpTransportTypes(allowStreamableHTTP bool) map[string]core.MCPTransport {
	m := map[string]core.MCPTransport{
		"http": core.MCPHTTP,
		"sse":  core.MCPSSE,
	}
	if allowStreamableHTTP {
		m["streamable-http"] = core.MCPHTTP
	}
	return m
}

// inferMCPTransport applies §5.1/§5.2's transport inference: command means
// stdio, url means http (or whatever types maps its explicit "type" to).
// Setting both command and url, or neither, is an error naming path.
func inferMCPTransport(path, name, typ, command, url string, types map[string]core.MCPTransport) (core.MCPTransport, error) {
	if command != "" && url != "" {
		return "", fmt.Errorf("config: %s: mcp server %q: both command and url are set", path, name)
	}
	if command == "" && url == "" {
		return "", fmt.Errorf("config: %s: mcp server %q: neither command nor url is set", path, name)
	}
	if command != "" {
		return core.MCPStdio, nil
	}
	if typ == "" {
		return core.MCPHTTP, nil
	}
	if t, ok := types[typ]; ok {
		return t, nil
	}
	return "", fmt.Errorf("config: %s: mcp server %q: unknown type %q", path, name, typ)
}

// parseMCPDuration parses raw with time.ParseDuration, naming path, name,
// and field on error. An empty raw parses as the zero Duration: the
// Manager applies its own defaults (10s/2m) later, not this package.
func parseMCPDuration(raw, path, name, field string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s: mcp server %q: invalid %s %q: %w", path, name, field, raw, err)
	}
	return d, nil
}

// resolveMCPCwd applies §5.2's default-cwd rule. For a project server, an
// empty cwd becomes baseDir (the defining directory), and a relative cwd
// resolves against baseDir too. For a global-file server, an empty cwd
// stays "" -- the Manager fills in the workdir later -- and a relative
// cwd is left exactly as written, since the Manager joins it with the
// workdir at that point, not here.
func resolveMCPCwd(raw, baseDir, home string, isProject bool) string {
	if raw == "" {
		if isProject {
			return baseDir
		}
		return ""
	}
	expanded := paths.ExpandHome(raw, home)
	if filepath.IsAbs(expanded) {
		return expanded
	}
	if isProject {
		return filepath.Join(baseDir, expanded)
	}
	return expanded
}

// mcpTOMLServerKeys lists every [mcp.servers.<name>] key besides "enabled",
// used by isToggleOnlyTOML to detect an enable-only entry.
func mcpTOMLServerKeys() []string {
	return []string{"type", "command", "args", "env", "cwd", "url", "headers", "startup_timeout", "tool_timeout", "oauth"}
}

// isToggleOnlyTOML reports whether [mcp.servers.<name>] defines only
// "enabled" (R2): true only if "enabled" is defined and no other known key
// is.
func isToggleOnlyTOML(md toml.MetaData, name string) bool {
	if !md.IsDefined("mcp", "servers", name, "enabled") {
		return false
	}
	for _, k := range mcpTOMLServerKeys() {
		if md.IsDefined("mcp", "servers", name, k) {
			return false
		}
	}
	return true
}

// mcpServerFromTOML converts one [mcp.servers.<name>] table into a
// core.MCPServer. toggleOnly reports whether it is an enable-only entry
// (R2): if so, only Name, Source, and Enabled are set and validation of
// transport/command/url is skipped. defDir is the directory a relative
// cwd resolves against, and the default when cwd is unset for a project
// server (resolveMCPCwd); isProject selects between the project and
// global cwd rules.
func mcpServerFromTOML(name string, dto mcpServerDTO, md toml.MetaData, path, defDir string, isProject bool, home string) (core.MCPServer, bool, error) {
	if err := validateMCPName(name, path); err != nil {
		return core.MCPServer{}, false, err
	}
	if isToggleOnlyTOML(md, name) {
		b := *dto.Enabled
		return core.MCPServer{Name: name, Source: path, Enabled: &b}, true, nil
	}

	transport, err := inferMCPTransport(path, name, dto.Type, dto.Command, dto.URL, mcpTransportTypes(false))
	if err != nil {
		return core.MCPServer{}, false, err
	}
	startup, err := parseMCPDuration(dto.StartupTimeout, path, name, "startup_timeout")
	if err != nil {
		return core.MCPServer{}, false, err
	}
	toolTimeout, err := parseMCPDuration(dto.ToolTimeout, path, name, "tool_timeout")
	if err != nil {
		return core.MCPServer{}, false, err
	}

	return core.MCPServer{
		Name:      name,
		Source:    path,
		Transport: transport,
		Command:   dto.Command,
		Args:      dto.Args,
		Env:       dto.Env,
		Cwd:       resolveMCPCwd(dto.Cwd, defDir, home, isProject),
		URL:       dto.URL,
		Headers:   dto.Headers,
		OAuth: core.MCPOAuth{
			ClientID:     dto.OAuth.ClientID,
			ClientSecret: dto.OAuth.ClientSecret,
			Scopes:       dto.OAuth.Scopes,
		},
		StartupTimeout: startup,
		ToolTimeout:    toolTimeout,
		Enabled:        dto.Enabled,
	}, false, nil
}

// applyMCP folds one TOML file's [mcp] table into s.cfg.MCP: each server
// entry is merged with mergeMCPServer, and Disabled is unioned (§5.2: "the
// union of all layers"). dir is the fold's directory as loadLayer computes
// it (the file's own directory, i.e. ".../.jig" for a project file);
// isProject selects the defining directory used for a relative/empty cwd
// (its parent, for a project file).
func (s *state) applyMCP(dto tomlFile, md toml.MetaData, file, dir, home string, isProject bool) error {
	defDir := dir
	if isProject {
		defDir = filepath.Dir(dir)
	}
	for name, entry := range dto.MCP.Servers {
		server, toggleOnly, err := mcpServerFromTOML(name, entry, md, file, defDir, isProject, home)
		if err != nil {
			return err
		}
		s.cfg.MCP.Servers = mergeMCPServer(s.cfg.MCP.Servers, name, server, toggleOnly)
	}
	s.cfg.MCP.Disabled = unionStrings(s.cfg.MCP.Disabled, dto.MCP.Disabled)
	return nil
}

// mergeMCPServer applies §5.2's per-server merge rule, shared by the
// per-file fold (state.applyMCP, per-layer .mcp.json handling) and
// config.Merge across layers: a full entry (toggleOnly false) replaces any
// existing same-named entry as a whole. A toggle-only entry (only
// "enabled" set, R2) sets only Enabled when dst already holds a full entry
// (Transport != ""); otherwise it is kept in dst as-is (replacing any
// previous toggle-only entry), since a later merge may still apply it to a
// fuller entry.
func mergeMCPServer(dst map[string]core.MCPServer, name string, entry core.MCPServer, toggleOnly bool) map[string]core.MCPServer {
	if dst == nil {
		dst = make(map[string]core.MCPServer)
	}
	if !toggleOnly {
		dst[name] = entry
		return dst
	}
	if existing, ok := dst[name]; ok && existing.Transport != "" {
		existing.Enabled = entry.Enabled
		dst[name] = existing
		return dst
	}
	dst[name] = entry
	return dst
}

// unionStrings appends every item of add not already in dst, preserving
// dst's existing order.
func unionStrings(dst, add []string) []string {
	for _, s := range add {
		if !containsString(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}

// mergeMCPConfig is Merge's counterpart of applyMCP: it overlays hi's MCP
// config onto lo per §5.2, without mutating lo's backing maps.
func mergeMCPConfig(lo, hi core.MCPConfig) core.MCPConfig {
	out := core.MCPConfig{
		Disabled: unionStrings(append([]string(nil), lo.Disabled...), hi.Disabled),
	}
	if len(lo.Servers) > 0 || len(hi.Servers) > 0 {
		out.Servers = make(map[string]core.MCPServer, len(lo.Servers)+len(hi.Servers))
		for name, s := range lo.Servers {
			out.Servers[name] = s
		}
		for name, entry := range hi.Servers {
			out.Servers = mergeMCPServer(out.Servers, name, entry, entry.Transport == "")
		}
	}
	return out
}

// ResolvedMCP returns c's servers ready for use: toggle-only entries
// (Transport == "", R2), servers with Enabled explicitly false, and
// servers named in c.Disabled are dropped. The rest are sorted by name.
func ResolvedMCP(c core.MCPConfig) []core.MCPServer {
	disabled := make(map[string]bool, len(c.Disabled))
	for _, name := range c.Disabled {
		disabled[name] = true
	}
	out := make([]core.MCPServer, 0, len(c.Servers))
	for name, s := range c.Servers {
		if s.Transport == "" {
			continue
		}
		if s.Enabled != nil && !*s.Enabled {
			continue
		}
		if disabled[name] {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
