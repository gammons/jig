package config

import (
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/data/paths"
)

// state accumulates one config.Load pass across files, in merge order
// (later files win). See the package doc and Task 8's brief for the merge
// rules per key.
type state struct {
	cfg           core.Config
	globalAgents  map[string]core.AgentConfig
	projectAgents map[string]core.AgentConfig
}

// apply merges one decoded, substituted file into s. file is its path (used
// as AgentConfig.Source); dir is its directory (used to resolve relative
// instructions/skill paths); isGlobal marks the single global config file,
// as opposed to a project (workDir/.jig/config.toml) file.
func (s *state) apply(dto tomlFile, md toml.MetaData, file, dir, home string, isGlobal bool) {
	s.applyScalars(dto, md)
	s.applyProviders(dto, md)
	mergePermissions(&s.cfg.Permissions, dto.Permissions)
	mergeStringMap(&s.cfg.ModelAliases, dto.ModelAliases)
	mergeStringMap(&s.cfg.Keybinds, dto.Keybinds)
	appendDedup(&s.cfg.Instructions, dto.Instructions, dir, home)
	appendDedup(&s.cfg.SkillPaths, dto.Skills.Paths, dir, home)

	if isGlobal {
		s.globalAgents = applyAgents(s.globalAgents, dto.Agents, md, file)
	} else {
		s.projectAgents = applyAgents(s.projectAgents, dto.Agents, md, file)
	}
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
// src's rule sets one, and Patterns are merged key by key.
func mergePermissions(dst *core.PermissionRules, src map[string]core.Rule) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(core.PermissionRules, len(src))
	}
	for tool, rule := range src {
		existing := (*dst)[tool]
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
		(*dst)[tool] = existing
	}
}

// mergeStringMap merges src into *dst, key by key, later files winning per
// key.
func mergeStringMap(dst *map[string]string, src map[string]string) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v
	}
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
