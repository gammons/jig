package ui

import (
	"slices"

	"github.com/gammons/jig/internal/ui/theme"
)

// themeState owns the App's palette: the custom palettes it may choose
// from, the current one, and the Set built from it. version is the
// styles version pushed to widgets that cache renders; it only grows.
type themeState struct {
	custom  []theme.Palette
	current theme.Palette
	version int
	set     theme.Set
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
