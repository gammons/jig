package wintree

// LayoutNode is the renderable shape of the tree: leaves carry their
// window id and rect; splits carry direction and children. The UI
// walks this to compose window panes (it never sees *node).
type LayoutNode struct {
	Leaf     bool
	ID       LeafID
	Rect     Rect
	Dir      Dir
	Children []LayoutNode
}

// Layout resolves every node's rect within bounds. Children of a
// split divide the parent extent: fixed children (SetFixed) each take
// min(fixed, remaining) cells in order — so fixed children that
// overflow the available extent are shrunk starting from the last one
// — and the remaining flex children then divide what's left equally,
// remainder going to the earliest one cell each, so rects always tile
// exactly. Negative bounds are clamped to zero rather than panicking.
func (t *Tree) Layout(bounds Rect) LayoutNode {
	if bounds.W < 0 {
		bounds.W = 0
	}
	if bounds.H < 0 {
		bounds.H = 0
	}
	return layoutNode(t.root, bounds)
}

func layoutNode(n *node, r Rect) LayoutNode {
	if n.isLeaf() {
		return LayoutNode{Leaf: true, ID: n.id, Rect: r}
	}
	out := LayoutNode{Rect: r, Dir: n.dir, Children: make([]LayoutNode, 0, len(n.children))}
	for i, cr := range childRects(n.dir, n.children, r) {
		out.Children = append(out.Children, layoutNode(n.children[i], cr))
	}
	return out
}

// childRects divides r among children along dir: fixed children take
// min(fixed, remaining) in order (see Layout's doc comment for the
// overflow rule), then flex children split what's left equally,
// remainder to the earliest. This is the single source of truth for
// split geometry — Layout and Split's refusal math (which validates
// via ComputeRects) both depend on it agreeing with itself.
func childRects(dir Dir, children []*node, r Rect) []Rect {
	k := len(children)
	total := r.W
	if dir == SplitStacked {
		total = r.H
	}

	sizes := make([]int, k)
	var flexIdx []int
	remaining := total
	for i, c := range children {
		if c.fixed > 0 {
			n := c.fixed
			if n > remaining {
				n = remaining
			}
			sizes[i] = n
			remaining -= n
		} else {
			flexIdx = append(flexIdx, i)
		}
	}
	switch {
	case len(flexIdx) > 0:
		base, rem := remaining/len(flexIdx), remaining%len(flexIdx)
		for j, idx := range flexIdx {
			s := base
			if j < rem {
				s++
			}
			sizes[idx] = s
		}
	case remaining > 0 && k > 0:
		// Every child is fixed but they didn't use the full extent:
		// hand the leftover to the last one so rects still tile r.
		sizes[k-1] += remaining
	}

	out := make([]Rect, k)
	if dir == SplitSideBySide {
		x := r.X
		for i := range children {
			out[i] = Rect{X: x, Y: r.Y, W: sizes[i], H: r.H}
			x += sizes[i]
		}
	} else {
		y := r.Y
		for i := range children {
			out[i] = Rect{X: r.X, Y: y, W: r.W, H: sizes[i]}
			y += sizes[i]
		}
	}
	return out
}

// ComputeRects flattens Layout into a per-window rect map.
func (t *Tree) ComputeRects(bounds Rect) map[LeafID]Rect {
	out := make(map[LeafID]Rect)
	var walk func(LayoutNode)
	walk = func(n LayoutNode) {
		if n.Leaf {
			out[n.ID] = n.Rect
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t.Layout(bounds))
	return out
}
