// Package trust restricts an untrusted project's configuration to what it
// may safely apply (spec §8.3, R7) and describes, as Effects, what a
// project layer changes.
package trust

import (
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/agents"
	"github.com/gammons/jig/internal/service/permission"
)

// Layers holds the global and project configuration layers separately,
// before any merge: the TOML configs and the markdown agent files (agentfs
// results) of each scope.
type Layers struct {
	Global, Project     core.Config
	GlobalMD, ProjectMD map[string]core.AgentConfig
}

// Restrict returns l with Project and ProjectMD reduced to what an
// untrusted project may apply (spec §8.3 + R7), and the effects dropped
// (nil when nothing was). Global and GlobalMD pass through unchanged.
//
//   - Providers (including ImageModels), Instructions, SkillPaths, and
//     AgentBrowser are dropped whole; they could redirect credentials,
//     inject prompt files, or turn on a permission preset.
//   - DefaultModel, SmallModel, ModelAliases, Theme, and Keybinds are kept.
//   - Top-level permissions go through permission.Tighten against
//     Effective(nil, Global.Permissions). Because the top-level rules sit
//     under every agent's own rules at runtime, an ask pattern is also
//     dropped when any global agent (built-in, Global.Agents, GlobalMD)
//     has a deny for that tool: a more specific ask would out-rank it.
//   - Each project agent's permissions go through Tighten against
//     Effective(agentGlobal, Overlay(Global.Permissions, keptTopLevel)),
//     where agentGlobal is the built-in overlaid with the global TOML and
//     markdown layers; the markdown layer's baseline also includes the
//     kept TOML entries for that agent. Other agent fields are kept.
//
// Restrict never mutates l, and its Project and ProjectMD share no maps,
// slices, or pointers with l. Dropped fields are nil, never empty.
func Restrict(l Layers) (Layers, []Effect) {
	out := Layers{Global: l.Global, GlobalMD: l.GlobalMD}
	var drop Layers

	out.Project, drop.Project = restrictScalars(l.Project)
	out.Project.Permissions, drop.Project.Permissions = restrictTopLevel(l)

	mergedCfg := permission.Overlay(l.Global.Permissions, out.Project.Permissions)
	out.Project.Agents, drop.Project.Agents = restrictAgents(l, l.Project.Agents, mergedCfg, nil)
	out.ProjectMD, drop.ProjectMD = restrictAgents(l, l.ProjectMD, mergedCfg, out.Project.Agents)

	return out, Effects(drop)
}

// restrictScalars splits p's non-permission, non-agent fields into the
// ones an untrusted project keeps and the ones it drops.
func restrictScalars(p core.Config) (kept, dropped core.Config) {
	kept = core.Config{
		DefaultModel: p.DefaultModel,
		SmallModel:   p.SmallModel,
		ModelAliases: cloneStringMap(p.ModelAliases),
		Keybinds:     cloneStringMap(p.Keybinds),
		Theme:        p.Theme,
	}
	dropped = core.Config{
		Providers:    p.Providers,
		Instructions: p.Instructions,
		SkillPaths:   p.SkillPaths,
		AgentBrowser: p.AgentBrowser,
	}
	return kept, dropped
}

// restrictTopLevel tightens the project's top-level permissions against
// the global config, then drops any kept ask pattern for a tool some
// global agent denies.
func restrictTopLevel(l Layers) (kept, dropped core.PermissionRules) {
	kept, dropped = permission.Tighten(permission.Effective(nil, l.Global.Permissions), l.Project.Permissions)
	denied := agentDeniedTools(l)
	for tool, r := range kept {
		if !denied[tool] {
			continue
		}
		k, d := splitAsks(r)
		d.Default = dropped[tool].Default
		for p, a := range dropped[tool].Patterns {
			d.Patterns = putPattern(d.Patterns, p, a)
		}
		kept = setRule(kept, tool, k)
		dropped = setRule(dropped, tool, d)
	}
	return nilIfEmpty(kept), nilIfEmpty(dropped)
}

// agentDeniedTools returns the tools that any global agent's permissions
// (built-in overlaid with Global.Agents and GlobalMD) deny anything for.
func agentDeniedTools(l Layers) map[string]bool {
	names := agents.BuiltinNames()
	for n := range l.Global.Agents {
		names = append(names, n)
	}
	for n := range l.GlobalMD {
		names = append(names, n)
	}
	denied := make(map[string]bool)
	for _, n := range names {
		for tool, r := range agentGlobal(l, n) {
			if permission.HasDeny(r) {
				denied[tool] = true
			}
		}
	}
	return denied
}

// splitAsks moves r's ask patterns out of r: k is r without them, d holds
// only them.
func splitAsks(r core.Rule) (k, d core.Rule) {
	k.Default = r.Default
	for p, a := range r.Patterns {
		if a == core.Ask {
			d.Patterns = putPattern(d.Patterns, p, a)
		} else {
			k.Patterns = putPattern(k.Patterns, p, a)
		}
	}
	return k, d
}

// agentGlobal is agent name's permissions from the built-in, Global TOML,
// and Global markdown layers, in that precedence order.
func agentGlobal(l Layers, name string) core.PermissionRules {
	b, _ := agents.Builtin(name)
	return permission.Overlay(permission.Overlay(b.Permissions, l.Global.Agents[name].Permissions), l.GlobalMD[name].Permissions)
}

// restrictAgents tightens each agent's permissions in layer against its
// baseline: Effective(agentGlobal overlaid with below[name], mergedCfg).
// kept holds every agent (with non-permission fields copied); dropped
// holds only the dropped permissions, with the agent's Source.
func restrictAgents(l Layers, layer map[string]core.AgentConfig, mergedCfg core.PermissionRules, below map[string]core.AgentConfig) (kept, dropped map[string]core.AgentConfig) {
	for name, ac := range layer {
		agentPerms := permission.Overlay(agentGlobal(l, name), below[name].Permissions)
		k, d := permission.Tighten(permission.Effective(agentPerms, mergedCfg), ac.Permissions)

		c := cloneAgent(ac)
		c.Permissions = k
		if kept == nil {
			kept = make(map[string]core.AgentConfig, len(layer))
		}
		kept[name] = c

		if d != nil {
			if dropped == nil {
				dropped = make(map[string]core.AgentConfig)
			}
			dropped[name] = core.AgentConfig{Permissions: d, Source: ac.Source}
		}
	}
	return kept, dropped
}

// cloneAgent deep-copies ac's reference-typed fields except Permissions,
// which the caller replaces.
func cloneAgent(ac core.AgentConfig) core.AgentConfig {
	c := ac
	c.Permissions = nil
	if ac.Tools != nil {
		c.Tools = append([]string{}, ac.Tools...)
	}
	if ac.CanSpawn != nil {
		v := *ac.CanSpawn
		c.CanSpawn = &v
	}
	if ac.Hidden != nil {
		v := *ac.Hidden
		c.Hidden = &v
	}
	return c
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func putPattern(m map[string]core.Action, p string, a core.Action) map[string]core.Action {
	if m == nil {
		m = make(map[string]core.Action)
	}
	m[p] = a
	return m
}

// setRule stores r under tool, or deletes tool when r is empty.
func setRule(rules core.PermissionRules, tool string, r core.Rule) core.PermissionRules {
	if r.Default == "" && len(r.Patterns) == 0 {
		delete(rules, tool)
		return rules
	}
	if rules == nil {
		rules = make(core.PermissionRules)
	}
	rules[tool] = r
	return rules
}

func nilIfEmpty(rules core.PermissionRules) core.PermissionRules {
	if len(rules) == 0 {
		return nil
	}
	return rules
}
