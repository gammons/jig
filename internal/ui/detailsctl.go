package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/ui/transcript"
)

// detailsCtl reconciles asynchronous details content into the open split:
// a build's result (with its image, if any), and keeping a running
// subagent's details current.
type detailsCtl struct{ a *App }

// result applies a detailsMsg. Its image Result is cached whatever the
// selection (the renderer already counted a kitty upload for it); the
// content applies only to the block the split shows. A shown image sends
// its kitty upload (once per size the terminal holds) or is placed as a
// sixel over the details body.
func (d detailsCtl) result(msg detailsMsg) tea.Cmd {
	a := d.a
	if msg.Image != nil {
		a.img.store(msg.key, *msg.Image)
	}
	// A build for a block the selection has since left is stale.
	if msg.Block != a.view.detailsFor {
		return nil
	}
	// The split was opened (SetContent, scroll at the top) before this
	// content arrived; a later build for the same block (edit context,
	// the image, a live subagent refresh) keeps the user's scroll.
	a.w.details.ReplaceContent(msg.Content)
	a.img.shown = nil
	if msg.Image == nil || !a.view.detailsOpen {
		return nil
	}
	return tea.Batch(a.img.show(msg.key), placeSixel(a))
}

// shownSubagent returns the block the open split shows if it is a
// running Subagent block.
func (d detailsCtl) shownSubagent() (transcript.Block, bool) {
	a := d.a
	if !a.view.detailsOpen {
		return transcript.Block{}, false
	}
	b, ok := a.sess.proj.Block(a.view.detailsFor)
	if !ok || b.Kind != transcript.KindSubagent || b.Sub == nil || b.Sub.Child == "" {
		return transcript.Block{}, false
	}
	return b, true
}

// childEvent notes a descendant event: while the split shows a subagent,
// its details are re-read on the next streamTick (at most once per tick).
func (d detailsCtl) childEvent() {
	if b, ok := d.shownSubagent(); ok && isLive(b) {
		d.a.view.stream.subStale = true
	}
}

// refresh re-reads the shown subagent's child messages if a child event
// arrived since the last tick.
func (d detailsCtl) refresh() tea.Cmd {
	a := d.a
	if !a.view.stream.subStale {
		return nil
	}
	a.view.stream.subStale = false
	b, ok := d.shownSubagent()
	if !ok {
		return nil
	}
	_, cmd := buildSubagentDetails(a.ctx, b, a.ports)
	return cmd
}
