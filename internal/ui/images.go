package ui

import (
	"image"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/jig/internal/bubbles/imgrender"
)

// maxCachedImages bounds the rendered-image cache; past it the least
// recently used Result is evicted.
const maxCachedImages = 32

// imgKey identifies one rendered image: its blob ref (content-derived, so
// a key never maps to different bytes) and the cell box it was fit to.
type imgKey struct {
	ref        string
	cols, rows int
}

// imageState is the App's image rendering: the renderer for the detected
// protocol, the rendered Results per key (Render recomputes every call)
// with their use order (least recent first), which size of each kitty
// image the terminal currently holds (sent: ref → the first placeholder
// line of the uploaded Result, which encodes the image id and fitted
// size), and the sixel Result the details pane is showing (shown), to
// re-place after a relayout. The renderer runs in Cmds (renderFor);
// everything else is touched only from Update.
type imageState struct {
	r     *imgrender.Renderer
	cache map[imgKey]imgrender.Result
	order []imgKey
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

// kitty reports whether s renders kitty images (the only protocol with
// terminal-side state).
func (s *imageState) kitty() bool { return s.r.Protocol() == imgrender.Kitty }

// needsUpload reports whether showing res for ref needs an upload res
// lacks: the terminal holds ref at another size (or not at all).
func (s *imageState) needsUpload(ref string, res imgrender.Result) bool {
	return s.kitty() && res.Upload == "" && len(res.Lines) > 0 && s.sent[ref] != res.Lines[0]
}

// cached returns the Result rendered for k, if one is cached and can be
// shown. A kitty Result without the upload the terminal now needs (it
// was rendered while the terminal held that size, which another size
// has since replaced) is dropped instead, so the caller renders afresh.
func (s *imageState) cached(k imgKey) (imgrender.Result, bool) {
	if s == nil {
		return imgrender.Result{}, false
	}
	res, ok := s.cache[k]
	if !ok {
		return imgrender.Result{}, false
	}
	if s.needsUpload(k.ref, res) {
		s.evict(k)
		return imgrender.Result{}, false
	}
	s.touch(k)
	return res, true
}

// renderFor returns the render a details Cmd runs for k, off the Update
// goroutine. It captures what the terminal holds for k's ref now: when
// the renderer returns no upload (it produced one for this size before)
// but the terminal holds another size, the renderer forgets k's upload
// and renders again, so the Result carries the upload it needs.
func (s *imageState) renderFor(k imgKey) func(image.Image) imgrender.Result {
	r, kitty, holds := s.r, s.kitty(), s.sent[k.ref]
	return func(img image.Image) imgrender.Result {
		res := r.Render(k.ref, img, k.cols, k.rows)
		if kitty && res.Upload == "" && len(res.Lines) > 0 && res.Lines[0] != holds {
			r.Forget(k.ref)
			res = r.Render(k.ref, img, k.cols, k.rows)
		}
		return res
	}
}

// store caches res for k as the most recently used, evicting the least
// recently used past maxCachedImages.
func (s *imageState) store(k imgKey, res imgrender.Result) {
	if _, ok := s.cache[k]; !ok {
		for len(s.order) >= maxCachedImages {
			s.evict(s.order[0])
		}
	}
	s.cache[k] = res
	s.touch(k)
}

// touch makes k the most recently used key.
func (s *imageState) touch(k imgKey) {
	s.order = slices.DeleteFunc(s.order, func(o imgKey) bool { return o == k })
	s.order = append(s.order, k)
}

// evict drops k. When no cached Result of k's ref remains, the renderer
// forgets the ref's last upload too, so its next render uploads again
// (the dropped Result may have been the only one carrying those bytes).
func (s *imageState) evict(k imgKey) {
	delete(s.cache, k)
	s.order = slices.DeleteFunc(s.order, func(o imgKey) bool { return o == k })
	if !slices.ContainsFunc(s.order, func(o imgKey) bool { return o.ref == k.ref }) {
		s.r.Forget(k.ref)
	}
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
