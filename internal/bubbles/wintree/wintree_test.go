package wintree

import (
	"reflect"
	"testing"
)

func TestNew_SingleLeaf(t *testing.T) {
	tr, id := New()
	if got := tr.Leaves(); !reflect.DeepEqual(got, []LeafID{id}) {
		t.Fatalf("Leaves() = %v, want [%v]", got, id)
	}
	if tr.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", tr.Len())
	}
}

func TestComputeRects_SingleWindowFillsBounds(t *testing.T) {
	tr, id := New()
	bounds := Rect{X: 0, Y: 0, W: 120, H: 40}
	rects := tr.ComputeRects(bounds)
	if rects[id] != bounds {
		t.Fatalf("rect = %+v, want %+v", rects[id], bounds)
	}
}

func TestComputeRects_NegativeBoundsNoPanic(t *testing.T) {
	tr, id := New()
	rects := tr.ComputeRects(Rect{X: 0, Y: 0, W: -5, H: -10})
	if got, want := rects[id], (Rect{X: 0, Y: 0, W: 0, H: 0}); got != want {
		t.Fatalf("rect = %+v, want %+v", got, want)
	}
}

func TestComputeRects_ZeroBoundsNoPanic(t *testing.T) {
	tr, a := New()
	b, err := tr.Split(a, SplitSideBySide, Rect{X: 0, Y: 0, W: 200, H: 10})
	if err != nil {
		t.Fatal(err)
	}
	rects := tr.ComputeRects(Rect{})
	if rects[a].W != 0 || rects[a].H != 0 || rects[b].W != 0 || rects[b].H != 0 {
		t.Fatalf("rects = %+v, want zero-size", rects)
	}
}
