package blocklist

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// entry is one item's render, memoized under the key (ID, Version, width,
// stylesVersion). The ID is the map key in cache.entries; the rest are
// stored here. lines is dropped on eviction; height and the search memo
// survive it, so offsets and match counts never need a re-render.
type entry struct {
	version, width, sv int
	height             int
	lines              []string // each exactly width cells; nil once evicted

	matchQuery string // the query matched was computed for
	matched    bool
	matchKnown bool
}

// cache holds every entry. Model keeps it behind a pointer so View, a value
// method, can fill and evict it; its contents are only a memo of RenderFunc,
// never state View's output depends on.
type cache struct {
	entries map[string]*entry
	live    map[string]struct{} // IDs whose entry currently holds lines
	renders int                 // RenderFunc calls, for tests
}

func newCache() *cache {
	return &cache{entries: map[string]*entry{}, live: map[string]struct{}{}}
}

// valid reports whether e is it's render at width and sv.
func (e *entry) valid(it Item, width, sv int) bool {
	return e != nil && e.version == it.Version && e.width == width && e.sv == sv
}

// get returns it's entry with lines, rendering it when it is missing,
// stale, or evicted. An evicted entry re-rendered under the same key keeps
// its recorded height, so offsets computed from it stay correct.
func (c *cache) get(it Item, width, sv int, st Styles, render RenderFunc) *entry {
	e := c.entries[it.ID]
	if e.valid(it, width, sv) && e.lines != nil {
		return e
	}
	lines := fit(render(it, width, st), width)
	c.renders++
	c.live[it.ID] = struct{}{}
	if e.valid(it, width, sv) {
		e.lines = resize(lines, e.height, width)
		return e
	}
	e = &entry{version: it.Version, width: width, sv: sv, height: len(lines), lines: lines}
	c.entries[it.ID] = e
	return e
}

// height returns it's height at width and sv, rendering only when no valid
// entry exists.
func (c *cache) height(it Item, width, sv int, st Styles, render RenderFunc) int {
	if e := c.entries[it.ID]; e.valid(it, width, sv) {
		return e.height
	}
	return c.get(it, width, sv, st, render).height
}

// matches reports whether it's rendered lines, stripped of escapes,
// contain query case-insensitively. The answer is memoized per entry.
func (c *cache) matches(it Item, width, sv int, st Styles, render RenderFunc, query string) bool {
	if query == "" {
		return false
	}
	if e := c.entries[it.ID]; e.valid(it, width, sv) && e.matchKnown && e.matchQuery == query {
		return e.matched
	}
	e := c.get(it, width, sv, st, render)
	q := strings.ToLower(query)
	e.matched = false
	for _, l := range e.lines {
		if strings.Contains(strings.ToLower(xansi.Strip(l)), q) {
			e.matched = true
			break
		}
	}
	e.matchQuery, e.matchKnown = query, true
	return e.matched
}

// evict drops the lines of every live entry keep rejects. Heights stay.
func (c *cache) evict(keep func(id string) bool) {
	for id := range c.live {
		if keep(id) {
			continue
		}
		if e := c.entries[id]; e != nil {
			e.lines = nil
		}
		delete(c.live, id)
	}
}

// prune forgets every entry whose ID is not in index.
func (c *cache) prune(index map[string]int) {
	for id := range c.entries {
		if _, ok := index[id]; !ok {
			delete(c.entries, id)
			delete(c.live, id)
		}
	}
}

// cachedLines is the number of entries currently holding lines.
func (c *cache) cachedLines() int { return len(c.live) }

// fit normalizes a RenderFunc result to lines of exactly width cells:
// embedded newlines split, tabs expand to 4 spaces, long lines are cut
// (closing any open style), short ones are padded. It never returns fewer
// than one line.
func fit(raw []string, width int) []string {
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		for l := range strings.SplitSeq(r, "\n") {
			out = append(out, fitLine(l, width))
		}
	}
	if len(out) == 0 {
		out = append(out, strings.Repeat(" ", width))
	}
	return out
}

func fitLine(l string, width int) string {
	if strings.Contains(l, "\t") {
		l = strings.ReplaceAll(l, "\t", "    ")
	}
	w := ansi.Width(l)
	if w > width {
		l = ansi.Truncate(l, width, "")
		if strings.Contains(l, "\x1b") {
			l += xansi.ResetStyle
		}
		w = ansi.Width(l)
	}
	if w < width {
		l += strings.Repeat(" ", width-w)
	}
	return l
}

// resize pads (with blank lines) or cuts lines to exactly n.
func resize(lines []string, n, width int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines
}
