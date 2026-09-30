package ui

import (
	"slices"
	"time"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// paneKind is what a pane shows: a transcript view, or a details pane in
// the column.
type paneKind int

const (
	paneTranscript paneKind = iota
	paneDetails
)

// listSize is the transcript list's applied size and the debounce state
// for a pending width change (see App.relayout/applyListWidth).
type listSize struct {
	listW, listH, pendingW, resizeGen int
}

// pane is one transcript view's projection, blocklist, and render
// bookkeeping — or, for kind paneDetails, a details body shown in the
// column: the transcript projection, the blocklist widget, the
// tool-call-group layout (track), tool/reasoning durations (times), and
// the applied/pending list size (sz). Only the fields the current tasks
// use are declared; later tasks (kids/column panes) add more.
type pane struct {
	kind     paneKind
	session  core.SessionID
	proj     *transcript.Projection
	list     blocklist.Model
	track    itemTrack
	times    blockTimes
	sz       listSize
	title    string // a kid pane's breadcrumb text, or a details pane's header
	load     kidLoad
	body     details.Model      // paneDetails
	owner    *pane              // paneDetails: the pane whose block it shows
	forBlock transcript.BlockID // paneDetails: that block
	buildGen int                // paneDetails: async build generation (detailsMsg staleness)
}

// kidLoad is a kid pane's on-demand history load (spec §4.2): while
// loading is set, live events aren't applied to the pane's projection but
// appended to buffered instead, and replayed once the load resolves. gen
// guards a stale load's result from applying after a newer one started.
type kidLoad struct {
	loading  bool
	buffered []event.Event
	gen      int
}

// newTranscriptPane builds a paneTranscript pane over proj, with its own
// blocklist styled from set.
func newTranscriptPane(proj *transcript.Projection, r *renderer, set *theme.Set) *pane {
	p := &pane{
		kind: paneTranscript, session: proj.Self(), proj: proj,
		list:  blocklist.New(r.render, blocklist.WithStyles(set.Blocklist)),
		track: newItemTrack(),
	}
	p.resetBlocks()
	return p
}

// resetBlocks clears the per-block state a new projection invalidates.
func (p *pane) resetBlocks() {
	p.times = blockTimes{starts: map[transcript.BlockID]time.Time{}, durs: map[transcript.BlockID]time.Duration{}}
	p.track.reset()
}

// withDirty prepends the dirty blocks (emptying the set) to ids, without
// repeating any.
func (p *pane) withDirty(ids []transcript.BlockID) []transcript.BlockID {
	if len(p.track.dirty.order) == 0 {
		return ids
	}
	out := p.track.dirty.take()
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// timeTools records a tool call's start, or its duration on finish.
func (p *pane) timeTools(ev event.Event, ids []transcript.BlockID, now time.Time) {
	for _, id := range ids {
		if _, ok := ev.(event.ToolCallStarted); ok {
			p.times.starts[id] = now
			continue
		}
		if start, ok := p.times.starts[id]; ok {
			p.times.durs[id] = now.Sub(start)
			delete(p.times.starts, id)
		}
	}
}

// data builds b's blockData from the pane's current render state. frame
// is the root run's spinner frame.
func (p *pane) data(b transcript.Block, frame int) blockData {
	return blockData{
		Block:    b,
		Duration: p.times.durs[b.ID],
		Frame:    frame,
		Thinking: b.Thinking,
	}
}

// allItems regroups p's blocks and builds every item of the layout, in
// display order, each at a new version.
func (p *pane) allItems(frame int) []blocklist.Item {
	entries, _ := p.track.fold.regroup(p.proj.Blocks())
	return p.track.layoutItems(p, entries, bumpAll, frame)
}

// items builds, each at a new version, the items that show the blocks
// ids (a member of a collapsed group shows as its header), tracks which
// of them are live, and records the new ones in the fold layout.
func (p *pane) items(ids []transcript.BlockID, frame int) []blocklist.Item {
	return p.track.upsert(p, ids, frame)
}

// tick advances the pane's live blocks to re-render: every dirty one,
// then every live one (App.onTick's per-pane sibling to
// sessionState.tick).
func (p *pane) tick() []transcript.BlockID {
	ids := p.track.dirty.take()
	live := make([]transcript.BlockID, 0, len(p.track.live))
	for id := range p.track.live {
		live = append(live, id)
	}
	slices.Sort(live)
	for _, id := range live {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}
