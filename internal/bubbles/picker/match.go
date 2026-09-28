package picker

import (
	"sort"

	"github.com/sahilm/fuzzy"
)

// rowKind distinguishes a group header from a selectable item row.
type rowKind int

const (
	rowHeader rowKind = iota
	rowItem
)

// row is one displayed line: either a group header or an item, carrying
// the rune indexes into Item.Title that a fuzzy match hit (nil when the
// query is empty).
type row struct {
	kind    rowKind
	header  string
	item    Item
	matched []int
}

// firstItemRow returns the index of the first selectable row, or -1.
func firstItemRow(rows []row) int {
	for i, r := range rows {
		if r.kind == rowItem {
			return i
		}
	}
	return -1
}

// currentRow returns the index of the row whose item is Current, or -1.
func currentRow(rows []row) int {
	for i, r := range rows {
		if r.kind == rowItem && r.item.Current {
			return i
		}
	}
	return -1
}

// moveRow returns the nearest item row from cur in direction dir (+1/-1),
// skipping headers. It returns cur unchanged when there is no item row in
// that direction (no wrapping).
func moveRow(rows []row, cur, dir int) int {
	for i := cur + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].kind == rowItem {
			return i
		}
	}
	return cur
}

// groupedRows lays out items for an empty query: grouped by Item.Group in
// first-appearance order under headers (no header for the "" group), with
// an Actions level's Recent group (from recent, capped at 5) leading.
func groupedRows(level Level, items []Item, recent []string) []row {
	var rows []row
	if level.Actions {
		if rec := recentGroup(items, recent, 5); len(rec) > 0 {
			rows = append(rows, row{kind: rowHeader, header: "Recent"})
			for _, it := range rec {
				rows = append(rows, row{kind: rowItem, item: it})
			}
		}
	}

	var order []string
	seen := map[string]bool{}
	groups := map[string][]Item{}
	for _, it := range items {
		if !seen[it.Group] {
			seen[it.Group] = true
			order = append(order, it.Group)
		}
		groups[it.Group] = append(groups[it.Group], it)
	}
	for _, g := range order {
		if g != "" {
			rows = append(rows, row{kind: rowHeader, header: g})
		}
		for _, it := range groups[g] {
			rows = append(rows, row{kind: rowItem, item: it})
		}
	}
	return rows
}

// recentGroup returns the items whose ID is in recent (most recent first),
// skipping IDs no longer present, capped at n.
func recentGroup(items []Item, recent []string, n int) []Item {
	byID := make(map[string]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	var out []Item
	for _, id := range recent {
		if len(out) >= n {
			break
		}
		if it, ok := byID[id]; ok {
			out = append(out, it)
		}
	}
	return out
}

// filteredRows fuzzy-matches items against query (over Title + " " +
// Detail), ranked by score; ties go to recency (Actions levels only),
// then original order.
func filteredRows(level Level, items []Item, query string, recent []string) []row {
	matches := filterItems(items, query, recent, level.Actions)
	rows := make([]row, len(matches))
	for i, mt := range matches {
		rows[i] = row{kind: rowItem, item: items[mt.index], matched: mt.matched}
	}
	return rows
}

// filterMatch is one item's fuzzy-match result.
type filterMatch struct {
	index   int
	matched []int
	score   int
}

// itemSource is a fuzzy.Source over items' "Title Detail" text.
type itemSource []Item

func (s itemSource) Len() int { return len(s) }
func (s itemSource) String(i int) string {
	if s[i].Detail == "" {
		return s[i].Title
	}
	return s[i].Title + " " + s[i].Detail
}

func filterItems(items []Item, query string, recent []string, actions bool) []filterMatch {
	if query == "" {
		return nil
	}
	matches := fuzzy.FindFromNoSort(query, itemSource(items))
	rank := recencyRank(recent)
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if !actions {
			return false
		}
		ri := rank(items[matches[i].Index].ID)
		rj := rank(items[matches[j].Index].ID)
		return ri < rj
	})
	out := make([]filterMatch, len(matches))
	for i, mt := range matches {
		out[i] = filterMatch{
			index:   mt.Index,
			matched: titleRuneIndexes(items[mt.Index].Title, mt.MatchedIndexes),
			score:   mt.Score,
		}
	}
	return out
}

// recencyRank returns a function ranking an ID by its position in recent
// (0 = most recent); an ID absent from recent ranks after every present ID.
func recencyRank(recent []string) func(id string) int {
	idx := make(map[string]int, len(recent))
	for i, id := range recent {
		idx[id] = i
	}
	return func(id string) int {
		if r, ok := idx[id]; ok {
			return r
		}
		return len(recent)
	}
}

// titleRuneIndexes converts byte offsets into the "Title Detail" matched
// string (as sahilm/fuzzy reports them, byte offsets, not rune indexes)
// into rune indexes within title, keeping only offsets inside title's
// byte span so a match inside Detail doesn't highlight the wrong rune.
func titleRuneIndexes(title string, byteIdx []int) []int {
	if len(byteIdx) == 0 {
		return nil
	}
	want := make(map[int]bool, len(byteIdx))
	for _, b := range byteIdx {
		if b < len(title) {
			want[b] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	var out []int
	ri := 0
	for bi := range title {
		if want[bi] {
			out = append(out, ri)
		}
		ri++
	}
	return out
}
