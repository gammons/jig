package ui

import (
	"slices"
	"time"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// paneKind is what a pane shows. Only paneTranscript exists for now;
// later tasks add more.
type paneKind int

const (
	paneTranscript paneKind = iota
)

// listSize is the transcript list's applied size and the debounce state
// for a pending width change (see App.relayout/applyListWidth).
type listSize struct {
	listW, listH, pendingW, resizeGen int
}

// pane is one transcript view's projection, blocklist, and render
// bookkeeping: the transcript projection, the blocklist widget, the
// tool-call-group layout (track), tool/reasoning durations (times), and
// the applied/pending list size (sz). Only the fields the current tasks
// use are declared; later tasks (kids/column panes) add more.
type pane struct {
	kind    paneKind
	session core.SessionID
	proj    *transcript.Projection
	list    blocklist.Model
	track   itemTrack
	times   blockTimes
	sz      listSize
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
