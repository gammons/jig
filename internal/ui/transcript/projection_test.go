package transcript

import "testing"

func TestAddNotice_AppendsBlockWithNextOrdinal(t *testing.T) {
	p := New(root)
	id1 := p.AddNotice("could not load subagent: boom", LevelError)
	id2 := p.AddNotice("info notice", LevelInfo)
	if id1 != "n/0" || id2 != "n/1" {
		t.Fatalf("ids = %q, %q, want n/0, n/1", id1, id2)
	}
	blocks := p.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(blocks))
	}
	if blocks[0].Kind != KindNotice || blocks[0].Text != "could not load subagent: boom" || blocks[0].Level != LevelError {
		t.Errorf("block 0 = %+v, want a LevelError notice with that text", blocks[0])
	}
	if blocks[1].Kind != KindNotice || blocks[1].Text != "info notice" || blocks[1].Level != LevelInfo {
		t.Errorf("block 1 = %+v, want a LevelInfo notice with that text", blocks[1])
	}
}
