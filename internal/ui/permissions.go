package ui

import (
	"slices"
	"time"

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

// cardArmDelay is how long the card's keys stay disarmed after a new
// request or after the selection moves onto the card, so a key meant for
// the prompt or the list can't answer it by accident.
const cardArmDelay = 400 * time.Millisecond

// cardArmMsg arms the card, if gen is still the latest disarm.
type cardArmMsg struct{ gen int }

// cardArm is the arming state: the latest disarm (gen), and the request
// and selected block the last sync saw, to notice a new request or the
// selection arriving on the card.
type cardArm struct {
	gen int
	req string
	sel transcript.BlockID
}

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
	if it, ok := a.sess.main.list.Selected(); ok {
		if pp := p.shownOn(transcript.BlockID(it.ID)); pp != nil {
			return pp
		}
	}
	for _, pp := range a.sess.main.proj.Pending() {
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
	b, ok := p.a.sess.main.proj.Block(id)
	if !ok {
		return nil
	}
	return b.Permission
}

// sync points the card at its target, disarms it on a new request or when
// the selection arrives on it (returning the arming tick), and re-renders
// the blocks whose card changed: the one it left and the one it is on (a
// new request, a keystroke in the deny input, arming, a restyle, or a new
// list width).
func (p permCtl) sync() tea.Cmd {
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
	cmd := p.guard(req, blk)
	if a.sess.main.sz.listW != a.w.cardAt.width {
		a.w.card.SetWidth(a.sess.main.sz.listW)
	}
	next := cardKey{block: blk, ver: a.w.card.Version(), width: a.sess.main.sz.listW}
	if next == a.w.cardAt {
		return cmd
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
	return cmd
}

// guard disarms the card showing req under blk when req is new or the
// selection has just moved onto blk, and returns the tick that arms it
// cardArmDelay later (App clock); a later disarm supersedes it.
func (p permCtl) guard(req *permcard.Request, blk transcript.BlockID) tea.Cmd {
	a := p.a
	var sel transcript.BlockID
	if it, ok := a.sess.main.list.Selected(); ok {
		sel = transcript.BlockID(it.ID)
	}
	id := ""
	if req != nil {
		id = req.ID
	}
	g := &a.w.arm
	arrived := blk != "" && sel == blk && g.sel != sel
	fresh := id != "" && id != g.req
	g.req, g.sel = id, sel
	if !fresh && !arrived {
		return nil
	}
	return p.disarm()
}

// disarm disarms the card and returns the tick that arms it cardArmDelay
// later (App clock); a later disarm supersedes it.
func (p permCtl) disarm() tea.Cmd {
	a := p.a
	a.w.card.SetArmed(false)
	a.w.arm.gen++
	return a.after(cardArmDelay, cardArmMsg{gen: a.w.arm.gen})
}

// armed arms the card when msg is the latest disarm's tick.
func (p permCtl) armed(msg cardArmMsg) {
	if msg.gen == p.a.w.arm.gen {
		p.a.w.card.SetArmed(true)
	}
}

// requested applies the focus rule (spec §6.5) to request e, now
// pending: with an empty prompt the App enters NORMAL and selects the
// card's block (a descendant's request shows on its owning Subagent
// block); while the user is typing (or picking) it stays put and hints.
// Switching the mode disarms the card, even one already armed on the
// selected block for an older request: a key typed for the prompt must
// not answer it.
func (p permCtl) requested(e event.PermissionRequested) tea.Cmd {
	a := p.a
	if !slices.ContainsFunc(a.sess.main.proj.Pending(), func(pp transcript.PendingPermission) bool { return pp.RequestID == e.RequestID }) {
		return nil
	}
	if a.mode == modePicker || a.w.prompt.Value() != "" {
		a.view.hint = permissionHint
		return nil
	}
	switched := a.mode != modeNormal
	cmd := a.setMode(modeNormal)
	if switched {
		cmd = tea.Batch(cmd, p.disarm())
	}
	if it, ok := a.sess.main.list.Selected(); ok && p.shownOn(transcript.BlockID(it.ID)) != nil {
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
	for _, pp := range p.a.sess.main.proj.Pending() {
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
	if it, ok := p.a.sess.main.list.Selected(); ok {
		i = slices.Index(blocks, transcript.BlockID(it.ID))
	}
	return p.selectBlock(blocks[(i+1)%len(blocks)])
}

// selectBlock selects id in the transcript, following it with the
// details split when open.
func (p permCtl) selectBlock(id transcript.BlockID) tea.Cmd {
	a := p.a
	before, _ := a.sess.main.list.Selected()
	a.sess.main.list.Select(string(id))
	return normalKeys{a}.syncDetails(before)
}

// onCard reports whether the selected block carries the card.
func (p permCtl) onCard() bool {
	a := p.a
	it, ok := a.sess.main.list.Selected()
	return ok && a.w.card.Request() != nil && transcript.BlockID(it.ID) == a.w.cardAt.block
}

// key sends k to the card.
func (p permCtl) key(k tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	p.a.w.card, cmd = p.a.w.card.Update(k)
	return cmd
}
