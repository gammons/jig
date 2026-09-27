package agents

import (
	"errors"

	"github.com/gammons/jig/internal/core"
)

// ErrNoModelConfigured is returned by ResolveModel when the agent, parent,
// session, and cfg.DefaultModel are all zero/unset.
var ErrNoModelConfigured = errors.New("no model configured: set default_model in config.toml or pass --model")

// ResolveModel returns the first non-zero model among a's own Model,
// parent, session, and cfg.DefaultModel, in that order. It errors if all
// four are zero.
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
	return core.ParseModelRef(s.cfg.DefaultModel)
}

// SmallModel returns cfg.SmallModel parsed as a ModelRef when set and
// parsable, otherwise fallback.
func (s *Service) SmallModel(fallback core.ModelRef) core.ModelRef {
	if s.cfg.SmallModel == "" {
		return fallback
	}
	ref, err := core.ParseModelRef(s.cfg.SmallModel)
	if err != nil {
		return fallback
	}
	return ref
}
