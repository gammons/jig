package ui

import tea "charm.land/bubbletea/v2"

// relayout recomputes the layout for the current size and prompt height
// and sizes every widget to it. The transcript list's height applies at
// once; a width change is debounced (it re-renders every block).
func (a *App) relayout() tea.Cmd {
	a.lay = layoutFor(a)
	a.w.status.SetWidth(a.lay.Status.W)
	a.w.search.SetWidth(max(a.lay.Status.W-2, 0))
	a.w.side.SetSize(a.lay.Side.W, a.lay.Side.H)
	a.w.picker.SetSize(a.width, a.height)
	a.w.confirm.SetSize(a.width, a.height)
	if columnOpen(a) {
		bw, bh := a.lay.Side.W, max(a.lay.Side.H-columnHeaderRows, 0)
		for _, p := range a.view.col {
			if p.kind == paneDetails {
				p.body.SetSize(bw, bh)
			}
		}
	}

	tw, th := a.lay.Transcript.W, a.lay.Transcript.H
	if a.lay.MainCrumb {
		th = max(th-columnHeaderRows, 0)
	}
	if a.sess.main.sz.listW == 0 || tw == a.sess.main.sz.listW {
		a.sess.main.sz.pendingW = tw
		a.sess.main.list.SetSize(tw, th)
		a.sess.main.sz.listW, a.sess.main.sz.listH = tw, th
		return nil
	}
	a.sess.main.list.SetSize(a.sess.main.sz.listW, th)
	a.sess.main.sz.listH = th
	if tw == a.sess.main.sz.pendingW {
		return nil
	}
	a.sess.main.sz.resizeGen++
	a.sess.main.sz.pendingW = tw
	return a.after(resizeDebounce, resizeMsg{gen: a.sess.main.sz.resizeGen})
}

// applyListWidth gives the transcript list the layout's current size.
func (a *App) applyListWidth() {
	th := a.lay.Transcript.H
	if a.lay.MainCrumb {
		th = max(th-columnHeaderRows, 0)
	}
	a.sess.main.list.SetSize(a.lay.Transcript.W, th)
	a.sess.main.sz.listW, a.sess.main.sz.listH = a.lay.Transcript.W, th
	a.sess.main.sz.pendingW = a.sess.main.sz.listW
}
