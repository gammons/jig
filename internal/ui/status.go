package ui

import (
	"slices"
	"strings"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/statusbar"
	"github.com/gammons/jig/internal/core"
)

// status builds the status bar's session segments (the App adds the mode,
// the untrusted flag, and the hint). CtxUsed is the last root step's input
// plus cache-read tokens against the current model's context window.
func (s *sessionState) status(aliases map[string]string) statusbar.State {
	ref := s.modelRef()
	st := statusbar.State{
		Agent:    ansi.SanitizeLine(s.info.Agent),
		Model:    ansi.SanitizeLine(displayModel(ref, aliases)),
		Running:  s.run.running,
		Frame:    s.run.frame,
		CtxUsed:  s.usage.Input + s.usage.CacheRead,
		CtxLimit: s.contextWindow(ref),
		CostUSD:  s.cost,
		Pending:  len(s.proj.Pending()),
		Queued:   s.queued,
	}
	if s.run.running {
		st.Elapsed = s.clk.Now().Sub(s.run.startedAt)
	}
	return st
}

// modelRef is the "provider/model" the next send runs on: the session's
// own model, else the agent's configured one, else the one the last root
// step reported.
func (s *sessionState) modelRef() string {
	if s.info.Model != "" {
		return s.info.Model
	}
	if i := s.agentIndex(); i >= 0 && !s.cat.agents[i].Model.IsZero() {
		return s.cat.agents[i].Model.String()
	}
	return s.model
}

// contextWindow looks ref up in the cached catalog; 0 when unknown.
func (s *sessionState) contextWindow(ref string) int64 {
	mr, err := core.ParseModelRef(ref)
	if err != nil {
		return 0
	}
	for _, p := range s.cat.providers {
		if p.Info.ID != mr.Provider {
			continue
		}
		for _, m := range p.Info.Models {
			if m.Ref == mr {
				return m.ContextWindow
			}
		}
	}
	return 0
}

// displayModel shows ref by an alias that maps to it (the first by name,
// for a stable choice), else by its short model ID (without the provider).
func displayModel(ref string, aliases map[string]string) string {
	names := make([]string, 0, len(aliases))
	for name, target := range aliases {
		if target == ref {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		slices.Sort(names)
		return names[0]
	}
	if _, model, ok := strings.Cut(ref, "/"); ok {
		return model
	}
	return ref
}
