package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/ui/transcript"
)

// foldCtl runs the transcript's tool-call groups from the App (tool-call
// groups spec §5–6): it renders an event's changes, rebuilding the list
// when the layout changed other than at its end, and keeps the selection
// on a block that is still listed.
type foldCtl struct{ a *App }

// apply renders an event's changed blocks: a rebuild when res.relist,
// else an upsert.
func (c foldCtl) apply(res applyResult) tea.Cmd {
	if res.relist {
		return c.relist(res.upsert)
	}
	c.a.flush(res.upsert)
	return nil
}

// relist rebuilds the list from the current layout, re-rendering only
// the headers and the items showing changed (itemTrack.relistItems). When
// the selected item is no longer listed (its group collapsed, or the
// first call of a new group turned into its header), the selection moves
// to its group's header, and an open details split follows it.
func (c foldCtl) relist(changed []transcript.BlockID) tea.Cmd {
	a := c.a
	before, had := a.sess.main.list.Selected()
	items := a.sess.main.track.relistItems(a.sess.main, changed, a.sess.run.frame)
	a.w.setItems(a.sess.main, items)
	if !had || slices.ContainsFunc(items, func(it blocklist.Item) bool { return it.ID == before.ID }) {
		return nil
	}
	if g, ok := a.sess.main.track.fold.groupOf(transcript.BlockID(before.ID)); ok {
		a.sess.main.list.Select(string(g))
	}
	return normalKeys{a}.syncDetails(before)
}

// toggle expands or collapses the selected group header, or collapses
// the group of the selected member (the selection then moves to its
// header). It does nothing on any other block.
func (c foldCtl) toggle() tea.Cmd {
	a := c.a
	it, ok := a.sess.main.list.Selected()
	if !ok {
		return nil
	}
	g, ok := a.sess.main.track.fold.toggle(transcript.BlockID(it.ID))
	if !ok {
		return nil
	}
	cmd := c.relayout()
	if g == transcript.BlockID(it.ID) {
		// Expanding a header that was the list's last item must not let
		// SetItems' follow rule move the selection to its last member.
		a.sess.main.list.Select(it.ID)
	}
	return cmd
}

// relayout re-lays out the groups after a fold or search change and
// rebuilds the list.
func (c foldCtl) relayout() tea.Cmd {
	c.a.sess.main.track.fold.regroup(c.a.sess.main.proj.Blocks())
	return c.relist(nil)
}

// group returns group id's member blocks and their durations; false when
// id is not a group.
func (c foldCtl) group(id transcript.BlockID) ([]transcript.Block, []time.Duration, bool) {
	ids, ok := c.a.sess.main.track.fold.members(id)
	if !ok {
		return nil, nil, false
	}
	members, durs := groupMembers(c.a.sess.main, ids)
	return members, durs, len(members) > 0
}

// setSearch holds every group open while a search is applied (on), so
// a match inside a collapsed group can be found, and lets each group
// return to its own state when the search is cleared.
func (c foldCtl) setSearch(on bool) tea.Cmd {
	f := &c.a.sess.main.track.fold
	if f.search == on {
		return nil
	}
	f.search = on
	return c.relayout()
}
