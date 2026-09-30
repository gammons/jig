package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// relayout recomputes the layout for the current size and prompt height
// and sizes every widget to it. Each transcript pane's list height
// applies at once; a width change is debounced separately per pane
// (resizePane, keyed by that pane's own resizeGen), so main and the
// column's transcript panes each coalesce their own resize bursts
// without interfering with one another (spec §5.1).
func (a *App) relayout() tea.Cmd {
	a.lay = layoutFor(a)
	a.w.status.SetWidth(a.lay.Status.W)
	a.w.search.SetWidth(max(a.lay.Status.W-2, 0))
	a.w.side.SetSize(a.lay.Side.W, a.lay.Side.H)
	a.w.picker.SetSize(a.width, a.height)
	a.w.confirm.SetSize(a.width, a.height)
	var cmds []tea.Cmd
	if columnOpen(a) {
		bw, bh := a.lay.Side.W, max(a.lay.Side.H-columnHeaderRows, 0)
		for _, p := range a.view.col {
			if p.kind == paneDetails {
				p.body.SetSize(bw, bh)
			} else {
				cmds = append(cmds, resizePane(a, p, bw, bh))
			}
		}
	}

	tw, th := a.lay.Transcript.W, a.lay.Transcript.H
	if a.lay.MainCrumb {
		th = max(th-columnHeaderRows, 0)
	}
	cmds = append(cmds, resizePane(a, a.sess.main, tw, th))
	return tea.Batch(cmds...)
}

// resizePane gives p's transcript list height h at once; a width change
// from p's currently applied width is debounced (resizeDebounce), so a
// burst of resizes re-renders every block in p once, not once per step.
// The very first size (p.sz.listW == 0, before anything has been
// applied) and a width matching what's already shown apply at once.
func resizePane(a *App, p *pane, w, h int) tea.Cmd {
	if p.sz.listW == 0 || w == p.sz.listW {
		p.sz.pendingW = w
		p.list.SetSize(w, h)
		p.sz.listW, p.sz.listH = w, h
		return nil
	}
	p.list.SetSize(p.sz.listW, h)
	p.sz.listH = h
	if w == p.sz.pendingW {
		return nil
	}
	p.sz.resizeGen++
	p.sz.pendingW = w
	return a.after(resizeDebounce, resizeMsg{gen: p.sz.resizeGen, pane: p})
}

// applyPaneWidth gives p's transcript list its pending debounced width.
func applyPaneWidth(p *pane, w, h int) {
	p.list.SetSize(w, h)
	p.sz.listW, p.sz.listH = w, h
	p.sz.pendingW = w
}

// applyResizeMsg applies a debounced resizeMsg to its pane, if that pane
// is still main or still in the column and msg.gen still matches its
// current debounce generation; a message for any other pane (popped
// from the column, or unrelated) is dropped (spec §5.1).
func applyResizeMsg(a *App, msg resizeMsg) {
	p := msg.pane
	if p == nil {
		return
	}
	if p == a.sess.main {
		if msg.gen != p.sz.resizeGen {
			return
		}
		th := a.lay.Transcript.H
		if a.lay.MainCrumb {
			th = max(th-columnHeaderRows, 0)
		}
		applyPaneWidth(p, a.lay.Transcript.W, th)
		return
	}
	if msg.gen != p.sz.resizeGen || !slices.Contains(a.view.col, p) {
		return
	}
	applyPaneWidth(p, a.lay.Side.W, max(a.lay.Side.H-columnHeaderRows, 0))
}
