package ui

import (
	"maps"
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
// search forces every group open, the groups holding a member awaiting
// permission (awaiting), and the layout last computed (order), so
// regroup can tell an append from a restructure.
type foldState struct {
	groups   []transcript.Group
	owner    map[transcript.BlockID]int
	byID     map[transcript.BlockID]int
	open     map[transcript.BlockID]bool
	search   bool
	awaiting map[transcript.BlockID]bool
	order    []foldEntry
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
	held := f.awaiting
	f.awaiting = map[transcript.BlockID]bool{}
	for _, b := range blocks {
		if gi, ok := f.owner[b.ID]; ok && b.State == transcript.StateAwaiting {
			f.awaiting[f.groups[gi].ID] = true
		}
	}
	entries := f.layout(blocks)
	// A hold starting reveals members that already exist, several at
	// once, so it is a restructure even when they land at the list's end.
	appended := maps.Equal(held, f.awaiting) && len(entries) >= len(f.order) && slices.Equal(entries[:len(f.order)], f.order)
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
// it, a search is applied, or a member awaits permission (its card must
// never be hidden).
func (f *foldState) expanded(gi int) bool {
	id := f.groups[gi].ID
	return f.open[id] || f.search || f.awaiting[id]
}

// toggle flips the user's state of the group id heads, or collapses the
// group id is a member of, and returns that group's ID. It reports false
// for any other block. A group held open (a search, a pending permission)
// keeps showing until the hold ends; the flipped state applies then.
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
