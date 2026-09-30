package blocklist

// hitter maps a view cell to an item line, walking offsets the way
// visibleRows does, and returns an item's rendered lines at the current
// width from the cache.
type hitter struct {
	m Model
}

// HitTest maps the view cell (x, y) to an item line: id is the item's ID,
// line is the line within its rendered lines, and col is x-1 (x=0 is the
// selection prefix, x=w-1 is the scrollbar column). ok is false for the
// prefix column, the scrollbar column, and cells outside [0,w)×[0,h). A
// gap row returns the nearest item line: the end of the item above it, or
// the start of the item below it for a leading gap (a gap row that is the
// first visible row of the view, with no item content above it on screen).
func HitTest(m Model, x, y int) (id string, line, col int, ok bool) {
	h := hitter{m}
	return h.test(x, y)
}

// Lines returns id's rendered lines at the current width, from the cache.
func Lines(m Model, id string) []string {
	i, ok := m.index[id]
	if !ok {
		return nil
	}
	return m.c.get(m.items[i], m.w-2, m.sv, m.styles, m.render).lines
}

func (h hitter) test(x, y int) (id string, line, col int, ok bool) {
	m := h.m
	if x <= 0 || x >= m.w-1 || y < 0 || y >= m.h || len(m.items) == 0 {
		return "", 0, 0, false
	}
	target := m.yOffset + y
	i := itemAt(m.offsets, target)
	col = x - 1

	start, ht := m.offsets[i], itemHeight(&m, i)
	l := target - start
	if l < ht {
		return m.items[i].ID, l, col, true
	}

	// A gap row: normally the item above's last line, unless it is a
	// leading gap (the first visible row of the view, with no item
	// content shown above it), which snaps to the item below instead.
	if y == 0 && i+1 < len(m.items) {
		return m.items[i+1].ID, 0, col, true
	}
	return m.items[i].ID, max(0, ht-1), col, true
}
