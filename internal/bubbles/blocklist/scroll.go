package blocklist

import "sort"

// The list is one flattened line space: item i occupies lines
// [offsets[i], offsets[i]+height(i)), followed by gapAfter(i) blank lines:
// Gap, or none when the next item is Tight. The last item is followed by
// Gap too, so offsets[n] = total + Gap. yOffset is the first visible line.

// gapOf is the effective blank-line count between items.
func gapOf(st Styles) int { return max(0, st.Gap) }

// gapAfter is the blank-line count after item i: none when the item
// below it is Tight.
func gapAfter(m *Model, i int) int {
	if i+1 < len(m.items) && m.items[i+1].Tight {
		return 0
	}
	return gapOf(m.styles)
}

// itemHeight is item i's height in lines, from the offsets.
func itemHeight(m *Model, i int) int {
	return m.offsets[i+1] - gapAfter(m, i) - m.offsets[i]
}

// relayout recomputes every item's height (from the cache; only new,
// changed, or restyled items render) and the offsets, then positions the
// view: bottom-aligned when the last item is selected, otherwise with the
// anchor item id's line within at the top.
func (m *Model) relayout(id string, within int) {
	iw := m.w - 2
	offsets := make([]int, len(m.items)+1)
	line := 0
	for i, it := range m.items {
		offsets[i] = line
		ht := 1
		if iw >= 1 {
			ht = m.c.height(it, iw, m.sv, m.styles, m.render)
		}
		line += ht + gapAfter(m, i)
	}
	offsets[len(m.items)] = line
	m.offsets = offsets
	m.total = 0
	if len(m.items) > 0 {
		m.total = line - gapOf(m.styles)
	}

	if len(m.items) > 0 && m.flags.follow {
		m.yOffset = max(0, m.total-m.h)
		return
	}
	if i, ok := m.index[id]; ok {
		m.yOffset = m.offsets[i] + within
	}
	m.yOffset = clamp(m.yOffset, 0, m.total-m.h)
}

// anchor returns the item at the top of the view and how many of its lines
// are scrolled past, so relayout can keep it in place.
func (m *Model) anchor() (string, int) {
	if len(m.items) == 0 || len(m.offsets) != len(m.items)+1 {
		return "", 0
	}
	i := itemAt(m.offsets, m.yOffset)
	return m.items[i].ID, m.yOffset - m.offsets[i]
}

// ensureVisible scrolls the minimum needed to show the whole selected item,
// or, when it is taller than the view, its first line (top-aligned when
// the first line is off screen).
func (m *Model) ensureVisible() {
	if m.sel < 0 || m.sel >= len(m.items) {
		return
	}
	start := m.offsets[m.sel]
	ht := itemHeight(m, m.sel)
	switch {
	case start < m.yOffset:
		m.yOffset = start
	case ht <= m.h && start+ht > m.yOffset+m.h:
		m.yOffset = start + ht - m.h
	case ht > m.h && start >= m.yOffset+m.h:
		m.yOffset = start
	}
	m.yOffset = clamp(m.yOffset, 0, m.total-m.h)
}

// moveTo selects item i (clamped) and scrolls it into view. It sets follow
// when i lands on the last item, and clears it otherwise.
func (m *Model) moveTo(i int) {
	if len(m.items) == 0 {
		return
	}
	m.sel = clamp(i, 0, len(m.items)-1)
	m.flags.follow = m.sel == len(m.items)-1
	m.ensureVisible()
}

// halfPage scrolls half a view in dir (±1) and moves the selection to the
// item h/2 lines from the selected item's first line — at least one item,
// so a selection on a tall item never gets stuck.
func (m *Model) halfPage(dir int) {
	if len(m.items) == 0 {
		return
	}
	half := max(1, m.h/2)
	target := itemAt(m.offsets, clamp(m.offsets[m.sel]+dir*half, 0, m.total-1))
	if target == m.sel {
		target = clamp(m.sel+dir, 0, len(m.items)-1)
	}
	m.yOffset = clamp(m.yOffset+dir*half, 0, m.total-m.h)
	m.sel = target
	m.flags.follow = m.sel == len(m.items)-1
	m.ensureVisible()
}

// itemAt returns the last item whose first line is <= line (0 when none).
func itemAt(offsets []int, line int) int {
	n := len(offsets) - 1
	i := sort.Search(n, func(i int) bool { return offsets[i] > line }) - 1
	return max(0, i)
}

// clamp limits v to [lo, hi]; lo wins when hi < lo.
func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
