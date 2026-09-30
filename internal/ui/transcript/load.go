package transcript

import (
	"strings"

	"github.com/gammons/jig/internal/core"
)

// Load replaces every block with the projection of msgs, the root
// session's stored history in order. A block whose ID existed before Load
// gets a Version above its old one, so caches keyed by (ID, Version) never
// serve a stale rendering; notice IDs keep counting across loads.
//
// State that stored messages don't record survives: the owners of live
// descendant sessions (joined by the children loaded subagent blocks name)
// and unresolved permission requests, which are shown again on their
// blocks.
func (p *Projection) Load(msgs []core.Message) {
	old := make(map[BlockID]int, len(p.list.blocks))
	for _, b := range p.list.blocks {
		old[b.ID] = b.Version
	}
	reset(p)
	for _, m := range msgs {
		if m.Role == core.RoleUser {
			p.list.add(userBlock(m))
			continue
		}
		p.loadAssistant(m)
	}
	p.tree.seed(&p.list)
	p.perms.reattach(&p.list)
	for _, b := range p.list.blocks {
		if v, ok := old[b.ID]; ok {
			b.Version = v + 1
		}
	}
}

// userBlock projects a user message: its text parts joined with "\n", and
// its attachments' paths.
func userBlock(m core.Message) *Block {
	var texts, paths []string
	for _, part := range m.Parts {
		switch {
		case part.Kind == core.PartText:
			texts = append(texts, part.Text)
		case part.Kind == core.PartAttachment && part.Attachment != nil:
			paths = append(paths, part.Attachment.Path)
		}
	}
	return &Block{
		ID: BlockID("u/" + string(m.ID)), Kind: KindUser, MessageID: m.ID,
		Text: strings.Join(texts, "\n"), Attachments: paths,
	}
}

// loadAssistant projects an assistant message's parts in order, then
// settles calls the message never answered: an interrupted message cancels
// them and ends with a "cancelled" notice; a failed one errors them and ends
// with a "run failed" notice. Other unanswered calls stay pending.
func (p *Projection) loadAssistant(m core.Message) {
	var calls []*Block
	for _, part := range m.Parts {
		switch part.Kind {
		case core.PartText, core.PartReasoning:
			p.loadText(m.ID, part)
		case core.PartCompaction:
			p.addNotice(m.ID, titleCompaction, part.Text, LevelInfo)
		case core.PartToolCall:
			if part.Call != nil {
				calls = append(calls, p.list.add(newToolBlock(m.ID, *part.Call, StatePending)))
			}
		case core.PartToolResult:
			if part.Result == nil {
				continue
			}
			if b, ok := p.list.get(toolBlockID(part.Result.CallID)); ok {
				b.finish(*part.Result)
				p.derived.observe(b)
			}
		}
	}
	switch m.Status {
	case core.StatusInterrupted:
		settle(calls, StateCancelled)
		p.addNotice(m.ID, "", noticeCancelled, LevelInfo)
	case core.StatusFailed:
		settle(calls, StateError)
		p.addNotice(m.ID, "", noticeRunFailed, LevelError)
	}
}

// loadText adds a text or reasoning part as its own block, or as a notice
// when it is the max-steps notice (R18). Empty parts add nothing.
func (p *Projection) loadText(msg core.MessageID, part core.Part) {
	switch {
	case part.Text == "":
	case part.Kind == core.PartText && isMaxStepsNotice(part.Text):
		p.addNotice(msg, "", strings.TrimSpace(part.Text), LevelInfo)
	case part.Kind == core.PartText:
		p.addPart(msg, KindText, part.Text, false)
	default:
		p.addPart(msg, KindReasoning, part.Text, false)
	}
}

// settle sets every still-pending block in bs to state.
func settle(bs []*Block, state ToolState) {
	for _, b := range bs {
		if b.State == StatePending {
			b.State = state
		}
	}
}
