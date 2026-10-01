// Package theme holds jig's theme palettes: the built-in set ported from
// slk (internal/ui/styles/themes.go and tint.go, MIT, same author) plus
// the rules for completing a partial palette (a custom theme, or a
// built-in missing its derived fields) into one with every field set.
//
// This package does no I/O and imports no third-party package: it is pure
// data and pure functions. Reading theme files from disk lives in
// internal/data/themefs; converting a Palette into lipgloss styles lives
// in the per-widget mapping functions that use charm.land/lipgloss/v2.
package theme

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// BaseColors holds a theme's ten core colors, as hex ("#RRGGBB") or
// ANSI-16 index strings ("0"-"15").
type BaseColors struct {
	Primary, Accent, Warning, Error  string
	Background, Surface, SurfaceDark string
	Text, TextMuted, Border          string
}

// SidebarColors holds a theme's sidebar colors. Each falls back to its
// message-pane equivalent (Background/Text/TextMuted) in Complete when
// left empty.
type SidebarColors struct {
	SidebarBackground, SidebarText, SidebarTextMuted string
}

// SelectionColors holds the selected-text and search-match highlight
// colors. Each falls back to a derived default (Primary/Background,
// Warning/Background) in Complete when left empty.
type SelectionColors struct {
	SelectionBackground, SelectionForeground string
	SearchHighlightBg, SearchHighlightFg     string
}

// TintColors holds colors derived by mixing (see mixColors) when a theme
// leaves them empty: the compose box's insert-mode background and the
// selected-row tint used when its panel is/isn't focused.
type TintColors struct {
	ComposeInsertBG, SelectionBgFocused, SelectionBgUnfocused string
}

// Palette holds one theme's semantic colors. It mirrors slk's
// ThemeColors, minus RailBackground (jig has no workspace rail), grouped
// into embedded structs so field promotion keeps p.Primary-style access
// while keeping each struct's field count under this repo's cap.
type Palette struct {
	Name string
	BaseColors
	SidebarColors
	SelectionColors
	TintColors
}

// Builtin returns every built-in palette, sorted alphabetically by Name.
// The data is ported from slk's builtinThemes map, split across
// builtin_a.go/builtin_b.go/builtin_c.go to keep each file under the
// repo's 500-line size limit.
func Builtin() []Palette {
	all := make([]Palette, 0, 59)
	all = append(all, builtinA()...)
	all = append(all, builtinB()...)
	all = append(all, builtinC()...)
	return all
}

// Default returns the completed "dark" built-in palette, jig's fallback
// theme.
func Default() Palette {
	return Complete(darkRaw())
}

// darkRaw returns the "Dark" built-in palette exactly as ported from slk
// (before Complete fills its derived fields). Complete uses it as the
// fallback source for a palette's empty base-color fields, mirroring
// slk's LoadCustomThemes / lookupTheme fallback-to-dark rule.
func darkRaw() Palette {
	for _, p := range Builtin() {
		if p.Name == "Dark" {
			return p
		}
	}
	// Unreachable: builtin_a.go always has a "Dark" entry.
	return Palette{}
}

// Lookup finds a palette by name, case-insensitive. custom is searched
// first, so a custom theme can override a built-in of the same name.
func Lookup(name string, custom []Palette) (Palette, bool) {
	lower := strings.ToLower(name)
	for _, p := range custom {
		if strings.ToLower(p.Name) == lower {
			return p, true
		}
	}
	for _, p := range Builtin() {
		if strings.ToLower(p.Name) == lower {
			return p, true
		}
	}
	return Palette{}, false
}

// defaultTintAlpha is the share of the foreground color mixColors uses
// when deriving ComposeInsertBG and the selection tints, ported from
// slk's tint.go.
const defaultTintAlpha = 0.15

// Complete fills p's empty fields: the ten base color fields fall back to
// Default's ("dark"'s) value; then the sidebar/selection/search fields
// fall back to their message-pane equivalent; then ComposeInsertBG and
// the selection tints are derived by mixing colors, unless the palette
// sets them explicitly. This mirrors slk's styles.Apply derivation order.
func Complete(p Palette) Palette {
	fillBase(&p.BaseColors, darkRaw().BaseColors)

	if p.SidebarBackground == "" {
		p.SidebarBackground = p.Background
	}
	if p.SidebarText == "" {
		p.SidebarText = p.Text
	}
	if p.SidebarTextMuted == "" {
		p.SidebarTextMuted = p.TextMuted
	}

	if p.SelectionBackground == "" {
		p.SelectionBackground = p.Primary
	}
	if p.SelectionForeground == "" {
		p.SelectionForeground = p.Background
	}

	if p.SearchHighlightBg == "" {
		p.SearchHighlightBg = p.Warning
	}
	if p.SearchHighlightFg == "" {
		p.SearchHighlightFg = p.Background
	}

	if p.ComposeInsertBG == "" {
		p.ComposeInsertBG = mixColors(p.Accent, p.Background, defaultTintAlpha)
	}
	if p.SelectionBgFocused == "" {
		p.SelectionBgFocused = mixColors(p.Accent, p.Background, defaultTintAlpha)
	}
	if p.SelectionBgUnfocused == "" {
		p.SelectionBgUnfocused = mixColors(p.TextMuted, p.Background, defaultTintAlpha)
	}

	return p
}

// fillBase fills b's ten fields from d wherever b leaves them empty.
func fillBase(b *BaseColors, d BaseColors) {
	if b.Primary == "" {
		b.Primary = d.Primary
	}
	if b.Accent == "" {
		b.Accent = d.Accent
	}
	if b.Warning == "" {
		b.Warning = d.Warning
	}
	if b.Error == "" {
		b.Error = d.Error
	}
	if b.Background == "" {
		b.Background = d.Background
	}
	if b.Surface == "" {
		b.Surface = d.Surface
	}
	if b.SurfaceDark == "" {
		b.SurfaceDark = d.SurfaceDark
	}
	if b.Text == "" {
		b.Text = d.Text
	}
	if b.TextMuted == "" {
		b.TextMuted = d.TextMuted
	}
	if b.Border == "" {
		b.Border = d.Border
	}
}

// mixColors returns a straight-line RGB interpolation between fg and bg,
// ported from slk's tint.go. alpha is the share of fg: 0 returns bg, 1
// returns fg. Either color may be a bare ANSI-16 index string ("0"-"15")
// rather than hex; since there's no way to know a terminal's actual
// palette RGB values for an index, mixing degrades to returning bg
// unchanged in that case (matching the "bypass mixColors" pattern slk's
// own ansi-dark/ansi-light themes use for the fields this can't handle).
func mixColors(fg, bg string, alpha float64) string {
	if alpha <= 0 {
		return bg
	}
	if alpha >= 1 {
		return fg
	}
	fr, fgc, fb, ok1 := parseHex(fg)
	br, bgc, bb, ok2 := parseHex(bg)
	if !ok1 || !ok2 {
		return bg
	}
	r := uint8(math.Round(float64(fr)*alpha + float64(br)*(1-alpha)))
	g := uint8(math.Round(float64(fgc)*alpha + float64(bgc)*(1-alpha)))
	b := uint8(math.Round(float64(fb)*alpha + float64(bb)*(1-alpha)))
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// tint is mixColors, except that when either color is an ANSI-16 index
// (so no mix is possible) it returns fg rather than bg: the caller wants
// a softened fg, and plain fg beats losing the color altogether.
func tint(fg, bg string, alpha float64) string {
	_, _, _, ok1 := parseHex(fg)
	_, _, _, ok2 := parseHex(bg)
	if !ok1 || !ok2 {
		return fg
	}
	return mixColors(fg, bg, alpha)
}

// successColor is the palette's "done/added" foreground: Accent blended
// halfway toward TextMuted, so finished todos and added files read as
// green without being the theme's loudest color.
func successColor(p Palette) string {
	return tint(p.Accent, p.TextMuted, successAlpha)
}

// successAlpha is Accent's share of successColor.
const successAlpha = 0.55

// removedLineBg is a removed diff line's background: a light wash of
// Error over Background, the same idea as the added line's
// SelectionBgFocused tint, so a deletion is marked without drowning the
// code in solid red.
func removedLineBg(p Palette) string {
	return tint(p.Error, p.Background, removedTintAlpha)
}

// removedTintAlpha is Error's share of removedLineBg.
const removedTintAlpha = 0.2

// gaugeTrackColor is the unfilled part of the sidebar's context gauge:
// TextMuted halfway toward Background (the sidebar is drawn over the
// screen's Background), unless that sinks too close to Background to see
// (or can't be mixed), then TextMuted itself. Border, used before, sits
// at a WCAG contrast of ~1.1-1.3 on many themes: practically invisible.
func gaugeTrackColor(p Palette) string {
	c := mixColors(p.TextMuted, p.Background, 0.5)
	if r, ok := Contrast(c, p.Background); ok && r >= minTrackContrast {
		return c
	}
	return p.TextMuted
}

// minTrackContrast is the lowest WCAG contrast gaugeTrackColor accepts
// against the sidebar's background.
const minTrackContrast = 1.6

// parseHex parses s as "#RRGGBB". ok is false for any other form,
// including a bare ANSI-16 index string.
func parseHex(s string) (r, g, b uint8, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	rv, err := strconv.ParseUint(s[1:3], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	gv, err := strconv.ParseUint(s[3:5], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	bv, err := strconv.ParseUint(s[5:7], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(rv), uint8(gv), uint8(bv), true
}

// colorKeySetters maps a custom theme's snake_case TOML/map keys
// (matching slk's ThemeColors toml tags, minus rail_background) to the
// Palette field they set. A function, not a package var, per this repo's
// no-package-mutable-vars rule.
func colorKeySetters() map[string]func(p *Palette, v string) {
	return map[string]func(p *Palette, v string){
		"primary":                func(p *Palette, v string) { p.Primary = v },
		"accent":                 func(p *Palette, v string) { p.Accent = v },
		"warning":                func(p *Palette, v string) { p.Warning = v },
		"error":                  func(p *Palette, v string) { p.Error = v },
		"background":             func(p *Palette, v string) { p.Background = v },
		"surface":                func(p *Palette, v string) { p.Surface = v },
		"surface_dark":           func(p *Palette, v string) { p.SurfaceDark = v },
		"text":                   func(p *Palette, v string) { p.Text = v },
		"text_muted":             func(p *Palette, v string) { p.TextMuted = v },
		"border":                 func(p *Palette, v string) { p.Border = v },
		"sidebar_background":     func(p *Palette, v string) { p.SidebarBackground = v },
		"sidebar_text":           func(p *Palette, v string) { p.SidebarText = v },
		"sidebar_text_muted":     func(p *Palette, v string) { p.SidebarTextMuted = v },
		"selection_background":   func(p *Palette, v string) { p.SelectionBackground = v },
		"selection_foreground":   func(p *Palette, v string) { p.SelectionForeground = v },
		"search_highlight_bg":    func(p *Palette, v string) { p.SearchHighlightBg = v },
		"search_highlight_fg":    func(p *Palette, v string) { p.SearchHighlightFg = v },
		"compose_insert_bg":      func(p *Palette, v string) { p.ComposeInsertBG = v },
		"selection_bg_focused":   func(p *Palette, v string) { p.SelectionBgFocused = v },
		"selection_bg_unfocused": func(p *Palette, v string) { p.SelectionBgUnfocused = v },
	}
}

// Custom builds a Palette named name from a custom theme's colors map
// (snake_case keys, as loaded by themefs.Load's [colors] table). Unknown
// keys produce a warning each (sorted by key) and are otherwise ignored;
// the returned Palette is not completed, callers do that with Complete.
func Custom(name string, colors map[string]string) (Palette, []string) {
	p := Palette{Name: name}

	keys := make([]string, 0, len(colors))
	for k := range colors {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	setters := colorKeySetters()
	var warnings []string
	for _, k := range keys {
		set, ok := setters[k]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("theme %q: unknown color key %q", name, k))
			continue
		}
		set(&p, colors[k])
	}
	return p, warnings
}
