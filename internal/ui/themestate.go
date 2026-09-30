package ui

import (
	"slices"

	"github.com/gammons/jig/internal/ui/theme"
)

// themeState owns the App's palette: the custom palettes it may choose
// from, the current one, and the Set built from it. version is the
// styles version pushed to widgets that cache renders; it only grows.
// orig is the palette to restore while the theme picker previews
// another (nil when no preview is in progress). gen keys the debounced
// preview (themeApplyMsg): only the latest highlight change applies.
type themeState struct {
	custom  []theme.Palette
	current theme.Palette
	version int
	set     theme.Set
	orig    *theme.Palette
	gen     int
}

// newThemeState picks name among custom and the built-ins, falling back to
// the default ("dark") palette for an unknown or empty name.
func newThemeState(name string, custom []theme.Palette) *themeState {
	p := theme.Default()
	if found, ok := theme.Lookup(name, custom); ok {
		p = theme.Complete(found)
	}
	ts := &themeState{custom: slices.Clone(custom), current: p, version: 1}
	ts.set = theme.Build(p, ts.version)
	return ts
}

// apply makes p current at the next styles version.
func (ts *themeState) apply(p theme.Palette) {
	ts.version++
	ts.current = p
	ts.set = theme.Build(p, ts.version)
}

// pushTheme pushes the current theme's Set to every widget: the
// transcript's Markdown renderer (its block renderer reads the Set
// through a pointer), the prompt, picker, details, permission card,
// status bar, sidebar, and confirm dialog, and last the transcript list
// at the new styles version. Every item's version is bumped too, so no
// block keeps a render from the old palette; the list's own cache key
// includes the styles version, so SetStyles re-renders each block once.
// The card's new version re-renders its block on the next sync.
func pushTheme(a *App) {
	set := a.theme.set
	a.w.render.md.SetStyles(set.Markdown)
	a.w.prompt.SetStyles(set.Prompt)
	a.w.picker.SetStyles(set.Picker)
	a.w.details.SetStyles(set.Details)
	a.w.card.SetStyles(set.Card)
	a.w.status.SetStyles(set.Status)
	a.w.side.SetStyles(set.Sidebar)
	a.w.confirm.SetStyles(set.Confirm)
	for id := range a.sess.track.versions {
		a.sess.track.versions[id]++
	}
	a.w.list.SetStyles(set.Blocklist, a.theme.version)
}

// preview applies the palette named name, remembering the palette to
// restore on the first preview. It reports whether anything changed.
func (ts *themeState) preview(name string) bool {
	found, ok := theme.Lookup(name, ts.custom)
	if !ok {
		return false
	}
	if ts.orig == nil {
		o := ts.current
		ts.orig = &o
	}
	p := theme.Complete(found)
	if p.Name == ts.current.Name {
		return false
	}
	ts.apply(p)
	return true
}

// restore ends a preview, re-applying the original palette. It reports
// whether a preview was in progress.
func (ts *themeState) restore() bool {
	if ts.orig == nil {
		return false
	}
	o := *ts.orig
	ts.orig = nil
	ts.apply(o)
	return true
}

// keep ends a preview on the palette named name, applying it if it is
// not current, and reports whether it exists.
func (ts *themeState) keep(name string) bool {
	found, ok := theme.Lookup(name, ts.custom)
	ts.orig = nil
	if !ok {
		return false
	}
	if p := theme.Complete(found); p.Name != ts.current.Name {
		ts.apply(p)
	}
	return true
}

// shown is the name of the palette the user has (the original while a
// preview is in progress).
func (ts *themeState) shown() string {
	if ts.orig != nil {
		return ts.orig.Name
	}
	return ts.current.Name
}
