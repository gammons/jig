package blocklist

// SetSearch sets the search query and returns how many items match; ""
// clears it. An item matches when any of its rendered lines, stripped of
// escapes, contains query case-insensitively. Matching items' visible lines
// are highlighted, and NextMatch/PrevMatch move between them.
func (m *Model) SetSearch(query string) int {
	m.query = query
	iw := m.w - 2
	if query == "" || iw < 1 {
		return 0
	}
	n := 0
	for _, it := range m.items {
		if m.c.matches(it, iw, m.sv, m.styles, m.render, query) {
			n++
		}
	}
	return n
}

// nextMatch selects the next (dir 1) or previous (dir -1) matching item,
// wrapping around; it does nothing without a query or a match.
func (m *Model) nextMatch(dir int) {
	n, iw := len(m.items), m.w-2
	if m.query == "" || n == 0 || iw < 1 {
		return
	}
	for k := 1; k <= n; k++ {
		i := ((m.sel+dir*k)%n + n) % n
		if m.c.matches(m.items[i], iw, m.sv, m.styles, m.render, m.query) {
			m.moveTo(i)
			return
		}
	}
}
