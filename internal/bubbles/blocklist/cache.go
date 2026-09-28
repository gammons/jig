package blocklist

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// slot is one render of an item, memoized under the key (Version, width,
// stylesVersion). lines is dropped on eviction; height and the search memo
// survive it, so offsets and match counts never need a re-render.
type slot struct {
	version, width, sv int
	height             int
	lines              []string // each exactly width cells; nil once evicted

	matchQuery string // the query matched was computed for
	matched    bool
	matchKnown bool
}

// entry is one item's renders, keyed by its ID in cache.entries: the one
// at the current width and the one at the previous width, so toggling
// between two widths (the details split opening and closing) re-renders
// nothing.
type entry struct {
	cur, prev slot
}

// cache holds every entry. Model keeps it behind a pointer so View, a value
// method, can fill and evict it; its contents are only a memo of RenderFunc,
// never state View's output depends on.
type cache struct {
	entries map[string]*entry
	live    map[string]struct{} // IDs whose entry currently holds lines in a slot
	renders int                 // RenderFunc calls, for tests
}

func newCache() *cache {
	return &cache{entries: map[string]*entry{}, live: map[string]struct{}{}}
}

// valid reports whether s is it's render at width and sv. A zero slot
// (width 0) never is: renders always have width >= 1.
func (s *slot) valid(it Item, width, sv int) bool {
	return s.width == width && s.width > 0 && s.version == it.Version && s.sv == sv
}

// find returns e's slot for it at width and sv, or nil.
func (e *entry) find(it Item, width, sv int) *slot {
	switch {
	case e == nil:
		return nil
	case e.cur.valid(it, width, sv):
		return &e.cur
	case e.prev.valid(it, width, sv):
		return &e.prev
	}
	return nil
}

// get returns it's slot with lines, rendering it when it is missing,
// stale, or evicted. An evicted slot re-rendered under the same key keeps
// its recorded height, so offsets computed from it stay correct. A render
// at a new width moves the current slot to prev; one at the current width
// (a new version or styles) replaces it in place.
func (c *cache) get(it Item, width, sv int, st Styles, render RenderFunc) *slot {
	e := c.entries[it.ID]
	s := e.find(it, width, sv)
	if s != nil && s.lines != nil {
		return s
	}
	lines := fit(render(it, width, st), width)
	c.renders++
	c.live[it.ID] = struct{}{}
	if s != nil {
		s.lines = resize(lines, s.height, width)
		return s
	}
	if e == nil {
		e = &entry{}
		c.entries[it.ID] = e
	}
	if e.cur.width != width {
		e.prev = e.cur
	}
	e.cur = slot{version: it.Version, width: width, sv: sv, height: len(lines), lines: lines}
	return &e.cur
}

// height returns it's height at width and sv, rendering only when no valid
// slot exists.
func (c *cache) height(it Item, width, sv int, st Styles, render RenderFunc) int {
	if s := c.entries[it.ID].find(it, width, sv); s != nil {
		return s.height
	}
	return c.get(it, width, sv, st, render).height
}

// matches reports whether it's rendered lines, stripped of escapes,
// contain query case-insensitively. The answer is memoized per slot.
func (c *cache) matches(it Item, width, sv int, st Styles, render RenderFunc, query string) bool {
	if query == "" {
		return false
	}
	if s := c.entries[it.ID].find(it, width, sv); s != nil && s.matchKnown && s.matchQuery == query {
		return s.matched
	}
	s := c.get(it, width, sv, st, render)
	q := strings.ToLower(query)
	s.matched = false
	for _, l := range s.lines {
		if strings.Contains(strings.ToLower(xansi.Strip(l)), q) {
			s.matched = true
			break
		}
	}
	s.matchQuery, s.matchKnown = query, true
	return s.matched
}

// evict drops the lines of every slot of a live entry that keep rejects.
// Heights stay.
func (c *cache) evict(keep func(id string, s *slot) bool) {
	for id := range c.live {
		e := c.entries[id]
		if e == nil {
			delete(c.live, id)
			continue
		}
		if e.cur.lines != nil && !keep(id, &e.cur) {
			e.cur.lines = nil
		}
		if e.prev.lines != nil && !keep(id, &e.prev) {
			e.prev.lines = nil
		}
		if e.cur.lines == nil && e.prev.lines == nil {
			delete(c.live, id)
		}
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
