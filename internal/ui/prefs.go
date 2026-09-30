package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

// prefsCtl applies the persisted UI preferences: the ones loaded at
// startup, and the view toggles that save them back.
type prefsCtl struct{ a *App }

// load applies the prefs read at startup.
func (p prefsCtl) load(prefs core.Prefs) {
	a := p.a
	a.view.sidebarPref = prefs.Sidebar
	p.setHideReasoning(prefs.HideReasoning)
	a.view.history = append([]string(nil), prefs.History[a.opts.ProjectKey]...)
	a.w.prompt.SetHistory(a.view.history)
	a.view.pick.recent = slices.Clone(prefs.Recent[:min(len(prefs.Recent), maxRecent)])
	a.w.picker.SetRecent(a.view.pick.recent)
}

// toggleSidebar shows or hides the sidebar and saves the choice.
func (p prefsCtl) toggleSidebar() tea.Cmd {
	show := !p.a.lay.SideVisible
	p.a.view.sidebarPref = &show
	return prefsUpdateCmd(p.a.ports, func(pr *core.Prefs) { pr.Sidebar = &show })
}

// setReasoning turns streamed reasoning off (hide) or on, the choice from
// the reasoning picker level, and saves it.
func (p prefsCtl) setReasoning(hide bool) tea.Cmd {
	p.setHideReasoning(hide)
	return prefsUpdateCmd(p.a.ports, func(pr *core.Prefs) { pr.HideReasoning = hide })
}

// setHideReasoning sets the renderer's hideReasoning and re-renders the
// blocks still thinking (a finished reasoning block renders the same
// either way).
func (p prefsCtl) setHideReasoning(hide bool) {
	a := p.a
	if a.w.render.hideReasoning == hide {
		return
	}
	a.w.render.hideReasoning = hide
	var ids []transcript.BlockID
	for _, b := range a.sess.main.proj.Blocks() {
		if b.Thinking {
			ids = append(ids, b.ID)
		}
	}
	a.flush(ids)
}
