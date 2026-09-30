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
	before, had := a.w.list.Selected()
	items := a.sess.track.relistItems(a.sess, changed)
	a.w.setItems(items)
	if !had || slices.ContainsFunc(items, func(it blocklist.Item) bool { return it.ID == before.ID }) {
		return nil
	}
	if g, ok := a.sess.track.fold.groupOf(transcript.BlockID(before.ID)); ok {
		a.w.list.Select(string(g))
	}
	return normalKeys{a}.syncDetails(before)
}

// toggle expands or collapses the selected group header, or collapses
// the group of the selected member (the selection then moves to its
// header). It does nothing on any other block.
func (c foldCtl) toggle() tea.Cmd {
	a := c.a
	it, ok := a.w.list.Selected()
	if !ok {
		return nil
	}
	if _, ok := a.sess.track.fold.toggle(transcript.BlockID(it.ID)); !ok {
		return nil
	}
	return c.relayout()
}

// relayout re-lays out the groups after a fold or search change and
// rebuilds the list.
func (c foldCtl) relayout() tea.Cmd {
	c.a.sess.track.fold.regroup(c.a.sess.proj.Blocks())
	return c.relist(nil)
}

// group returns group id's member blocks and their durations; false when
// id is not a group.
func (c foldCtl) group(id transcript.BlockID) ([]transcript.Block, []time.Duration, bool) {
	ids, ok := c.a.sess.track.fold.members(id)
	if !ok {
		return nil, nil, false
	}
	members, durs := groupMembers(c.a.sess, ids)
	return members, durs, len(members) > 0
}
