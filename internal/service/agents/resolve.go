package agents

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gammons/jig/internal/core"
)

// ErrNoModelConfigured is returned by ResolveModel when the agent, parent,
// session, and cfg.DefaultModel are all zero/unset.
var ErrNoModelConfigured = errors.New("no model configured: set default_model in config.toml or pass --model")

// ParseRef resolves a model string: "provider/model" is parsed directly,
// and anything else is looked up as an alias in aliases (whose value must
// itself be "provider/model").
func ParseRef(s string, aliases map[string]string) (core.ModelRef, error) {
	if strings.Contains(s, "/") {
		return core.ParseModelRef(s)
	}
	value, ok := aliases[s]
	if !ok {
		return core.ModelRef{}, fmt.Errorf("model alias %q is not defined in [model_aliases]", s)
	}
	return core.ParseModelRef(value)
}

// ResolveRef resolves a model string (a "provider/model" ref or a
// [model_aliases] name) against s's config. It backs --model,
// default_model, small_model, and agents' model fields.
func (s *Service) ResolveRef(str string) (core.ModelRef, error) {
	return ParseRef(str, s.cfg.ModelAliases)
}

// ResolveModel returns the first non-zero model among a's own Model,
// parent, session, and cfg.DefaultModel (which may be an alias), in that
// order. It errors if all four are zero.
func (s *Service) ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error) {
	if !a.Model.IsZero() {
		return a.Model, nil
	}
	if !parent.IsZero() {
		return parent, nil
	}
	if !session.IsZero() {
		return session, nil
	}
	if s.cfg.DefaultModel == "" {
		return core.ModelRef{}, ErrNoModelConfigured
	}
	return s.ResolveRef(s.cfg.DefaultModel)
}

// ResolveEffort returns the requested reasoning effort for a run of a:
// the first level on the scale among session (the session's or
// --effort's choice; pass "" for a subagent), a's own Effort, and
// cfg.DefaultEffort, or "" (the model's catalog default) when none is.
// A value off the scale, such as a hand-edited stored one, is skipped.
func (s *Service) ResolveEffort(a core.Agent, session core.Effort) core.Effort {
	def, _ := core.ParseEffort(s.cfg.DefaultEffort)
	for _, e := range []core.Effort{session, a.Effort, def} {
		if e.Known() {
			return e
		}
	}
	return ""
}

// SmallModel returns cfg.SmallModel (a ref or alias) resolved to a
// ModelRef when set and resolvable, otherwise fallback.
func (s *Service) SmallModel(fallback core.ModelRef) core.ModelRef {
	if s.cfg.SmallModel == "" {
		return fallback
	}
	ref, err := s.ResolveRef(s.cfg.SmallModel)
	if err != nil {
		return fallback
	}
	return ref
}
