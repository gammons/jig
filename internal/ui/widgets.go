package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/blocklist"
	"github.com/gammons/jig/internal/bubbles/confirm"
	"github.com/gammons/jig/internal/bubbles/details"
	"github.com/gammons/jig/internal/bubbles/permcard"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/bubbles/prompt"
	"github.com/gammons/jig/internal/bubbles/sidebar"
	"github.com/gammons/jig/internal/bubbles/statusbar"
	"github.com/gammons/jig/internal/ui/theme"
)

// widgets holds every widget the App owns. upserts counts list Upsert
// calls (streaming coalescing is asserted on it).
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
}

// upsert re-renders items in the transcript list; nothing for none.
func (w *widgets) upsert(items []blocklist.Item) {
	if len(items) == 0 {
		return
	}
	w.upserts++
	w.list.Upsert(items...)
}

// newWidgets builds every widget styled from set; the picker loads its
// levels with load.
func newWidgets(set *theme.Set, edit prompt.EditFunc, load picker.LoadFunc) widgets {
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
		card:    permcard.New(func(string, permcard.Reply) tea.Cmd { return nil }, permcard.WithStyles(set.Card)),
		status:  statusbar.New(statusbar.WithStyles(set.Status)),
		side:    sidebar.New(sidebar.WithStyles(set.Sidebar)),
		confirm: confirm.New(confirm.WithStyles(set.Confirm)),
		search:  search,
		render:  r,
	}
}
