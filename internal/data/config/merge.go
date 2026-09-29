package config

import (
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/paths"
)

// state accumulates one config.Load pass across the files of a single
// layer (either just the global file, or the project files root-to-leaf),
// in merge order (later files win). See the package doc and Task 8's
// brief for the merge rules per key.
type state struct {
	cfg core.Config
}

// apply merges one decoded, substituted file into s. file is its path (used
// as AgentConfig.Source); dir is its directory (used to resolve relative
// instructions/skill paths).
func (s *state) apply(dto tomlFile, md toml.MetaData, file, dir, home string) {
	s.applyScalars(dto, md)
	s.applyProviders(dto, md)
	mergePermissions(&s.cfg.Permissions, dto.Permissions)
	mergeStringMap(&s.cfg.ModelAliases, dto.ModelAliases)
	mergeStringMap(&s.cfg.Keybinds, dto.Keybinds)
	appendDedup(&s.cfg.Instructions, dto.Instructions, dir, home)
	appendDedup(&s.cfg.SkillPaths, dto.Skills.Paths, dir, home)
	s.cfg.Agents = applyAgents(s.cfg.Agents, dto.Agents, md, file)
}

func (s *state) applyScalars(dto tomlFile, md toml.MetaData) {
	if md.IsDefined("default_model") {
		s.cfg.DefaultModel = dto.DefaultModel
	}
	if md.IsDefined("small_model") {
		s.cfg.SmallModel = dto.SmallModel
	}
	if md.IsDefined("theme") {
		s.cfg.Theme = dto.Theme
	}
	if md.IsDefined("integrations", "agent_browser", "enabled") {
		s.cfg.AgentBrowser = dto.Integrations.AgentBrowser.Enabled
	}
}

func (s *state) applyProviders(dto tomlFile, md toml.MetaData) {
	if len(dto.Providers) == 0 {
		return
	}
	if s.cfg.Providers == nil {
		s.cfg.Providers = make(map[string]core.ProviderConfig, len(dto.Providers))
	}
	for name, p := range dto.Providers {
		dst := s.cfg.Providers[name]
		mergeProviderFields(&dst, name, p, md)
		s.cfg.Providers[name] = dst
	}
}

func mergeProviderFields(dst *core.ProviderConfig, name string, p providerDTO, md toml.MetaData) {
	if md.IsDefined("providers", name, "type") {
		dst.Type = p.Type
	}
	if md.IsDefined("providers", name, "api_key") {
		dst.APIKey = p.APIKey
	}
	if md.IsDefined("providers", name, "base_url") {
		dst.BaseURL = p.BaseURL
	}
	if md.IsDefined("providers", name, "models") {
		dst.Models = p.Models
	}
	if md.IsDefined("providers", name, "options") {
		dst.Options = p.Options
	}
	if md.IsDefined("providers", name, "image_models") {
		dst.ImageModels = p.ImageModels
	}
}

// applyAgents merges dtoAgents into dst, field by field (mergeAgentFields),
// allocating dst if needed. It returns the (possibly newly allocated) map so
// callers with a nil map can capture the result.
func applyAgents(dst map[string]core.AgentConfig, dtoAgents map[string]agentDTO, md toml.MetaData, file string) map[string]core.AgentConfig {
	if len(dtoAgents) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]core.AgentConfig, len(dtoAgents))
	}
	for name, a := range dtoAgents {
		existing := dst[name]
		if mergeAgentFields(&existing, name, a, md) {
			existing.Source = file
		}
		dst[name] = existing
	}
	return dst
}

// mergeAgentFields overlays the fields a set in the TOML file onto dst,
// leaving fields it did not set untouched. It reports whether it touched
// anything, so the caller can update AgentConfig.Source accordingly.
func mergeAgentFields(dst *core.AgentConfig, name string, a agentDTO, md toml.MetaData) bool {
	touched := false
	if md.IsDefined("agents", name, "description") {
		dst.Description = a.Description
		touched = true
	}
	if md.IsDefined("agents", name, "mode") {
		dst.Mode = a.Mode
		touched = true
	}
	if md.IsDefined("agents", name, "model") {
		dst.Model = a.Model
		touched = true
	}
	if md.IsDefined("agents", name, "prompt") {
		dst.Prompt = a.Prompt
		touched = true
	}
	if md.IsDefined("agents", name, "max_steps") {
		dst.MaxSteps = a.MaxSteps
		touched = true
	}
	if a.CanSpawn != nil {
		dst.CanSpawn = a.CanSpawn
		touched = true
	}
	if a.Hidden != nil {
		dst.Hidden = a.Hidden
		touched = true
	}
	if md.IsDefined("agents", name, "tools") {
		dst.Tools = a.Tools
		touched = true
	}
	if len(a.Permissions) > 0 {
		mergePermissions(&dst.Permissions, a.Permissions)
		touched = true
	}
	return touched
}

// mergePermissions merges src into *dst per tool: Default is replaced if
// src's rule sets one, and Patterns are merged key by key. It always
// allocates a fresh map (and per-tool Patterns map) rather than mutating
// *dst's backing storage in place, so it is safe to call with a *dst that
// aliases a caller-owned map, as Merge's lo does.
func mergePermissions(dst *core.PermissionRules, src map[string]core.Rule) {
	if len(src) == 0 {
		return
	}
	out := make(core.PermissionRules, len(*dst)+len(src))
	for tool, rule := range *dst {
		out[tool] = cloneRule(rule)
	}
	for tool, rule := range src {
		existing := out[tool]
		if rule.Default != "" {
			existing.Default = rule.Default
		}
		if len(rule.Patterns) > 0 {
			if existing.Patterns == nil {
				existing.Patterns = make(map[string]core.Action, len(rule.Patterns))
			}
			for pattern, action := range rule.Patterns {
				existing.Patterns[pattern] = action
			}
		}
		out[tool] = existing
	}
	*dst = out
}

func cloneRule(r core.Rule) core.Rule {
	if r.Patterns == nil {
		return r
	}
	cp := make(map[string]core.Action, len(r.Patterns))
	for k, v := range r.Patterns {
		cp[k] = v
	}
	return core.Rule{Default: r.Default, Patterns: cp}
}

// mergeStringMap merges src into *dst, key by key, later files winning per
// key. Like mergePermissions, it always allocates a fresh map rather than
// mutating *dst's backing storage in place.
func mergeStringMap(dst *map[string]string, src map[string]string) {
	if len(src) == 0 {
		return
	}
	out := make(map[string]string, len(*dst)+len(src))
	for k, v := range *dst {
		out[k] = v
	}
	for k, v := range src {
		out[k] = v
	}
	*dst = out
}

// appendDedup resolves each item relative to dir (with "~" expanded to
// home), then appends it to *dst unless it is already present.
func appendDedup(dst *[]string, items []string, dir, home string) {
	for _, item := range items {
		abs := paths.ExpandHome(item, home)
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		if !containsString(*dst, abs) {
			*dst = append(*dst, abs)
		}
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// Merge overlays hi on lo with Plan 1's per-key rules, reusing the same
// field helpers as the per-file fold above (mergePermissions,
// mergeStringMap) so the two cannot drift. It operates on already-decoded
// core.Config values, where "hi sets this field" is approximated by
// non-empty/non-nil in hi (empty string, nil slice, nil map, or nil *bool
// mean unset) -- unlike the fold, which asks the TOML decoder's
// toml.MetaData.IsDefined.
func Merge(lo, hi core.Config) core.Config {
	out := lo
	mergeConfigScalars(&out, hi)
	mergeProvidersMap(&out.Providers, hi.Providers)
	mergePermissions(&out.Permissions, hi.Permissions)
	mergeStringMap(&out.ModelAliases, hi.ModelAliases)
	mergeStringMap(&out.Keybinds, hi.Keybinds)
	out.Instructions = mergeAppendDedup(lo.Instructions, hi.Instructions)
	out.SkillPaths = mergeAppendDedup(lo.SkillPaths, hi.SkillPaths)
	mergeAgentsMap(&out.Agents, hi.Agents)
	out.MCP = mergeMCPConfig(lo.MCP, hi.MCP)
	return out
}

// mergeConfigScalars is Merge's counterpart of state.applyScalars: it
// overlays hi's scalar fields onto dst when hi sets them, non-empty in hi
// standing in for toml.MetaData.IsDefined.
func mergeConfigScalars(dst *core.Config, hi core.Config) {
	if hi.DefaultModel != "" {
		dst.DefaultModel = hi.DefaultModel
	}
	if hi.SmallModel != "" {
		dst.SmallModel = hi.SmallModel
	}
	if hi.Theme != "" {
		dst.Theme = hi.Theme
	}
	if hi.AgentBrowser != "" {
		dst.AgentBrowser = hi.AgentBrowser
	}
}

// mergeProvidersMap is Merge's counterpart of state.applyProviders: it
// overlays hi's providers onto *dst field by field (mergeProviderConfigFields),
// without mutating *dst's original backing map (which may alias lo's).
func mergeProvidersMap(dst *map[string]core.ProviderConfig, hi map[string]core.ProviderConfig) {
	if len(hi) == 0 {
		return
	}
	out := make(map[string]core.ProviderConfig, len(*dst)+len(hi))
	for name, p := range *dst {
		out[name] = p
	}
	for name, p := range hi {
		existing := out[name]
		mergeProviderConfigFields(&existing, p)
		out[name] = existing
	}
	*dst = out
}

// mergeProviderConfigFields is Merge's counterpart of mergeProviderFields:
// the same field list, but reading "hi sets this field" straight off the
// already-decoded core.ProviderConfig rather than toml.MetaData.
func mergeProviderConfigFields(dst *core.ProviderConfig, hi core.ProviderConfig) {
	if hi.Type != "" {
		dst.Type = hi.Type
	}
	if hi.APIKey != "" {
		dst.APIKey = hi.APIKey
	}
	if hi.BaseURL != "" {
		dst.BaseURL = hi.BaseURL
	}
	if hi.Models != nil {
		dst.Models = hi.Models
	}
	if hi.Options != nil {
		dst.Options = hi.Options
	}
	if hi.ImageModels != nil {
		dst.ImageModels = hi.ImageModels
	}
}

// mergeAgentsMap is Merge's counterpart of applyAgents: it overlays hi's
// agents onto *dst field by field (mergeAgentConfigFields), without
// mutating *dst's original backing map (which may alias lo's).
func mergeAgentsMap(dst *map[string]core.AgentConfig, hi map[string]core.AgentConfig) {
	if len(hi) == 0 {
		return
	}
	out := make(map[string]core.AgentConfig, len(*dst)+len(hi))
	for name, a := range *dst {
		out[name] = a
	}
	for name, a := range hi {
		existing := out[name]
		if mergeAgentConfigFields(&existing, a) {
			existing.Source = a.Source
		}
		out[name] = existing
	}
	*dst = out
}

// mergeAgentConfigFields is Merge's counterpart of mergeAgentFields: the
// same field list, but reading "hi sets this field" straight off the
// already-decoded core.AgentConfig rather than toml.MetaData. It reports
// whether it touched anything, so the caller can update Source.
func mergeAgentConfigFields(dst *core.AgentConfig, hi core.AgentConfig) bool {
	touched := false
	if hi.Description != "" {
		dst.Description = hi.Description
		touched = true
	}
	if hi.Mode != "" {
		dst.Mode = hi.Mode
		touched = true
	}
	if hi.Model != "" {
		dst.Model = hi.Model
		touched = true
	}
	if hi.Prompt != "" {
		dst.Prompt = hi.Prompt
		touched = true
	}
	if hi.MaxSteps != 0 {
		dst.MaxSteps = hi.MaxSteps
		touched = true
	}
	if hi.CanSpawn != nil {
		dst.CanSpawn = hi.CanSpawn
		touched = true
	}
	if hi.Hidden != nil {
		dst.Hidden = hi.Hidden
		touched = true
	}
	if hi.Tools != nil {
		dst.Tools = hi.Tools
		touched = true
	}
	if len(hi.Permissions) > 0 {
		mergePermissions(&dst.Permissions, hi.Permissions)
		touched = true
	}
	return touched
}

// mergeAppendDedup is Merge's counterpart of appendDedup: lo and hi are
// already-resolved absolute paths (each resolved, at fold time, relative
// to its declaring file's directory), so it only needs to append and
// de-duplicate, not resolve.
func mergeAppendDedup(lo, hi []string) []string {
	out := append([]string(nil), lo...)
	for _, item := range hi {
		if !containsString(out, item) {
			out = append(out, item)
		}
	}
	return out
}
