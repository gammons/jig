package ui

import "github.com/gammons/jig/internal/ui/transcript"

// itemTrack is the transcript list's bookkeeping, kept apart from
// sessionState (archtest's struct limits): the item version issued per
// ID (versions only ever grow, so an ID shown again never matches a stale
// cached render), the dirty streaming blocks (rendered on the next
// streamTick), the live items, whose spinner each tick advances, and the
// tool-call groups (fold).
type itemTrack struct {
	versions map[transcript.BlockID]int
	dirty    idSet
	live     map[transcript.BlockID]bool
	fold     foldState
}

// newItemTrack returns an empty itemTrack.
func newItemTrack() itemTrack {
	return itemTrack{
		versions: map[transcript.BlockID]int{},
		live:     map[transcript.BlockID]bool{},
	}
}

// reset clears what a new projection invalidates; versions survive.
func (t *itemTrack) reset() {
	t.dirty = idSet{}
	t.live = map[transcript.BlockID]bool{}
	t.fold = foldState{}
}
