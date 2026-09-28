package picker

import (
	"slices"
	"testing"
)

func TestTitleRuneIndexes_Multibyte(t *testing.T) {
	t.Parallel()
	title := "café — hôtel" // byte len 16; rune indexes: c0 a1 f2 é3 sp4 —5 sp6 h7 ô8 t9 e10 l11
	got := titleRuneIndexes(title, []int{0, 3, 10, 999})
	want := []int{0, 3, 7}
	if !slices.Equal(got, want) {
		t.Fatalf("titleRuneIndexes(%q, ...) = %v, want %v", title, got, want)
	}
}

func TestTitleRuneIndexes_EmptyOrOutOfRange(t *testing.T) {
	t.Parallel()
	if got := titleRuneIndexes("abc", nil); got != nil {
		t.Errorf("titleRuneIndexes(nil) = %v, want nil", got)
	}
	if got := titleRuneIndexes("abc", []int{99}); got != nil {
		t.Errorf("titleRuneIndexes(out of range) = %v, want nil", got)
	}
}

func TestFilterItems_ExcludesNonMatches(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "a", Title: "Open file"},
		{ID: "b", Title: "Close tab"},
	}
	matches := filterItems(items, "open", nil, false)
	if len(matches) != 1 || items[matches[0].index].ID != "a" {
		t.Fatalf("filterItems = %+v, want only item a", matches)
	}
}

func TestFilterItems_TieBreakRecencyThenOrder(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "best", Title: "Open"},
		{ID: "tie1", Title: "Reopen document"},
		{ID: "tie2", Title: "Reopen document"},
		{ID: "nomatch", Title: "Close tab"},
	}
	matches := filterItems(items, "open", []string{"tie2"}, true)
	if len(matches) != 3 {
		t.Fatalf("filterItems returned %d matches, want 3 (nomatch excluded): %+v", len(matches), matches)
	}
	got := make([]string, len(matches))
	for i, mt := range matches {
		got[i] = items[mt.index].ID
	}
	want := []string{"best", "tie2", "tie1"}
	if !slices.Equal(got, want) {
		t.Fatalf("filterItems order = %v, want %v (score, then recency tie-break)", got, want)
	}
}

func TestFilterItems_TieBreakOriginalOrderWithoutActions(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "tie1", Title: "Reopen document"},
		{ID: "tie2", Title: "Reopen document"},
	}
	// actions=false: recency is never consulted, so ties keep original order
	// even though tie2 is more recent.
	matches := filterItems(items, "open", []string{"tie2"}, false)
	if len(matches) != 2 || items[matches[0].index].ID != "tie1" || items[matches[1].index].ID != "tie2" {
		t.Fatalf("filterItems (non-Actions) = %+v, want original order [tie1 tie2]", matches)
	}
}

func TestGroupedRows_RecentCapAndGrouping(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "s1", Title: "s1", Group: "Session"},
		{ID: "s2", Title: "s2", Group: "Session"},
		{ID: "t1", Title: "t1", Group: "Tools"},
		{ID: "t2", Title: "t2", Group: "Tools"},
		{ID: "t3", Title: "t3", Group: "Tools"},
		{ID: "t4", Title: "t4", Group: "Tools"},
	}
	recent := []string{"t4", "t3", "t2", "t1", "s2", "s1", "missing"}
	rows := groupedRows(Level{Actions: true}, items, recent)

	if rows[0].kind != rowHeader || rows[0].header != "Recent" {
		t.Fatalf("rows[0] = %+v, want the Recent header", rows[0])
	}
	var recentIDs []string
	i := 1
	for ; rows[i].kind == rowItem; i++ {
		recentIDs = append(recentIDs, rows[i].item.ID)
	}
	want := []string{"t4", "t3", "t2", "t1", "s2"} // capped at 5, missing skipped
	if !slices.Equal(recentIDs, want) {
		t.Fatalf("Recent group = %v, want %v", recentIDs, want)
	}
	if rows[i].kind != rowHeader || rows[i].header != "Session" {
		t.Fatalf("first normal group header = %+v, want Session", rows[i])
	}
}

func TestGroupedRows_NoActionsNoRecentGroup(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "a", Title: "a", Group: "G"}}
	rows := groupedRows(Level{Actions: false}, items, []string{"a"})
	if rows[0].kind != rowHeader || rows[0].header != "G" {
		t.Fatalf("rows[0] = %+v, want the G header (no Recent group without Actions)", rows[0])
	}
}

func TestGroupedRows_UngroupedNoHeader(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "a", Title: "a"}, {ID: "b", Title: "b"}}
	rows := groupedRows(Level{}, items, nil)
	for _, r := range rows {
		if r.kind == rowHeader {
			t.Fatalf("rows = %+v, want no headers for ungrouped items", rows)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2 item rows", rows)
	}
}

func TestMoveRow_SkipsHeadersAndClampsAtEnds(t *testing.T) {
	t.Parallel()
	rows := []row{
		{kind: rowHeader, header: "G"},
		{kind: rowItem, item: Item{ID: "a"}},
		{kind: rowItem, item: Item{ID: "b"}},
		{kind: rowHeader, header: "H"},
		{kind: rowItem, item: Item{ID: "c"}},
	}
	if got := firstItemRow(rows); got != 1 {
		t.Fatalf("firstItemRow = %d, want 1", got)
	}
	if got := moveRow(rows, 1, 1); got != 2 {
		t.Fatalf("moveRow(1,+1) = %d, want 2", got)
	}
	if got := moveRow(rows, 2, 1); got != 4 {
		t.Fatalf("moveRow(2,+1) = %d, want 4 (skips the H header)", got)
	}
	if got := moveRow(rows, 4, 1); got != 4 {
		t.Fatalf("moveRow(4,+1) = %d, want 4 (clamped at the end)", got)
	}
	if got := moveRow(rows, 1, -1); got != 1 {
		t.Fatalf("moveRow(1,-1) = %d, want 1 (clamped at the start)", got)
	}
}
