package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/transcript"
)

// kidsCtl runs the App's live child panes (spec §4.1-4.2): one per
// spawned subagent, each a transcript.NewChild projection loaded from
// its own stored history and kept current by every event under the root.
type kidsCtl struct{ a *App }

// childLoadedMsg carries a kid pane's load result. A message whose gen is
// stale (superseded by a newer load of the same pane) is dropped.
type childLoadedMsg struct {
	session core.SessionID
	gen     int
	msgs    []core.Message
	err     error
}

// kidChange is one kid pane's blocks changed by routeKids, for the
// caller to upsert once the pane is in a column (a later task); routeKids
// itself never builds blocklist items, since no column exists yet.
type kidChange struct {
	p      *pane
	ids    []transcript.BlockID
	relist bool
}

// onEvent routes ev to every kid pane and, on a spawn, creates the new
// child's pane (after routeKids, so the spawn updates the parent's own
// subagent block first).
func (k kidsCtl) onEvent(ev event.Event) tea.Cmd {
	k.routeKids(ev)
	e, ok := ev.(event.SubagentSpawned)
	if !ok {
		return nil
	}
	_, cmd := k.kid(e.Child, e.Agent, e.Description)
	return cmd
}

// kid returns child's pane, creating and loading it if this is the first
// time it's seen. The load uses sessionMessagesCmd; every new pane loads,
// even one created for a subagent that turns out to be brand new (spec
// §4.2), since the App can't tell the difference up front.
func (k kidsCtl) kid(child core.SessionID, agent, desc string) (*pane, tea.Cmd) {
	a := k.a
	if p, ok := a.sess.kids[child]; ok {
		return p, nil
	}
	p := newTranscriptPane(transcript.NewChild(a.sess.info.ID, child), a.w.render, &a.theme.set)
	p.title = ansi.SanitizeLine("↳ " + agent + ": " + desc)
	p.load.loading = true
	p.load.gen++
	gen := p.load.gen
	a.sess.kids[child] = p
	cmd := sessionMessagesCmd(a.ctx, a.ports, child, func(msgs []core.Message, err error) tea.Msg {
		return childLoadedMsg{session: child, gen: gen, msgs: msgs, err: err}
	})
	return p, cmd
}

// routeKids offers ev to every kid pane: a loading pane buffers it for
// childLoaded to replay; others apply it at once. Deltas only mark their
// blocks dirty (as main does); every other change is returned for the
// caller to upsert once the pane is shown (a later task). A regroup runs
// per the same rule main uses.
func (k kidsCtl) routeKids(ev event.Event) []kidChange {
	var changes []kidChange
	for _, p := range k.a.sess.kids {
		if p.load.loading {
			p.load.buffered = append(p.load.buffered, ev)
			continue
		}
		ids := p.proj.Apply(ev)
		p.times.thinking(p.proj, ev, ids, k.a.sess.clk.Now())
		relist := regroupsOn(ev) && p.track.regroup(p.proj)
		if len(ids) == 0 && !relist {
			continue
		}
		switch ev.(type) {
		case event.TextDelta, event.ReasoningDelta:
			for _, id := range ids {
				p.track.dirty.add(id)
			}
			continue
		}
		switch ev.(type) {
		case event.ToolCallStarted, event.ToolCallFinished:
			p.timeTools(ev, ids, k.a.sess.clk.Now())
		}
		changes = append(changes, kidChange{p: p, ids: p.withDirty(ids), relist: relist})
	}
	return changes
}

// childLoaded applies a resolved load: the stored history (or a load
// error, as a notice), then every buffered event replayable per
// replayable's rule, in order.
func (k kidsCtl) childLoaded(msg childLoadedMsg) tea.Cmd {
	p, ok := k.a.sess.kids[msg.session]
	if !ok || msg.gen != p.load.gen {
		return nil
	}
	if msg.err != nil {
		p.proj.AddNotice("could not load subagent: "+ansi.SanitizeLine(msg.err.Error()), transcript.LevelError)
	} else {
		p.proj.Load(msg.msgs)
	}
	loaded, lastStatus := loadedIndex(msg.msgs)
	for _, ev := range p.load.buffered {
		if !replayable(ev, loaded, lastStatus) {
			continue
		}
		ids := p.proj.Apply(ev)
		switch ev.(type) {
		case event.TextDelta, event.ReasoningDelta:
			for _, id := range ids {
				p.track.dirty.add(id)
			}
		}
	}
	p.load.buffered = nil
	p.load.loading = false
	return nil
}

// clearKids drops every kid pane: wherever the root's own projection is
// rebuilt (a fresh or switched session, or a load), no earlier root's
// subagents can still be live.
func (k kidsCtl) clearKids() {
	k.a.sess.kids = map[core.SessionID]*pane{}
}

// loadedIndex is the set of message IDs a load's snapshot holds, and the
// status of its last message (used by replayable's RunFailed rule).
func loadedIndex(msgs []core.Message) (map[core.MessageID]bool, core.MessageStatus) {
	loaded := make(map[core.MessageID]bool, len(msgs))
	var lastStatus core.MessageStatus
	for _, m := range msgs {
		loaded[m.ID] = true
		lastStatus = m.Status
	}
	return loaded, lastStatus
}

// replayable reports whether a buffered event, held while a kid pane
// loaded, must still be applied after the load: false when its message is
// already in the loaded snapshot, or it is a RunFailed and the snapshot's
// last message already carries a failed/interrupted notice for it (Load
// adds one; replaying would duplicate it). Every other event (no
// MessageID, or one outside the snapshot) is replayed.
func replayable(ev event.Event, loaded map[core.MessageID]bool, lastStatus core.MessageStatus) bool {
	if id := messageIDOf(ev); id != "" && loaded[id] {
		return false
	}
	if _, ok := ev.(event.RunFailed); ok {
		if lastStatus == core.StatusFailed || lastStatus == core.StatusInterrupted {
			return false
		}
	}
	return true
}

// messageIDOf returns ev's MessageID, or "" for an event that carries
// none.
func messageIDOf(ev event.Event) core.MessageID {
	switch e := ev.(type) {
	case event.MessageStarted:
		return e.MessageID
	case event.TextDelta:
		return e.MessageID
	case event.ReasoningDelta:
		return e.MessageID
	case event.ToolCallStarted:
		return e.MessageID
	case event.ToolCallFinished:
		return e.MessageID
	case event.StepFinished:
		return e.MessageID
	case event.RunFinished:
		return e.MessageID
	}
	return ""
}
