// Package wintree owns a window split tree for a UI region: internal
// nodes are splits (direction + children, divided equally unless a
// child is pinned to a fixed size via SetFixed); leaves are windows
// identified by a stable LeafID. Pure data + geometry: no UI
// dependencies.
package wintree

import "errors"

// Dir is a split direction, named by visual result to avoid vim's
// confusing horizontal/vertical terminology.
type Dir int

const (
	// SplitStacked is vim's :sp — children stack top-to-bottom.
	SplitStacked Dir = iota
	// SplitSideBySide is vim's :vsp — children sit left-to-right.
	SplitSideBySide
)

// NavDir is a geometric focus-navigation direction (ctrl+w h/j/k/l).
type NavDir int

const (
	NavLeft NavDir = iota
	NavDown
	NavUp
	NavRight
)

// LeafID identifies a window. IDs are stable for the window's
// lifetime and never reused within a Tree.
type LeafID int

// Rect is a window rectangle in screen cells. Rects produced by
// ComputeRects tile the bounds exactly.
type Rect struct {
	X, Y, W, H int
}

// Minimum flex-leaf rect sizes. Leaves pinned via SetFixed are exempt
// from these checks.
const (
	MinWidth  = 20
	MinHeight = 3
)

var (
	ErrNotFound   = errors.New("wintree: no such window")
	ErrNoRoom     = errors.New("wintree: not enough room")
	ErrLastWindow = errors.New("wintree: cannot close last window")
)

// node is either a leaf (len(children) == 0; id valid) or a split
// (dir/children valid). fixed pins the node to that many cells along
// its parent split's axis; 0 means it shares space equally with its
// unfixed siblings (flex).
type node struct {
	id       LeafID
	fixed    int
	dir      Dir
	children []*node
}

func (n *node) isLeaf() bool { return len(n.children) == 0 }

// Tree is the window tree. Zero value is not usable; construct with New.
type Tree struct {
	root *node
	next LeafID
}

// New returns a tree with a single window, and that window's id.
func New() (*Tree, LeafID) {
	t := &Tree{next: 2}
	t.root = &node{id: 1}
	return t, 1
}

// Len returns the number of windows.
func (t *Tree) Len() int { return len(t.Leaves()) }

// Leaves returns all window ids in tree (depth-first, left-to-right /
// top-to-bottom) order.
func (t *Tree) Leaves() []LeafID {
	var out []LeafID
	var walk func(n *node)
	walk = func(n *node) {
		if n.isLeaf() {
			out = append(out, n.id)
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(t.root)
	return out
}

// findLeaf returns the leaf with the given id and its parent split
// (parent == nil when the leaf is the root). nil leaf means not found.
func (t *Tree) findLeaf(id LeafID) (leaf, parent *node) {
	var walk func(n, p *node) (*node, *node)
	walk = func(n, p *node) (*node, *node) {
		if n.isLeaf() {
			if n.id == id {
				return n, p
			}
			return nil, nil
		}
		for _, c := range n.children {
			if l, lp := walk(c, n); l != nil {
				return l, lp
			}
		}
		return nil, nil
	}
	return walk(t.root, nil)
}

// SetFixed pins id to n cells along its parent split's axis (0 =
// flex, share space equally with its unfixed siblings). Returns
// ErrNotFound if id does not exist.
func (t *Tree) SetFixed(id LeafID, n int) error {
	leaf, _ := t.findLeaf(id)
	if leaf == nil {
		return ErrNotFound
	}
	leaf.fixed = n
	return nil
}

// firstLeaf returns the first (tree-order) leaf under n.
func firstLeaf(n *node) *node {
	for !n.isLeaf() {
		n = n.children[0]
	}
	return n
}
