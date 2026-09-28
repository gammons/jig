package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/permcard"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ui/transcript"
)

// permissionHint is the status hint shown when a request arrives while
// the user is typing (spec §6.5, verbatim).
const permissionHint = "⚠ permission pending · esc gp"

// permCtl runs the permission card from the App: which pending request
// it shows and under which block, the focus rule, the card's keys, and
// gp. The requests themselves live in the projection (Pending, and each
// block's Permission); the card only mirrors one of them.
type permCtl struct{ a *App }

// replyFunc is the card's ReplyFunc: a Perms.Reply Cmd.
func replyFunc(a *App) permcard.ReplyFunc {
	return func(id string, r permcard.Reply) tea.Cmd {
		return permReplyCmd(a.ports, id, core.PermissionReply{Kind: replyKind(r.Kind), Message: r.Message})
	}
}

// replyKind maps a card reply to the port's.
func replyKind(k permcard.ReplyKind) core.ReplyKind {
	switch k {
	case permcard.ReplyOnce:
		return core.ReplyOnce
	case permcard.ReplyAlways:
		return core.ReplyAlways
	}
	return core.ReplyDeny
}

// target is the request the card shows: the one being answered with a
// message (while its request is still pending), else the selected
// block's, else the first pending request that has a block.
func (p permCtl) target() *transcript.PendingPermission {
	a := p.a
	if req := a.w.card.Request(); req != nil && a.w.card.Typing() {
		if pp := p.shownOn(a.w.cardAt.block); pp != nil && pp.RequestID == req.ID {
			return pp
		}
	}
	if it, ok := a.w.list.Selected(); ok {
		if pp := p.shownOn(transcript.BlockID(it.ID)); pp != nil {
			return pp
		}
	}
	for _, pp := range a.sess.proj.Pending() {
		if shown := p.shownOn(pp.Block); shown != nil {
			return shown
		}
	}
	return nil
}

// shownOn is the request block id shows, or nil.
func (p permCtl) shownOn(id transcript.BlockID) *transcript.PendingPermission {
	if id == "" {
		return nil
	}
	b, ok := p.a.sess.proj.Block(id)
	if !ok {
		return nil
	}
	return b.Permission
}

// sync points the card at its target and re-renders the blocks whose
// card changed: the one it left and the one it is on (a new request, a
// keystroke in the deny input, a restyle, or a new list width).
func (p permCtl) sync() {
	a := p.a
	var req *permcard.Request
	var blk transcript.BlockID
	if pp := p.target(); pp != nil {
		req = &permcard.Request{
			ID:       pp.RequestID,
			Tool:     ansi.SanitizeLine(pp.Tool),
			Subject:  ansi.SanitizeLine(pp.Subject),
			Subagent: ansi.SanitizeLine(pp.Subagent),
		}
		blk = pp.Block
	}
	a.w.card.Set(req)
	if a.view.listW != a.w.cardAt.width {
		a.w.card.SetWidth(a.view.listW)
	}
	next := cardKey{block: blk, ver: a.w.card.Version(), width: a.view.listW}
	if next == a.w.cardAt {
		return
	}
	old := a.w.cardAt.block
	a.w.cardAt = next
	ids := make([]transcript.BlockID, 0, 2)
	if old != "" && old != blk {
		ids = append(ids, old)
	}
	if blk != "" {
		ids = append(ids, blk)
	}
	a.flush(ids)
}

// requested applies the focus rule (spec §6.5) to request e, now
// pending: with an empty prompt the App enters NORMAL and selects the
// card's block (a descendant's request shows on its owning Subagent
// block); while the user is typing (or picking) it stays put and hints.
func (p permCtl) requested(e event.PermissionRequested) tea.Cmd {
	a := p.a
	if !slices.ContainsFunc(a.sess.proj.Pending(), func(pp transcript.PendingPermission) bool { return pp.RequestID == e.RequestID }) {
		return nil
	}
	if a.mode == modePicker || a.w.prompt.Value() != "" {
		a.view.hint = permissionHint
		return nil
	}
	cmd := a.setMode(modeNormal)
	if it, ok := a.w.list.Selected(); ok && p.shownOn(transcript.BlockID(it.ID)) != nil {
		return cmd
	}
	blocks := p.pendingBlocks()
	if len(blocks) == 0 {
		return cmd
	}
	return tea.Batch(cmd, p.selectBlock(blocks[0]))
}

// pendingBlocks lists the blocks showing a pending request, in request
// order, each once.
func (p permCtl) pendingBlocks() []transcript.BlockID {
	var out []transcript.BlockID
	for _, pp := range p.a.sess.proj.Pending() {
		if pp.Block != "" && !slices.Contains(out, pp.Block) && p.shownOn(pp.Block) != nil {
			out = append(out, pp.Block)
		}
	}
	return out
}

// next selects the pending block after the selected one (gp), wrapping;
// from a block with no request, the first.
func (p permCtl) next() tea.Cmd {
	blocks := p.pendingBlocks()
	if len(blocks) == 0 {
		return nil
	}
	i := -1
	if it, ok := p.a.w.list.Selected(); ok {
		i = slices.Index(blocks, transcript.BlockID(it.ID))
	}
	return p.selectBlock(blocks[(i+1)%len(blocks)])
}

// selectBlock selects id in the transcript, following it with the
// details split when open.
func (p permCtl) selectBlock(id transcript.BlockID) tea.Cmd {
	a := p.a
	before, _ := a.w.list.Selected()
	a.w.list.Select(string(id))
	return normalKeys{a}.syncDetails(before)
}

// onCard reports whether the selected block carries the card.
func (p permCtl) onCard() bool {
	a := p.a
	it, ok := a.w.list.Selected()
	return ok && a.w.card.Request() != nil && transcript.BlockID(it.ID) == a.w.cardAt.block
}

// key sends k to the card.
func (p permCtl) key(k tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	p.a.w.card, cmd = p.a.w.card.Update(k)
	return cmd
}
