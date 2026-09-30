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

// target is the request the card shows (spec §4.3), in order: the one
// being answered with a message (while its request is still pending),
// else the focused pane's selected block's, else the focused pane's
// first pending request with a block, else main's first pending request
// with a block. It returns the pane the request is shown in, or nil,nil
// when there is none.
func (p permCtl) target() (*pane, *transcript.PendingPermission) {
	a := p.a
	if req := a.w.card.Request(); req != nil && a.w.card.Typing() {
		if pp := p.shownOn(a.w.cardAt.pane, a.w.cardAt.block); pp != nil && pp.RequestID == req.ID {
			return a.w.cardAt.pane, pp
		}
	}
	if fp := columnFocused(a); fp.kind == paneTranscript {
		if it, ok := fp.list.Selected(); ok {
			if pp := p.shownOn(fp, transcript.BlockID(it.ID)); pp != nil {
				return fp, pp
			}
		}
		for _, pp := range fp.proj.Pending() {
			if shown := p.shownOn(fp, pp.Block); shown != nil {
				return fp, shown
			}
		}
	}
	main := a.sess.main
	for _, pp := range main.proj.Pending() {
		if shown := p.shownOn(main, pp.Block); shown != nil {
			return main, shown
		}
	}
	return nil, nil
}

// shownOn is the request pane's block id shows, or nil.
func (p permCtl) shownOn(pane *pane, id transcript.BlockID) *transcript.PendingPermission {
	if pane == nil || id == "" {
		return nil
	}
	b, ok := pane.proj.Block(id)
	if !ok {
		return nil
	}
	return b.Permission
}

// sync points the card at its target, disarms it on a new request or when
// the selection arrives on it (returning the arming tick), and re-renders
// the blocks whose card changed: the one it left and the one it is on (a
// new request, a keystroke in the deny input, arming, a restyle, or a new
// list width). It flushes the old pane's block and the new pane's block
// through flushPane, so the card is never drawn twice.
func (p permCtl) sync() tea.Cmd {
	a := p.a
	var req *permcard.Request
	var blk transcript.BlockID
	pane, pp := p.target()
	if pp != nil {
		req = &permcard.Request{
			ID:       pp.RequestID,
			Tool:     ansi.SanitizeLine(pp.Tool),
			Subject:  ansi.SanitizeLine(pp.Subject),
			Subagent: ansi.SanitizeLine(pp.Subagent),
		}
		blk = pp.Block
	}
	a.w.card.Set(req)
	cmd := p.guard(pane, req, blk)
	width := a.sess.main.sz.listW
	if pane != nil {
		width = pane.sz.listW
	}
	if width != a.w.cardAt.width {
		a.w.card.SetWidth(width)
	}
	next := cardKey{pane: pane, block: blk, ver: a.w.card.Version(), width: width}
	if next == a.w.cardAt {
		return cmd
	}
	old := a.w.cardAt
	a.w.cardAt = next
	if old.pane != nil && (old.pane != pane || old.block != blk) {
		a.flushPane(old.pane, []transcript.BlockID{old.block})
	}
	if pane != nil && blk != "" {
		a.flushPane(pane, []transcript.BlockID{blk})
	}
	return cmd
}

// guard disarms the card showing req under blk in pane when req is new or
// the selection has just moved onto blk, and returns the tick that arms
// it cardArmDelay later (App clock); a later disarm supersedes it.
func (p permCtl) guard(pane *pane, req *permcard.Request, blk transcript.BlockID) tea.Cmd {
	a := p.a
	var sel transcript.BlockID
	if pane != nil {
		if it, ok := pane.list.Selected(); ok {
			sel = transcript.BlockID(it.ID)
		}
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

// requested applies the focus rule (spec §4.3, §6.5) to request e, now
// pending. When the column's top pane is a transcript pane showing e's
// own session, focus moves to the column and that pane selects the
// request's tool block, whatever the mode or prompt (a descendant's live
// view is always current). Otherwise the rule is unchanged, applied to
// main: with an empty prompt the App enters NORMAL and selects the
// card's block (a descendant's request shows on its owning Subagent
// block); while the user is typing (or picking) it stays put and hints.
// Switching the mode disarms the card, even one already armed on the
// selected block for an older request: a key typed for the prompt must
// not answer it.
func (p permCtl) requested(e event.PermissionRequested) tea.Cmd {
	a := p.a
	if top := columnTop(a); top != nil && top.kind == paneTranscript && top.session == e.Session() {
		if !slices.ContainsFunc(top.proj.Pending(), func(pp transcript.PendingPermission) bool { return pp.RequestID == e.RequestID }) {
			return nil
		}
		setFocus(a, focusColumn)
		return p.selectBlock(top, transcript.BlockID("t/"+e.Call.ID))
	}
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
	main := a.sess.main
	if it, ok := main.list.Selected(); ok && p.shownOn(main, transcript.BlockID(it.ID)) != nil {
		return cmd
	}
	blocks := p.pendingBlocks(main)
	if len(blocks) == 0 {
		return cmd
	}
	return tea.Batch(cmd, p.selectBlock(main, blocks[0]))
}

// pendingBlocks lists pane's blocks showing a pending request, in
// request order, each once.
func (p permCtl) pendingBlocks(pane *pane) []transcript.BlockID {
	var out []transcript.BlockID
	for _, pp := range pane.proj.Pending() {
		if pp.Block != "" && !slices.Contains(out, pp.Block) && p.shownOn(pane, pp.Block) != nil {
			out = append(out, pp.Block)
		}
	}
	return out
}

// next selects the pending block after the focused pane's selected one
// (gp), wrapping; from a block with no request, the first. Nothing on a
// pane that isn't a transcript pane (a details pane has no requests).
func (p permCtl) next() tea.Cmd {
	fp := columnFocused(p.a)
	if fp.kind != paneTranscript {
		return nil
	}
	blocks := p.pendingBlocks(fp)
	if len(blocks) == 0 {
		return nil
	}
	i := -1
	if it, ok := fp.list.Selected(); ok {
		i = slices.Index(blocks, transcript.BlockID(it.ID))
	}
	return p.selectBlock(fp, blocks[(i+1)%len(blocks)])
}

// selectBlock selects id in pane's transcript, following it with the
// column's single details entry when pane is main.
func (p permCtl) selectBlock(pane *pane, id transcript.BlockID) tea.Cmd {
	before, _ := pane.list.Selected()
	pane.list.Select(string(id))
	if pane != p.a.sess.main {
		return nil
	}
	return normalKeys(p).syncDetails(before)
}

// onCard reports whether the focused pane's selected block carries the
// card.
func (p permCtl) onCard() bool {
	a := p.a
	fp := columnFocused(a)
	if fp.kind != paneTranscript {
		return false
	}
	it, ok := fp.list.Selected()
	return ok && a.w.card.Request() != nil && a.w.cardAt.pane == fp && transcript.BlockID(it.ID) == a.w.cardAt.block
}

// key sends k to the card.
func (p permCtl) key(k tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	p.a.w.card, cmd = p.a.w.card.Update(k)
	return cmd
}
