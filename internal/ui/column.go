package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/ui/transcript"
)

// focus is which pane NORMAL's navigation keys, the highlight, and
// enter/q/esc route to.
type focus int

const (
	focusMain focus = iota
	focusColumn
)

// columnHeaderRows is the breadcrumb and rule rows the column (or, when
// narrow with the column open, main) carries above its body.
const columnHeaderRows = 2

// The column (spec §3.3) is a.view.col, a breadcrumbed stack of right-hand
// panes that replaces the details split; a.view.focus says whether NORMAL's
// navigation keys, the highlight, and enter/q/esc route to main or the
// column. These are free functions, not methods, to stay under App's
// per-package method budget (archtest's struct-size check). This task
// pushes only details panes; a subagent's live transcript pane joins the
// column in a later task.

// columnOpen reports whether the column holds any entry.
func columnOpen(a *App) bool { return len(a.view.col) > 0 }

// setFocus makes f the App's focus, applying SetHighlight(mode == NORMAL
// && focused) to main's list.
func setFocus(a *App, f focus) {
	a.view.focus = f
	syncHighlight(a)
}

// syncHighlight re-applies SetHighlight to main's list from the current
// mode and focus; setMode must keep this consistent too.
func syncHighlight(a *App) {
	normal := a.mode == modeNormal
	a.sess.main.list.SetHighlight(normal && a.view.focus == focusMain)
}

// columnTop returns the column's top entry, or nil when it is closed.
func columnTop(a *App) *pane {
	if len(a.view.col) == 0 {
		return nil
	}
	return a.view.col[len(a.view.col)-1]
}

// columnFocused returns the pane NORMAL's navigation keys apply to:
// main, or the column's top entry whenever the column has focus,
// whatever kind of pane it is — navigate/gotoTop are responsible for
// routing a details pane's j/k to its body and swallowing the rest
// (spec §5.2). Other callers that always mean main regardless of focus
// (yank, search, gp, permissions) read a.sess.main directly instead of
// through here.
func columnFocused(a *App) *pane {
	if a.view.focus == focusColumn {
		if p := columnTop(a); p != nil {
			return p
		}
	}
	return a.sess.main
}

// columnBodySize is the column's body size at the App's current
// width/height: the Side rect computed as if the column were open,
// minus its breadcrumb and rule rows.
func columnBodySize(a *App) (int, int) {
	lay := computeLayout(a.width, a.height, a.w.prompt.Height(), a.view.sidebarPref, true, a.view.focus)
	return lay.Side.W, max(lay.Side.H-columnHeaderRows, 0)
}

// columnEntryFor returns the column entry for block id (owned by
// owner): a group header's member list, or a block's own details, built
// at the column's current size, its content from buildDetails/
// groupDetails. (A subagent block's live child pane joins in a later
// task; here every block, subagent or not, gets a details pane.) nil
// when id no longer names a block or group.
func columnEntryFor(a *App, owner *pane, id transcript.BlockID) (*pane, tea.Cmd) {
	w, h := columnBodySize(a)
	p := &pane{
		kind: paneDetails, owner: owner, forBlock: id,
		body: details.New(details.WithoutHeader(), details.WithStyles(a.theme.set.Details)),
	}
	p.body.SetSize(w, h)
	p.buildGen++
	gen := p.buildGen

	if owner == a.sess.main {
		if members, durs, ok := (foldCtl{a}).group(id); ok {
			content := groupDetails(members, durs)
			p.body.SetContent(content)
			p.title = ansi.SanitizeLine(content.Header)
			return p, nil
		}
	}
	b, ok := owner.proj.Block(id)
	if !ok {
		return nil, nil
	}
	content, cmd := buildDetails(a.ctx, b, w, h, a.w.render, a.ports, a.img)
	p.body.SetContent(content)
	p.title = ansi.SanitizeLine(content.Header)
	if cmd == nil {
		return p, nil
	}
	wrapped := func() tea.Msg {
		msg, ok := cmd().(detailsMsg)
		if !ok {
			return nil
		}
		msg.pane, msg.gen = p, gen
		return msg
	}
	return p, wrapped
}

// columnPush appends p to the column.
func columnPush(a *App, p *pane) tea.Cmd {
	a.view.col = append(a.view.col, p)
	return nil
}

// columnPop removes the column's top entry, returning focus to main
// once the column is empty.
func columnPop(a *App) {
	if len(a.view.col) == 0 {
		return
	}
	a.view.col = a.view.col[:len(a.view.col)-1]
	if len(a.view.col) == 0 {
		a.view.focus = focusMain
	}
	syncHighlight(a)
}

// columnClose empties the column and returns focus to main.
func columnClose(a *App) {
	a.view.col = nil
	a.view.focus = focusMain
	syncHighlight(a)
}

// columnReplace empties the column and pushes p as its sole entry.
func columnReplace(a *App, p *pane) tea.Cmd {
	a.view.col = nil
	return columnPush(a, p)
}

// columnDropStale pops the column's top entry while its block no longer
// exists in its owner's projection (e.g. dropUser removed a pending
// send's block), stopping at the first entry whose block still does.
func columnDropStale(a *App) {
	for len(a.view.col) > 0 {
		top := a.view.col[len(a.view.col)-1]
		if top.owner == nil {
			break
		}
		if _, ok := top.owner.proj.Block(top.forBlock); ok {
			break
		}
		columnPop(a)
	}
}

// columnSegments is the breadcrumb path: "main", then each column
// entry's title in order.
func columnSegments(a *App) []string {
	segs := make([]string, 0, len(a.view.col)+1)
	segs = append(segs, "main")
	for _, p := range a.view.col {
		segs = append(segs, p.title)
	}
	return segs
}

// columnView renders the column: the breadcrumb row, the rule row, and
// the top entry's body.
func columnView(a *App) string {
	top := columnTop(a)
	if top == nil {
		return ""
	}
	a.w.crumb.SetSegments(columnSegments(a))
	a.w.crumb.SetFocused(a.view.focus == focusColumn)
	a.w.crumb.SetHint("esc back")
	a.w.crumb.SetWidth(a.lay.Side.W)
	body := top.body.View()
	if top.kind == paneTranscript {
		body = top.list.View()
	}
	return a.w.crumb.View() + "\n" + a.w.crumb.RuleView() + "\n" + body
}

// detailsResult applies an async detailsMsg to its pane, if that pane is
// still in the column and msg.gen still matches its current build.
func detailsResult(a *App, msg detailsMsg) tea.Cmd {
	if msg.Image != nil {
		a.img.store(msg.key, *msg.Image)
	}
	if msg.pane == nil || msg.gen != msg.pane.buildGen || !slices.Contains(a.view.col, msg.pane) {
		return nil
	}
	msg.pane.body.ReplaceContent(msg.Content)
	msg.pane.title = ansi.SanitizeLine(msg.Content.Header)
	a.img.shown = nil
	if msg.Image == nil || columnTop(a) != msg.pane {
		return nil
	}
	return tea.Batch(a.img.show(msg.key), placeSixel(a))
}
