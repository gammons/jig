package ui

import (
	"charm.land/bubbles/v2/textinput"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/confirm"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/permcard"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/bubbles/statusbar"
	"github.com/gammons/jig/internal/ui/theme"
	"github.com/gammons/jig/internal/ui/transcript"
)

// widgets holds every widget the App owns. upserts counts list Upsert
// calls (streaming coalescing is asserted on it). cardAt is the block the
// permission card renders under and the card version and width its item
// was last built with; arm is the card's arming state (permCtl.guard).
type widgets struct {
	list    blocklist.Model
	prompt  prompt.Model
	picker  picker.Model
	details details.Model
	card    permcard.Model
	status  statusbar.Model
	side    sidebar.Model
	confirm confirm.Model
	search  textinput.Model
	render  *renderer
	upserts int
	cardAt  cardKey
	arm     cardArm
	// gen counts list upserts and resets: every projection change that
	// reaches the screen goes through one, so sideProj keys on it.
	gen      int
	sideProj sideCache
}

// cardKey records where the permission card is rendered: the block that
// carries it ("" for none), and the card version and width it was built
// at, so a change to either re-renders that block.
type cardKey struct {
	block transcript.BlockID
	ver   int
	width int
}

// upsert re-renders items in the transcript list; nothing for none.
func (w *widgets) upsert(items []blocklist.Item) {
	if len(items) == 0 {
		return
	}
	w.upserts++
	w.gen++
	w.list.Upsert(w.withCard(items)...)
}

// setItems replaces every item in the transcript list.
func (w *widgets) setItems(items []blocklist.Item) {
	w.gen++
	w.list.SetItems(w.withCard(items))
}

// withCard puts the card's view on the item of the block that carries it.
func (w *widgets) withCard(items []blocklist.Item) []blocklist.Item {
	if w.cardAt.block == "" {
		return items
	}
	for i, it := range items {
		if it.ID != string(w.cardAt.block) {
			continue
		}
		if d, ok := it.Data.(blockData); ok {
			d.Card = w.card.View()
			items[i].Data = d
		}
	}
	return items
}

// newWidgets builds every widget styled from set; the picker loads its
// levels with load, and the permission card replies through reply.
func newWidgets(set *theme.Set, edit prompt.EditFunc, load picker.LoadFunc, reply permcard.ReplyFunc) widgets {
	r := newRenderer(set)
	search := textinput.New()
	search.Prompt = "/"
	// A blinking cursor ticks on its own real-time timer (not the App's
	// injected clock); a static one keeps the search input's Focus Cmd
	// synchronous, like prompt's textarea (prompt.go's taStyles).
	sst := search.Styles()
	sst.Cursor.Blink = false
	search.SetStyles(sst)
	return widgets{
		list:    blocklist.New(r.render, blocklist.WithStyles(set.Blocklist)),
		prompt:  prompt.New(edit, prompt.WithStyles(set.Prompt)),
		picker:  picker.New(load, picker.WithStyles(set.Picker), picker.WithPreview(previewTheme)),
		details: details.New(details.WithStyles(set.Details)),
		card:    permcard.New(reply, permcard.WithStyles(set.Card)),
		status:  statusbar.New(statusbar.WithStyles(set.Status)),
		side:    sidebar.New(sidebar.WithStyles(set.Sidebar)),
		confirm: confirm.New(confirm.WithStyles(set.Confirm)),
		search:  search,
		render:  r,
	}
}
