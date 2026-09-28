package blocklist

import (
	"fmt"
	"strings"
	"testing"
)

const benchW, benchH, benchN = 120, 40, 2000

// benchItems returns benchN items of 1–12 lines of ~80 columns each, some
// with SGR styling, as a transcript would have.
func benchItems() []Item {
	items := make([]Item, benchN)
	for i := range items {
		lines := make([]string, 1+i%12)
		for k := range lines {
			lines[k] = fmt.Sprintf("\x1b[1mblock %d\x1b[0m line %d: the quick brown fox jumps over the lazy dog, again and again", i, k)
		}
		items[i] = Item{ID: fmt.Sprintf("b%d", i), Version: 1, Data: strings.Join(lines, "\n")}
	}
	return items
}

func benchList(b *testing.B) Model {
	b.Helper()
	m := New(textRender)
	m.SetSize(benchW, benchH)
	m.SetItems(benchItems())
	_ = m.View()
	return m
}

func BenchmarkBlocklist_View2000(b *testing.B) {
	m := benchList(b)
	m.Select("b1000")
	_ = m.View()
	down, up := keyMsg("j"), keyMsg("k")
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%2 == 0 {
			m, _ = m.Update(down)
		} else {
			m, _ = m.Update(up)
		}
		_ = m.View()
	}
}

func BenchmarkBlocklist_Update2000(b *testing.B) {
	m := benchList(b)
	last := m.items[len(m.items)-1]
	base := last.Data.(string)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		// Streaming: the last item grows by a word per tick, a line every 8.
		text := base + strings.Repeat(" word", i%8)
		if i%8 == 0 {
			text += "\nnew line"
		}
		m.Upsert(Item{ID: last.ID, Version: 2 + i, Data: text})
		_ = m.View()
	}
}

func BenchmarkBlocklist_Load2000(b *testing.B) {
	items := benchItems()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m := New(textRender)
		m.SetSize(benchW, benchH)
		m.SetItems(items)
		_ = m.View()
	}
}
