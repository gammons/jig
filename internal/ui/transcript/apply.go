package transcript

import (
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// Apply folds one live event into the projection and returns the IDs of
// the blocks it changed, in block order, or nil if none: a reasoning block
// whose thinking a new block ended comes first. Events from other roots
// are ignored; events from descendant sessions go to applyDescendant.
// Spawns and permission events are handled the same from any session.
func (p *Projection) Apply(ev event.Event) []BlockID {
	p.list.ended = nil
	ids := applyEvent(p, ev)
	if len(p.list.ended) == 0 {
		return ids
	}
	ended := p.list.ended
	p.list.ended = nil
	return append(ended, ids...)
}

// applyEvent is Apply without the ended-thinking report. A free function,
// not a method, to stay under the package's per-type method budget.
func applyEvent(p *Projection, ev event.Event) []BlockID {
	if ev.Root() != p.root {
		return nil
	}
	if !inSubtree(p, ev.Session()) {
		return nil
	}
	switch e := ev.(type) {
	case event.SubagentSpawned:
		return p.tree.spawn(&p.list, p.self, e)
	case event.PermissionRequested:
		return p.perms.request(&p.list, &p.tree, p.self, e)
	case event.PermissionResolved:
		return p.perms.resolve(&p.list, e.RequestID)
	}
	if ev.Session() != p.self {
		return applyDescendant(p, ev)
	}
	switch e := ev.(type) {
	case event.TextDelta:
		return p.delta(e.MessageID, KindText, e.Text)
	case event.ReasoningDelta:
		return p.delta(e.MessageID, KindReasoning, e.Text)
	case event.ToolCallStarted:
		return p.startTool(e.MessageID, e.Call)
	case event.ToolCallFinished:
		return p.finishTool(e.Result)
	case event.StepFinished:
		return p.endRun(func(b *Block) bool { return b.MessageID == e.MessageID }, "")
	case event.RunFinished:
		return p.endRun(func(*Block) bool { return true }, "")
	case event.RunFailed:
		return p.runFailed(e.Err)
	}
	// MessageStarted needs no change: deltas open a message's first block.
	return nil
}

// applyDescendant handles an event from a session under root. Descendant
// events never create blocks; their tool calls update the subagent block
// that owns the session. Events from sessions with no known owner are
// ignored. A free function, not a method, to stay under the package's
// per-type method budget.
func applyDescendant(p *Projection, ev event.Event) []BlockID {
	return p.tree.descendant(&p.list, ev)
}

// delta appends text to msg's open block of kind, or opens a new one when
// msg has none or its open block is of the other kind. The earlier block
// stays Streaming until its step ends. A text delta holding the max-steps
// notice (R18) becomes a notice instead.
func (p *Projection) delta(msg core.MessageID, kind Kind, text string) []BlockID {
	if kind == KindText && isMaxStepsNotice(text) {
		return []BlockID{p.addNotice(msg, "", strings.TrimSpace(text), LevelInfo).ID}
	}
	if b := p.open[msg]; b != nil && b.Kind == kind {
		b.Text += text
		b.Version++
		return []BlockID{b.ID}
	}
	b := p.addPart(msg, kind, text, true)
	b.Thinking = kind == KindReasoning
	p.open[msg] = b
	return []BlockID{b.ID}
}

// startTool shows call as running. A block that already exists for the
// call ID (a resumed call) is reused rather than duplicated.
func (p *Projection) startTool(msg core.MessageID, call core.ToolCall) []BlockID {
	b, ok := p.list.get(toolBlockID(call.ID))
	if !ok {
		p.list.add(newToolBlock(msg, call, StateRunning))
		return []BlockID{toolBlockID(call.ID)}
	}
	call = cloneCall(call)
	b.Call = &call
	b.Result = nil
	b.State = StateRunning
	b.Version++
	return []BlockID{b.ID}
}

// finishTool records r on its call's block and observes it for derived
// state.
func (p *Projection) finishTool(r core.ToolResult) []BlockID {
	b, ok := p.list.get(toolBlockID(r.CallID))
	if !ok {
		return nil
	}
	b.finish(r)
	p.derived.observe(b)
	b.Version++
	return []BlockID{b.ID}
}

// runFailed ends the run, settling live calls as cancelled (Err
// "cancelled") or errored, and appends a dim "cancelled" or a red
// "run failed: <err>" notice (R18).
func (p *Projection) runFailed(err string) []BlockID {
	all := func(*Block) bool { return true }
	if err == noticeCancelled {
		ids := p.endRun(all, StateCancelled)
		return append(ids, p.addNotice("", "", noticeCancelled, LevelInfo).ID)
	}
	ids := p.endRun(all, StateError)
	return append(ids, p.addNotice("", "", noticeRunFailed+": "+err, LevelError).ID)
}

// endRun clears Streaming on the matching blocks and closes their
// messages' open blocks. When settle is set, it also moves live (running
// or awaiting-permission) tool and subagent blocks to settle.
func (p *Projection) endRun(match func(*Block) bool, settle ToolState) []BlockID {
	var ids []BlockID
	for _, b := range p.list.blocks {
		if !match(b) {
			continue
		}
		changed := false
		if b.Streaming {
			b.Streaming = false
			delete(p.open, b.MessageID)
			changed = true
		}
		if b.Thinking {
			b.Thinking = false
			changed = true
		}
		if settle != "" && (b.State == StateRunning || b.State == StateAwaiting) {
			b.State = settle
			b.Permission = nil
			changed = true
		}
		if changed {
			b.Version++
			ids = append(ids, b.ID)
		}
	}
	return ids
}
