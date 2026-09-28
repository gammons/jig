package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/imgrender"
)

// maxCachedImages bounds the rendered-image cache; past it the cache
// starts over (a kitty upload still live in the terminal is re-sent from
// a fresh render when needed).
const maxCachedImages = 32

// imgKey identifies one rendered image: its blob ref (content-derived, so
// a key never maps to different bytes) and the cell box it was fit to.
type imgKey struct {
	ref        string
	cols, rows int
}

// imageState is the App's image rendering: the renderer for the detected
// protocol, the rendered Results per key (Render recomputes every call),
// which size of each kitty image the terminal currently holds (sent: ref
// → the first placeholder line of the uploaded Result, which encodes the
// image id and fitted size), and the sixel Result the details pane is
// showing (shown), to re-place after a relayout. The renderer is only
// called from Cmds; everything else is touched only from Update.
type imageState struct {
	r     *imgrender.Renderer
	cache map[imgKey]imgrender.Result
	sent  map[string]string
	shown *imgrender.Result
}

// newImageState starts an empty state rendering with protocol p.
func newImageState(p imgrender.Protocol, tmux bool) *imageState {
	return &imageState{
		r:     imgrender.New(p, imgrender.WithTmux(tmux)),
		cache: map[imgKey]imgrender.Result{},
		sent:  map[string]string{},
	}
}

// cached returns the Result rendered for k, if any.
func (s *imageState) cached(k imgKey) (imgrender.Result, bool) {
	if s == nil {
		return imgrender.Result{}, false
	}
	res, ok := s.cache[k]
	return res, ok
}

// store caches res for k. A kitty Result the renderer returned without an
// upload (it had produced one for this ref and fitted size before) takes
// the upload of the cached Result with the same placeholder cells, so a
// cached Result can always be re-sent after the terminal's image was
// replaced by another size.
func (s *imageState) store(k imgKey, res imgrender.Result) {
	if res.Upload == "" && len(res.Lines) > 0 {
		for ck, c := range s.cache {
			if ck.ref == k.ref && c.Upload != "" && len(c.Lines) > 0 && c.Lines[0] == res.Lines[0] {
				res.Upload = c.Upload
				break
			}
		}
	}
	if len(s.cache) >= maxCachedImages {
		s.cache = map[imgKey]imgrender.Result{}
	}
	s.cache[k] = res
}

// upload returns the tea.Raw Cmd sending k's kitty upload, unless the
// terminal already holds this ref at this size; nil otherwise.
func (s *imageState) upload(k imgKey) tea.Cmd {
	res, ok := s.cache[k]
	if !ok || res.Upload == "" || len(res.Lines) == 0 || s.sent[k.ref] == res.Lines[0] {
		return nil
	}
	s.sent[k.ref] = res.Lines[0]
	return tea.Raw(res.Upload)
}

// show makes k's Result the one on screen: it sends a kitty upload if
// needed and remembers a sixel Result for placement.
func (s *imageState) show(k imgKey) tea.Cmd {
	s.shown = nil
	res, ok := s.cache[k]
	if !ok {
		return nil
	}
	if res.Sixel != "" {
		s.shown = &res
	}
	return s.upload(k)
}

// placeSixel returns the tea.Raw Cmd drawing the shown sixel over the
// details body, whose first cell is at the details rect origin plus the
// pane's BodyOrigin; nil when no sixel is shown, the split is closed, or
// the pane has no body row.
func placeSixel(a *App) tea.Cmd {
	if a.img.shown == nil || !a.view.detailsOpen || !a.lay.DetailsOpen {
		return nil
	}
	bx, by, ok := a.w.details.BodyOrigin()
	if !ok {
		return nil
	}
	return tea.Raw(imgrender.Place(*a.img.shown, a.lay.Side.X+bx, a.lay.Side.Y+by))
}
