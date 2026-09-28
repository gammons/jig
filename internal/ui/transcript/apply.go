package transcript

import (
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// Apply folds one live event into the projection and returns the IDs of
// the blocks it changed, in block order, or nil if none. Events from other
// roots are ignored; events from descendant sessions go to applyDescendant.
func (p *Projection) Apply(ev event.Event) []BlockID {
	if ev.Root() != p.root {
		return nil
	}
	if ev.Session() != p.root {
		return p.applyDescendant(ev)
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
// events never create blocks; they update the subagent block that
// p.owner[ev.Session()] names. Nothing fills owner yet, so every
// descendant event is ignored.
func (p *Projection) applyDescendant(event.Event) []BlockID {
	return nil
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
	p.open[msg] = b
	return []BlockID{b.ID}
}

// startTool shows call as running. A block that already exists for the
// call ID (a resumed call) is reused rather than duplicated.
func (p *Projection) startTool(msg core.MessageID, call core.ToolCall) []BlockID {
	b, ok := p.list.get(BlockID(call.ID))
	if !ok {
		p.list.add(newToolBlock(msg, call, StateRunning))
		return []BlockID{BlockID(call.ID)}
	}
	b.Call = &call
	b.Result = nil
	b.State = StateRunning
	b.Version++
	return []BlockID{b.ID}
}

// finishTool records r on its call's block.
func (p *Projection) finishTool(r core.ToolResult) []BlockID {
	b, ok := p.list.get(BlockID(r.CallID))
	if !ok {
		return nil
	}
	b.finish(r)
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
