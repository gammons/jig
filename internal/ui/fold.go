package ui

import (
	"slices"

	"github.com/gammons/jig/internal/ui/transcript"
)

// entryKind says how the transcript list shows a block or group.
type entryKind int

const (
	entryPlain  entryKind = iota // a block outside any group
	entryHeader                  // a group's one-line header
	entryNested                  // a member of an expanded group, indented
)

// foldEntry is one item of the transcript list's layout: a plain block,
// a group header (id is the group's ID), or a nested member. group
// indexes foldState.groups for a header or member, and is -1 otherwise.
type foldEntry struct {
	kind  entryKind
	id    transcript.BlockID
	group int
}

// foldState is the root transcript's tool-call groups (tool-call groups
// spec §6.1): the groups, which group each member is in (owner) and each
// group's index (byID), the groups the user expanded (open), whether a
// search forces every group open, and the layout last computed (order),
// so regroup can tell an append from a restructure.
type foldState struct {
	groups []transcript.Group
	owner  map[transcript.BlockID]int
	byID   map[transcript.BlockID]int
	open   map[transcript.BlockID]bool
	search bool
	order  []foldEntry
}

// regroup recomputes the groups and the layout of blocks, keeps the
// layout as order, and reports whether it changed other than by new
// entries at its end (the list must then be rebuilt, not upserted).
func (f *foldState) regroup(blocks []transcript.Block) ([]foldEntry, bool) {
	f.groups = transcript.Groups(blocks)
	f.owner = make(map[transcript.BlockID]int)
	f.byID = make(map[transcript.BlockID]int, len(f.groups))
	for gi, g := range f.groups {
		f.byID[g.ID] = gi
		for _, id := range g.Members {
			f.owner[id] = gi
		}
	}
	entries := f.layout(blocks)
	appended := len(entries) >= len(f.order) && slices.Equal(entries[:len(f.order)], f.order)
	f.order = entries
	return entries, !appended
}

// layout lays blocks out: a member's group shows as its header (at its
// first call) and, while expanded, the members nested under it.
func (f *foldState) layout(blocks []transcript.Block) []foldEntry {
	out := make([]foldEntry, 0, len(blocks))
	for _, b := range blocks {
		gi, member := f.owner[b.ID]
		if !member {
			out = append(out, foldEntry{kind: entryPlain, id: b.ID, group: -1})
			continue
		}
		g := f.groups[gi]
		if b.ID == g.Members[0] {
			out = append(out, foldEntry{kind: entryHeader, id: g.ID, group: gi})
		}
		if f.expanded(gi) {
			out = append(out, foldEntry{kind: entryNested, id: b.ID, group: gi})
		}
	}
	return out
}

// expanded reports whether group gi shows its members: the user opened
// it, or a search is applied.
func (f *foldState) expanded(gi int) bool {
	return f.open[f.groups[gi].ID] || f.search
}

// toggle flips the user's state of the group id heads, or collapses the
// group id is a member of, and returns that group's ID. It reports false
// for any other block. A group held open (a search) keeps showing until
// the hold ends; the flipped state applies then.
func (f *foldState) toggle(id transcript.BlockID) (transcript.BlockID, bool) {
	if f.open == nil {
		f.open = map[transcript.BlockID]bool{}
	}
	if _, ok := f.byID[id]; ok {
		f.open[id] = !f.open[id]
		return id, true
	}
	gi, ok := f.owner[id]
	if !ok {
		return "", false
	}
	g := f.groups[gi].ID
	f.open[g] = false
	return g, true
}

// groupOf returns the ID of the group member id belongs to.
func (f *foldState) groupOf(id transcript.BlockID) (transcript.BlockID, bool) {
	gi, ok := f.owner[id]
	if !ok {
		return "", false
	}
	return f.groups[gi].ID, true
}

// members returns group id's member IDs, in display order.
func (f *foldState) members(id transcript.BlockID) ([]transcript.BlockID, bool) {
	gi, ok := f.byID[id]
	if !ok {
		return nil, false
	}
	return f.groups[gi].Members, true
}
