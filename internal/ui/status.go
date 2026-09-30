package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

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
		Branch:   s.cat.branch,
		Agent:    ansi.SanitizeLine(s.info.Agent),
		Model:    ansi.SanitizeLine(displayModel(ref, aliases)),
		Effort:   ansi.SanitizeLine(string(shownEffort(s))),
		Running:  s.run.running,
		Frame:    s.run.frame,
		CtxUsed:  s.usage.Input + s.usage.CacheRead,
		CtxLimit: s.contextWindow(ref),
		CostUSD:  s.cost,
	}
	st.Pending = len(s.main.proj.Pending())
	st.Queued = s.queued
	if s.run.running {
		st.Elapsed = s.clk.Now().Sub(s.run.startedAt)
	}
	return st
}

// modelRef is the "provider/model" the next send runs on: the session's
// own model, else the agent's configured one, else the one the last root
// step reported, else the configured default.
func (s *sessionState) modelRef() string {
	if s.info.Model != "" {
		return s.info.Model
	}
	if i := s.agentIndex(); i >= 0 && !s.cat.agents[i].Model.IsZero() {
		return s.cat.agents[i].Model.String()
	}
	if s.model != "" {
		return s.model
	}
	return s.cat.defaultModel
}

// contextWindow looks ref up in the cached catalog; 0 when unknown.
func (s *sessionState) contextWindow(ref string) int64 {
	m, _ := lookupModel(s.cat.providers, ref)
	return m.ContextWindow
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

// sync points the permission card at its request (returning its arming
// tick, if any) and rebuilds the status bar and the sidebar from the state.
func (a *App) sync() tea.Cmd {
	cmd := permCtl{a}.sync()
	a.w.status.Set(a.statusState())
	if a.lay.SideVisible {
		a.w.side.SetSections(sidebarSections(a))
	}
	return cmd
}

// statusState is the full status bar state.
func (a *App) statusState() statusbar.State {
	st := a.sess.status(a.opts.Aliases)
	st.Mode = a.mode.String()
	st.Untrusted = a.opts.Untrusted
	st.Hint = a.view.hint
	st.Pending = len(a.sess.main.proj.Pending())
	if st.Hint == "" && st.Pending > 0 && (a.mode != modeNormal || !permCtl{a}.onCard()) {
		st.Hint = permissionHint
	}
	if !a.lay.SideVisible {
		st.MCPIssues = mcpIssues(a.view.mcp.list)
	}
	return st
}

// lookupModel finds ref ("provider/model") in the cached catalog.
func lookupModel(providers []core.ProviderStatus, ref string) (core.ModelInfo, bool) {
	mr, err := core.ParseModelRef(ref)
	if err != nil {
		return core.ModelInfo{}, false
	}
	for _, p := range providers {
		if p.Info.ID != mr.Provider {
			continue
		}
		for _, m := range p.Info.Models {
			if m.Ref == mr {
				return m, true
			}
		}
	}
	return core.ModelInfo{}, false
}

// requestedEffort mirrors chat's precedence for a primary run: the
// session's choice, then the agent's effort, then default_effort.
func requestedEffort(s *sessionState) core.Effort {
	if s.info.Effort.Known() {
		return s.info.Effort
	}
	return fallbackEffort(s)
}

// fallbackEffort is the effort requested with no session choice: the
// agent's effort, then default_effort.
func fallbackEffort(s *sessionState) core.Effort {
	if i := s.agentIndex(); i >= 0 && s.cat.agents[i].Effort.Known() {
		return s.cat.agents[i].Effort
	}
	return s.cat.defaultEffort
}

// shownEffort is the level the next send's requests carry: the requested
// one defaulted and clamped against the current model's catalog entry.
func shownEffort(s *sessionState) core.Effort {
	m, ok := lookupModel(s.cat.providers, s.modelRef())
	if !ok {
		return ""
	}
	return core.EffectiveEffort(m, requestedEffort(s))
}

// effortControllable reports whether the current model lists effort levels.
func effortControllable(s *sessionState) bool {
	m, ok := lookupModel(s.cat.providers, s.modelRef())
	return ok && len(m.Efforts) > 0
}
