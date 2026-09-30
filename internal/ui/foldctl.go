package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/ui/transcript"
)

// foldCtl runs one pane's tool-call groups from the App (tool-call
// groups spec §5–6, D1: every transcript pane, main and each subagent,
// gets fold): it renders an event's changes, rebuilding the list when
// the layout changed other than at its end, and keeps the selection on
// a block that is still listed. p is the pane it operates on; callers
// that always mean main (the root event pipeline) pass a.sess.main,
// and NORMAL's fold/search/yank/gp keys pass columnFocused(a).
type foldCtl struct {
	a *App
	p *pane
}

// apply renders an event's changed blocks on foldCtl's pane: a rebuild
// when res.relist, else an upsert.
func (c foldCtl) apply(res applyResult) tea.Cmd {
	if res.relist {
		return c.relist(res.upsert)
	}
	c.a.flush(res.upsert)
	return nil
}

// relist rebuilds p's list from its current layout, re-rendering only
// the headers and the items showing changed (itemTrack.relistItems).
// When the selected item is no longer listed (its group collapsed, or
// the first call of a new group turned into its header), the selection
// moves to its group's header, and, on main, an open details split
// follows it.
func (c foldCtl) relist(changed []transcript.BlockID) tea.Cmd {
	a, p := c.a, c.p
	before, had := p.list.Selected()
	items := p.track.relistItems(p, changed, a.sess.run.frame)
	a.w.setItems(p, items)
	if !had || slices.ContainsFunc(items, func(it blocklist.Item) bool { return it.ID == before.ID }) {
		return nil
	}
	if g, ok := p.track.fold.groupOf(transcript.BlockID(before.ID)); ok {
		p.list.Select(string(g))
	}
	if p != a.sess.main {
		return nil
	}
	return normalKeys{a}.syncDetails(before)
}

// toggle expands or collapses the selected group header on p, or
// collapses the group of the selected member (the selection then moves
// to its header). It does nothing on any other block.
func (c foldCtl) toggle() tea.Cmd {
	p := c.p
	it, ok := p.list.Selected()
	if !ok {
		return nil
	}
	g, ok := p.track.fold.toggle(transcript.BlockID(it.ID))
	if !ok {
		return nil
	}
	cmd := c.relayout()
	if g == transcript.BlockID(it.ID) {
		// Expanding a header that was the list's last item must not let
		// SetItems' follow rule move the selection to its last member.
		p.list.Select(it.ID)
	}
	return cmd
}

// relayout re-lays out p's groups after a fold or search change and
// rebuilds its list.
func (c foldCtl) relayout() tea.Cmd {
	c.p.track.fold.regroup(c.p.proj.Blocks())
	return c.relist(nil)
}

// group returns group id's member blocks and their durations, from p;
// false when id is not a group.
func (c foldCtl) group(id transcript.BlockID) ([]transcript.Block, []time.Duration, bool) {
	ids, ok := c.p.track.fold.members(id)
	if !ok {
		return nil, nil, false
	}
	members, durs := groupMembers(c.p, ids)
	return members, durs, len(members) > 0
}

// setSearch holds every group open on p while a search is applied (on),
// so a match inside a collapsed group can be found, and lets each group
// return to its own state when the search is cleared.
func (c foldCtl) setSearch(on bool) tea.Cmd {
	f := &c.p.track.fold
	if f.search == on {
		return nil
	}
	f.search = on
	return c.relayout()
}
