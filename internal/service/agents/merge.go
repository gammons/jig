package agents

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// Sources holds the four layers of agent configuration that overlay the
// built-ins, in increasing precedence order.
type Sources struct {
	GlobalTOML  map[string]core.AgentConfig
	GlobalMD    map[string]core.AgentConfig
	ProjectTOML map[string]core.AgentConfig
	ProjectMD   map[string]core.AgentConfig
}

// Service resolves the final set of agents for a project: built-ins
// overlaid with config and markdown sources, ready for Get/Primary/
// Subagents/ResolveModel/SmallModel.
type Service struct {
	agents map[string]core.Agent
	cfg    core.Config
}

// New merges built-in agents with src's four layers, in precedence order
// built-in < GlobalTOML < GlobalMD < ProjectTOML < ProjectMD, field by
// field. It returns an error if any layer sets an invalid mode, an
// unparsable model reference, or a model alias that isn't defined in
// cfg.ModelAliases.
func New(cfg core.Config, src Sources) (*Service, error) {
	merged := builtins()
	layers := []map[string]core.AgentConfig{src.GlobalTOML, src.GlobalMD, src.ProjectTOML, src.ProjectMD}
	for _, layer := range layers {
		if err := applyLayer(merged, layer, cfg); err != nil {
			return nil, err
		}
	}
	return &Service{agents: merged, cfg: cfg}, nil
}

// applyLayer merges every entry of layer into merged, in a deterministic
// (sorted by name) order.
func applyLayer(merged map[string]core.Agent, layer map[string]core.AgentConfig, cfg core.Config) error {
	names := make([]string, 0, len(layer))
	for name := range layer {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		base, existed := merged[name]
		if !existed {
			base = core.Agent{Name: name, Mode: core.ModeAll}
		}
		out, err := mergeAgent(base, layer[name], cfg)
		if err != nil {
			return err
		}
		merged[name] = out
	}
	return nil
}

// mergeAgent overlays ac onto base, field by field: strings and ints
// override when non-zero, *bool fields override when non-nil, Tools
// overrides when non-nil, and Permissions merges per tool.
func mergeAgent(base core.Agent, ac core.AgentConfig, cfg core.Config) (core.Agent, error) {
	out := base

	if ac.Description != "" {
		out.Description = ac.Description
	}
	if ac.Mode != "" {
		mode, err := parseMode(base.Name, ac.Mode)
		if err != nil {
			return core.Agent{}, err
		}
		out.Mode = mode
	}
	if ac.Model != "" {
		ref, alias, err := resolveModelString(base.Name, ac.Model, cfg)
		if err != nil {
			return core.Agent{}, err
		}
		out.Model = ref
		out.ModelAlias = alias
	}
	if ac.Prompt != "" {
		out.Prompt = ac.Prompt
	}
	if ac.MaxSteps != 0 {
		out.MaxSteps = ac.MaxSteps
	}
	if ac.CanSpawn != nil {
		out.CanSpawn = *ac.CanSpawn
	}
	if ac.Hidden != nil {
		out.Hidden = *ac.Hidden
	}
	if ac.Tools != nil {
		out.Tools = ac.Tools
	}
	out.Permissions = mergePermissions(base.Permissions, ac.Permissions)

	return out, nil
}

// parseMode validates s as an AgentMode, returning an error naming
// agentName if it isn't one of the known modes.
func parseMode(agentName, s string) (core.AgentMode, error) {
	mode := core.AgentMode(s)
	switch mode {
	case core.ModePrimary, core.ModeSubagent, core.ModeAll:
		return mode, nil
	default:
		return "", fmt.Errorf("agent %q: invalid mode %q", agentName, s)
	}
}

// resolveModelString parses a "model" config value into a ModelRef and,
// when it was an alias, the alias text (kept for display). A value
// containing "/" is parsed directly; otherwise it is looked up in
// cfg.ModelAliases.
func resolveModelString(agentName, s string, cfg core.Config) (core.ModelRef, string, error) {
	ref, err := ParseRef(s, cfg.ModelAliases)
	if err != nil {
		return core.ModelRef{}, "", fmt.Errorf("agent %q: %w", agentName, err)
	}
	if strings.Contains(s, "/") {
		return ref, "", nil
	}
	return ref, s, nil
}

// mergePermissions overlays overlay onto base per tool: Default is
// replaced when overlay sets it, and Patterns is key-merged.
func mergePermissions(base, overlay core.PermissionRules) core.PermissionRules {
	if overlay == nil {
		return base
	}

	out := make(core.PermissionRules, len(base)+len(overlay))
	for tool, rule := range base {
		out[tool] = rule
	}
	for tool, orule := range overlay {
		merged := out[tool]
		if orule.Default != "" {
			merged.Default = orule.Default
		}
		if orule.Patterns != nil {
			patterns := make(map[string]core.Action, len(merged.Patterns)+len(orule.Patterns))
			for pat, action := range merged.Patterns {
				patterns[pat] = action
			}
			for pat, action := range orule.Patterns {
				patterns[pat] = action
			}
			merged.Patterns = patterns
		}
		out[tool] = merged
	}
	return out
}

// Get returns the merged agent named name, if any.
func (s *Service) Get(name string) (core.Agent, bool) {
	a, ok := s.agents[name]
	return a, ok
}

// Primary returns every non-hidden agent whose mode is primary or all,
// with build and plan first (in that order) and everything else sorted by
// name.
func (s *Service) Primary() []core.Agent {
	var out []core.Agent
	for _, a := range s.agents {
		if a.Hidden {
			continue
		}
		if a.Mode == core.ModePrimary || a.Mode == core.ModeAll {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessPrimary(out[i], out[j]) })
	return out
}

// primaryRank orders build first, plan second, and everything else after.
func primaryRank(name string) int {
	switch name {
	case "build":
		return 0
	case "plan":
		return 1
	default:
		return 2
	}
}

func lessPrimary(a, b core.Agent) bool {
	ra, rb := primaryRank(a.Name), primaryRank(b.Name)
	if ra != rb {
		return ra < rb
	}
	return a.Name < b.Name
}

// Subagents returns every non-hidden agent whose mode is subagent or all,
// sorted by name.
func (s *Service) Subagents() []core.Agent {
	var out []core.Agent
	for _, a := range s.agents {
		if a.Hidden {
			continue
		}
		if a.Mode == core.ModeSubagent || a.Mode == core.ModeAll {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
