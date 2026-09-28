package ui

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/actions"
)

// maxRecent is how many recent actions are kept (spec §6.4).
const maxRecent = 5

// pickerView is the App's picker state: the mode to return to when it
// closes, whether it was opened by an @ in the prompt (so esc leaves a
// literal @), and the recent action IDs, most recent first.
type pickerView struct {
	prev    mode
	mention bool
	recent  []string
}

// themePreviewMsg asks the App to preview the palette named name.
type themePreviewMsg struct{ name string }

// previewTheme is the picker's PreviewFunc: on the themes level, the
// highlighted palette is applied until the picker closes.
func previewTheme(level picker.Level, it picker.Item) tea.Cmd {
	if level.ID != levelThemes {
		return nil
	}
	return func() tea.Msg { return themePreviewMsg{name: it.ID} }
}

// pickerCtl runs the picker from the App: opening it, and reconciling
// what it emits (items, previews, a choice, entered text, a close) into
// the App's state. It also runs the actions no mode handler owns.
type pickerCtl struct{ a *App }

// open shows the picker at level, remembering the mode to return to;
// mention marks an open by @ in the prompt.
func (p pickerCtl) open(level picker.Level, mention bool) tea.Cmd {
	a := p.a
	if a.mode != modePicker {
		a.view.pick.prev = a.mode
	}
	a.view.pick.mention = mention
	a.setMode(modePicker)
	a.w.picker.SetRecent(a.view.pick.recent)
	return a.w.picker.Open(level)
}

// handle reconciles one message from the picker.
func (p pickerCtl) handle(msg tea.Msg) tea.Cmd {
	a := p.a
	switch msg := msg.(type) {
	case picker.ItemsMsg:
		var cmd tea.Cmd
		a.w.picker, cmd = a.w.picker.Update(msg)
		return cmd
	case themePreviewMsg:
		// A preview arriving after the picker closed is stale.
		if a.w.picker.IsOpen() && a.theme.preview(msg.name) {
			p.restyle()
		}
		return nil
	case picker.ClosedMsg:
		cmd := p.closed()
		if msg.Level.ID == levelFiles && a.view.pick.mention {
			a.w.prompt.Insert("@")
		}
		a.view.pick.mention = false
		return cmd
	case picker.InputMsg:
		cmd := p.closed()
		if msg.Level.ID != levelRename || a.sess.info.ID == "" {
			return cmd
		}
		return tea.Batch(cmd, renameCmd(a.ctx, a.ports, a.sess.info.ID, msg.Text), p.remember(actions.SessionRename))
	case picker.ChosenMsg:
		return p.chosen(msg)
	}
	return nil
}

// closed returns to the mode the picker was opened from and ends any
// theme preview, restoring the original palette.
func (p pickerCtl) closed() tea.Cmd {
	a := p.a
	if a.theme.restore() {
		p.restyle()
	}
	return a.setMode(a.view.pick.prev)
}

// chosen runs the choice on level: an action, or a drill-down's item.
func (p pickerCtl) chosen(msg picker.ChosenMsg) tea.Cmd {
	a := p.a
	if len(msg.Items) == 0 {
		return p.closed()
	}
	first := msg.Items[0].ID
	if msg.Level.ID == levelThemes {
		if a.theme.keep(first) {
			p.restyle()
		}
		name := first
		return tea.Batch(p.closed(), p.remember(actions.ViewTheme),
			prefsUpdateCmd(a.ports, func(pr *core.Prefs) { pr.Theme = name }))
	}
	cmds := []tea.Cmd{p.closed()}
	switch msg.Level.ID {
	case levelRoot:
		// a.runAction records id as recent itself, whether it got here
		// from this choice or from a key.
		id := actions.ID(first)
		if strings.HasPrefix(first, "transcript.") {
			cmds = append(cmds, a.setMode(modeNormal))
		}
		return tea.Batch(append(cmds, a.runAction(id))...)
	case levelSessions:
		cmds = append(cmds, p.resume(core.SessionID(first)))
	case levelModels:
		cmds = append(cmds, p.setModel(first))
	case levelAgents:
		cmds = append(cmds, p.setAgent(first))
	case levelFiles:
		for _, it := range msg.Items {
			a.w.prompt.Insert("@" + it.ID + " ")
			if !slices.Contains(a.sess.attach, it.ID) {
				a.sess.attach = append(a.sess.attach, it.ID)
			}
		}
		a.view.pick.mention = false
	}
	return tea.Batch(append(cmds, p.remember(levelAction(msg.Level.ID)))...)
}

// action runs a remappable action no mode handler owns: the session
// actions, a drill-down opened straight from a key, and extensions.
func (p pickerCtl) action(id actions.ID) tea.Cmd {
	a := p.a
	switch id {
	case actions.SessionNew:
		return p.newSession()
	case actions.SessionCompact:
		if a.sess.info.ID == "" {
			a.view.hint = "no session to compact"
			return nil
		}
		return compactCmd(a.ctx, a.ports, a.sess.info.ID)
	case actions.SessionRename:
		if a.sess.info.ID == "" {
			a.view.hint = "no session to rename"
			return nil
		}
	}
	if lvl, ok := drillLevel(id, a.sess.info.Title); ok {
		return p.open(lvl, false)
	}
	if act, ok := a.opts.Actions.Get(id); ok && act.Command != nil {
		return extCmd(a.ctx, act.Command, string(id))
	}
	return nil
}

// compacted shows a compaction's outcome; a success reloads the session
// so its summary notice appears.
func (p pickerCtl) compacted(msg compactedMsg) tea.Cmd {
	a := p.a
	switch {
	case msg.err == nil:
		a.view.hint = "compacted"
		if msg.id == a.sess.info.ID {
			return resumeCmd(a.ctx, a.ports, msg.id)
		}
	case errors.Is(msg.err, core.ErrBusy):
		a.view.hint = "session is busy"
	case strings.Contains(msg.err.Error(), "nothing to compact"):
		a.view.hint = "nothing to compact"
	default:
		a.view.hint = "compact: " + ansi.SanitizeLine(msg.err.Error())
	}
	return nil
}

// remember pushes id onto the recent actions, in the picker at once and
// in prefs.
func (p pickerCtl) remember(id actions.ID) tea.Cmd {
	a := p.a
	if id == "" {
		return nil
	}
	a.view.pick.recent = pushRecent(a.view.pick.recent, string(id))
	a.w.picker.SetRecent(a.view.pick.recent)
	return prefsUpdateCmd(a.ports, func(pr *core.Prefs) { pr.Recent = pushRecent(pr.Recent, string(id)) })
}

// pushRecent puts id first in recent, without repeating it, keeping at
// most maxRecent.
func pushRecent(recent []string, id string) []string {
	out := []string{id}
	for _, r := range recent {
		if r != id && len(out) < maxRecent {
			out = append(out, r)
		}
	}
	return out
}

// newSession clears the session: the next send creates one, with the
// agent and model chosen now.
func (p pickerCtl) newSession() tea.Cmd {
	a := p.a
	if a.sess.run.busy() {
		a.view.hint = "session is busy"
		return nil
	}
	a.sess = freshSession(a.sess, "")
	a.view.detailsOpen = false
	a.w.list.SetItems(nil)
	return nil
}

// resume loads session id (the resumeMsg handler swaps it in), unless a
// send is still in flight.
func (p pickerCtl) resume(id core.SessionID) tea.Cmd {
	a := p.a
	if a.sess.run.busy() {
		a.view.hint = "session is busy"
		return nil
	}
	return resumeCmd(a.ctx, a.ports, id)
}

// freshSession is a new session state for id, keeping old's cached
// catalog, agent, and model. Recorded attachments don't carry over: a
// path picked for the old session must not attach to the new one.
func freshSession(old *sessionState, id core.SessionID) *sessionState {
	s := newSessionState(id, old.clk)
	s.cat = old.cat
	s.info.Agent, s.info.Model = old.info.Agent, old.info.Model
	return s
}

// setModel makes ref the model the next send uses, and the session's.
func (p pickerCtl) setModel(ref string) tea.Cmd {
	a := p.a
	a.sess.info.Model = ref
	if a.sess.info.ID == "" {
		return nil
	}
	return configureCmd(a.ctx, a.ports, a.sess.info.ID, "", ref)
}

// setAgent makes name the agent, like tab does.
func (p pickerCtl) setAgent(name string) tea.Cmd {
	a := p.a
	a.sess.info.Agent = name
	a.w.prompt.SetAgent(ansi.SanitizeLine(name))
	if a.sess.info.ID == "" {
		return nil
	}
	return configureCmd(a.ctx, a.ports, a.sess.info.ID, name, "")
}

// restyle pushes the current theme's styles to the widgets that cache
// renders: the transcript list (a new version re-renders every block)
// and its Markdown renderer.
func (p pickerCtl) restyle() {
	a := p.a
	a.w.render.md.SetStyles(a.theme.set.Markdown)
	a.w.list.SetStyles(a.theme.set.Blocklist, a.theme.version)
}
