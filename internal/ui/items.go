package ui

import (
	"slices"
	"time"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/ui/transcript"
)

// itemTrack is the transcript list's bookkeeping, kept apart from
// sessionState (archtest's struct limits): the item version issued per
// ID (versions only ever grow, so an ID shown again never matches a stale
// cached render), the dirty streaming blocks (rendered on the next
// streamTick), the live items, whose spinner each tick advances, each
// block's nesting as last issued, each group header's shown open state as
// last issued, and the tool-call groups (fold).
type itemTrack struct {
	versions  map[transcript.BlockID]int
	dirty     idSet
	live      map[transcript.BlockID]bool
	nested    map[transcript.BlockID]bool
	shownOpen map[transcript.BlockID]bool
	fold      foldState
}

// newItemTrack returns an empty itemTrack.
func newItemTrack() itemTrack {
	return itemTrack{
		versions:  map[transcript.BlockID]int{},
		live:      map[transcript.BlockID]bool{},
		nested:    map[transcript.BlockID]bool{},
		shownOpen: map[transcript.BlockID]bool{},
	}
}

// reset clears what a new projection invalidates; versions survive, and so
// does the search hold (the blocklist keeps its applied query across a
// new item set).
func (t *itemTrack) reset() {
	t.dirty = idSet{}
	t.live = map[transcript.BlockID]bool{}
	t.fold = foldState{search: t.fold.search}
}

// bumpAll and bumpNone are build's bump rules: re-render every item, or
// only the ones build must (never issued, or re-nested).
func bumpAll(foldEntry) bool  { return true }
func bumpNone(foldEntry) bool { return false }

// regroup re-lays out p's blocks and reports whether the list must be
// rebuilt: its layout changed other than by new items at its end.
func (t *itemTrack) regroup(p *transcript.Projection) bool {
	_, changed := t.fold.regroup(p.Blocks())
	return changed
}

// visible maps changed block IDs to the entries of the items that show
// them, each once, in first-seen order: a block outside any group is
// itself; a member of an expanded group is itself and its group's header
// (the tally may have changed); a member of a collapsed group is only its
// header, and stops being live itself (the header animates for it); a
// group ID (a live header, from the tick) is its header.
func (t *itemTrack) visible(ids []transcript.BlockID) []foldEntry {
	f := &t.fold
	seen := make(map[transcript.BlockID]bool, len(ids))
	out := make([]foldEntry, 0, len(ids))
	add := func(e foldEntry) {
		if !seen[e.id] {
			seen[e.id] = true
			out = append(out, e)
		}
	}
	for _, id := range ids {
		if gi, ok := f.byID[id]; ok {
			add(foldEntry{kind: entryHeader, id: id, group: gi})
			continue
		}
		gi, ok := f.owner[id]
		if !ok {
			add(foldEntry{kind: entryPlain, id: id, group: -1})
			continue
		}
		if f.expanded(gi) {
			add(foldEntry{kind: entryNested, id: id, group: gi})
		} else {
			delete(t.live, id)
		}
		add(foldEntry{kind: entryHeader, id: f.groups[gi].ID, group: gi})
	}
	return out
}

// upsert builds, each at a new version, the items that show ids (see
// visible). An item built that fold.order does not list yet is one
// blocklist.Upsert appends at the end of the list, so it joins fold.order
// too: regroup compares a new layout against order, and an item listed by
// an upsert (a streaming block's first tick) but missing from order would
// make a later layout that absorbs it look like a plain append, leaving it
// listed. An entry already in order (a regroup just laid it out) is not
// added twice, nor is one build skips over (it is not listed at all). Only
// an entry never issued before this build can be missing from order, so
// only those scan it. Joining order also records its tightness, so it is
// appended before build reads it.
func (t *itemTrack) upsert(p *pane, ids []transcript.BlockID, frame int) []blocklist.Item {
	entries := t.visible(ids)
	for _, e := range entries {
		if t.versions[e.id] != 0 || slices.ContainsFunc(t.fold.order, func(o foldEntry) bool { return o.id == e.id }) {
			continue
		}
		// build lists (and issues a version for) exactly the entries
		// whose data exists.
		if _, _, ok := t.entryData(p, e, frame); ok {
			t.appendOrder(p, e)
		}
	}
	return t.build(p, entries, bumpAll, frame)
}

// appendOrder appends e to the end of fold.order, as blocklist.Upsert
// appends its item at the end of the list.
func (t *itemTrack) appendOrder(p *pane, e foldEntry) {
	prev := false
	if n := len(t.fold.order); n > 0 {
		last := t.fold.order[n-1]
		lb, _ := p.proj.Block(last.id)
		prev = isToolLine(last, lb)
	}
	b, _ := p.proj.Block(e.id)
	t.fold.order, _ = t.fold.appendEntry(t.fold.order, prev, e, b)
}

// layoutItems builds the whole list from entries (a full layout), so the
// live set is rebuilt from scratch: a member hidden since it was last
// built is no longer live.
func (t *itemTrack) layoutItems(p *pane, entries []foldEntry, bump func(foldEntry) bool, frame int) []blocklist.Item {
	t.live = map[transcript.BlockID]bool{}
	return t.build(p, entries, bump, frame)
}

// relistItems builds every item of the current layout (fold.order),
// re-rendering the items showing changed (a header among them when one of
// its members changed, for its tally) and the headers whose shown open
// state flipped (entryItem); every other item keeps its version, so the
// list re-renders only those.
func (t *itemTrack) relistItems(p *pane, changed []transcript.BlockID, frame int) []blocklist.Item {
	bumped := map[transcript.BlockID]bool{}
	for _, e := range t.visible(changed) {
		bumped[e.id] = true
	}
	return t.layoutItems(p, t.fold.order, func(e foldEntry) bool {
		return bumped[e.id]
	}, frame)
}

// build returns an item for each entry whose block still exists.
func (t *itemTrack) build(p *pane, entries []foldEntry, bump func(foldEntry) bool, frame int) []blocklist.Item {
	out := make([]blocklist.Item, 0, len(entries))
	for _, e := range entries {
		if it, ok := t.entryItem(p, e, bump(e), frame); ok {
			out = append(out, it)
		}
	}
	return out
}

// entryItem builds e's item. Its version goes up when bump is set, when
// it was never issued, or when its nesting changed since it was last
// issued (the cached render would keep the old indent). It records
// whether the item is live.
func (t *itemTrack) entryItem(p *pane, e foldEntry, bump bool, frame int) (blocklist.Item, bool) {
	data, live, ok := t.entryData(p, e, frame)
	if !ok {
		return blocklist.Item{}, false
	}
	nested := e.kind == entryNested
	if e.kind == entryHeader {
		open := data.Group.Open
		if was, issued := t.shownOpen[e.id]; !issued || was != open {
			bump = true
		}
		t.shownOpen[e.id] = open
	}
	if bump || t.versions[e.id] == 0 || t.nested[e.id] != nested {
		t.versions[e.id]++
	}
	t.nested[e.id] = nested
	if live {
		t.live[e.id] = true
	} else {
		delete(t.live, e.id)
	}
	return blocklist.Item{ID: string(e.id), Version: t.versions[e.id], Data: data, Tight: t.fold.tight[e.id]}, true
}

// entryData is e's blockData and whether it animates: a header's members
// and open state, or a block's data (nested when e is a member).
func (t *itemTrack) entryData(p *pane, e foldEntry, frame int) (blockData, bool, bool) {
	if e.kind == entryHeader {
		g := t.fold.groups[e.group]
		members, durs := groupMembers(p, g.Members)
		if len(members) == 0 {
			return blockData{}, false, false
		}
		data := blockData{
			Block: transcript.Block{ID: g.ID},
			Frame: frame,
			Group: &groupData{Members: members, Durs: durs, Open: t.fold.expanded(e.group)},
		}
		return data, groupLive(members), true
	}
	b, ok := p.proj.Block(e.id)
	if !ok {
		return blockData{}, false, false
	}
	data := p.data(b, frame)
	data.Nested = e.kind == entryNested
	return data, isLive(b) || data.Thinking, true
}

// groupMembers returns the blocks ids name that are still in p's
// projection, with their measured durations (0 for none).
func groupMembers(p *pane, ids []transcript.BlockID) ([]transcript.Block, []time.Duration) {
	blocks := make([]transcript.Block, 0, len(ids))
	durs := make([]time.Duration, 0, len(ids))
	for _, id := range ids {
		b, ok := p.proj.Block(id)
		if !ok {
			continue
		}
		blocks = append(blocks, b)
		durs = append(durs, p.times.durs[id])
	}
	return blocks, durs
}
