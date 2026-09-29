package blocklist

import (
	"cmp"
	"slices"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/jig/internal/bubbles/ansi"
)

// slot is one render of an item, memoized under the key (Version, width,
// stylesVersion). lines is dropped on eviction; height and the search memo
// survive it, so offsets and match counts never need a re-render. raw is
// the RenderFunc's output split at newlines, which lines was fit from line
// by line, so the next version's render at the same width re-fits only
// the lines that changed; it is dropped with lines.
type slot struct {
	version, width, sv int
	height             int
	lines              []string // each exactly width cells; nil once evicted
	raw                []string

	matchQuery string // the query matched was computed for
	matched    bool
	matchKnown bool
}

// entry is one item's renders, keyed by its ID in cache.entries: the one
// at the current width and the one at the previous width, so toggling
// between two widths (the details split opening and closing) re-renders
// nothing. bytes is what its slots' lines and raw hold; used is the View
// (cache.clock) that last showed or rendered it, the LRU order.
type entry struct {
	cur, prev slot
	bytes     int
	used      int
}

// DefaultCacheBudget is the bytes of rendered lines the list keeps before
// evicting the least recently shown items' lines. A 600-block session
// renders to ~3 MB in all, so it scrolls without re-rendering anything;
// only far larger ones re-render at their far ends.
const DefaultCacheBudget = 8 << 20

// cache holds every entry. Model keeps it behind a pointer so View, a value
// method, can fill and evict it; its contents are only a memo of RenderFunc,
// never state View's output depends on. live is the IDs whose entry holds
// lines; bytes is their total, kept at most budget by evicting the least
// recently used (except what the current View shows).
type cache struct {
	entries map[string]*entry
	live    map[string]struct{}
	bytes   int
	budget  int
	clock   int
	renders int // RenderFunc calls, for tests
	fits    int // lines fit rather than reused, for tests
}

func newCache() *cache {
	return &cache{entries: map[string]*entry{}, live: map[string]struct{}{}, budget: DefaultCacheBudget}
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
// (a new version or styles) replaces it in place, re-fitting only the
// lines that differ from the render it replaces.
func (c *cache) get(it Item, width, sv int, st Styles, render RenderFunc) *slot {
	e := c.entries[it.ID]
	s := e.find(it, width, sv)
	if s != nil && s.lines != nil {
		e.used = c.clock
		return s
	}
	var old *slot
	if e != nil && e.cur.width == width && e.cur.lines != nil {
		old = &e.cur
	}
	raw := splitRaw(render(it, width, st))
	lines := c.fitFrom(raw, width, old)
	c.renders++
	if s != nil {
		s.lines, s.raw = resize(lines, s.height, width), raw
	} else {
		if e == nil {
			e = &entry{}
			c.entries[it.ID] = e
		}
		if e.cur.width != width {
			e.prev = e.cur
		}
		e.cur = slot{version: it.Version, width: width, sv: sv, height: len(lines), lines: lines, raw: raw}
		s = &e.cur
	}
	e.used = c.clock
	c.account(it.ID, e)
	return s
}

// account re-counts e's bytes into the total and records whether it holds
// lines.
func (c *cache) account(id string, e *entry) {
	c.bytes -= e.bytes
	e.bytes = slotBytes(&e.cur) + slotBytes(&e.prev)
	c.bytes += e.bytes
	if e.cur.lines != nil || e.prev.lines != nil {
		c.live[id] = struct{}{}
	} else {
		delete(c.live, id)
	}
}

// slotBytes is what s's lines and raw hold.
func slotBytes(s *slot) int {
	n := 0
	for _, l := range s.lines {
		n += len(l)
	}
	for _, l := range s.raw {
		n += len(l)
	}
	return n
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

// evict drops lines the cache can no longer use or keep: every slot keep
// rejects (a removed item or a stale render), then, while the total is
// over budget, both slots of the least recently used entries, never one
// used at or after since (the current View's). Heights stay.
func (c *cache) evict(keep func(id string, s *slot) bool, since int) {
	for id := range c.live {
		e := c.entries[id]
		if e == nil {
			delete(c.live, id)
			continue
		}
		if e.cur.lines != nil && !keep(id, &e.cur) {
			e.cur.lines, e.cur.raw = nil, nil
		}
		if e.prev.lines != nil && !keep(id, &e.prev) {
			e.prev.lines, e.prev.raw = nil, nil
		}
		c.account(id, e)
	}
	if c.bytes <= c.budget {
		return
	}
	type aged struct {
		id   string
		used int
	}
	var old []aged
	for id := range c.live {
		if e := c.entries[id]; e.used < since {
			old = append(old, aged{id, e.used})
		}
	}
	slices.SortFunc(old, func(a, b aged) int { return cmp.Compare(a.used, b.used) })
	for _, o := range old {
		if c.bytes <= c.budget {
			break
		}
		e := c.entries[o.id]
		e.cur.lines, e.cur.raw = nil, nil
		e.prev.lines, e.prev.raw = nil, nil
		c.account(o.id, e)
	}
}

// prune forgets every entry whose ID is not in index.
func (c *cache) prune(index map[string]int) {
	for id, e := range c.entries {
		if _, ok := index[id]; !ok {
			c.bytes -= e.bytes
			delete(c.entries, id)
			delete(c.live, id)
		}
	}
}

// fit normalizes a RenderFunc result to lines of exactly width cells:
// embedded newlines split, tabs expand to 4 spaces, long lines are cut
// (closing any open style), short ones are padded. It never returns fewer
// than one line.
func fit(raw []string, width int) []string {
	var c cache
	return c.fitFrom(splitRaw(raw), width, nil)
}

// splitRaw splits a RenderFunc result at embedded newlines, one entry per
// line.
func splitRaw(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if !strings.Contains(r, "\n") {
			out = append(out, r)
			continue
		}
		out = append(out, strings.Split(r, "\n")...)
	}
	return out
}

// fitFrom fits split lines to width (see fit), reusing old's fitted line
// wherever the line equals the one old's was fit from: fitting is a pure
// function of the line and the width, and old is at width.
func (c *cache) fitFrom(split []string, width int, old *slot) []string {
	out := make([]string, len(split))
	for i, l := range split {
		if old != nil && i < len(old.raw) && i < len(old.lines) && old.raw[i] == l {
			out[i] = old.lines[i]
			continue
		}
		out[i] = fitLine(l, width)
		c.fits++
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
