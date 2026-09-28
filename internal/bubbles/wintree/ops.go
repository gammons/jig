package wintree

// Split divides window id in the given direction within bounds. The
// new window is placed after (below / right of) the source, starts
// flex-sized, and its id is returned. Returns ErrNoRoom when any
// resulting flex-leaf rect would violate MinWidth/MinHeight at the
// current bounds (including leaves nested in shrunk sibling
// subtrees; leaves pinned via SetFixed are exempt from the check),
// ErrNotFound for an unknown id. Refused splits leave the tree
// unchanged and do not consume an id.
func (t *Tree) Split(id LeafID, dir Dir, bounds Rect) (LeafID, error) {
	leaf, parent := t.findLeaf(id)
	if leaf == nil {
		return 0, ErrNotFound
	}

	nid := t.next
	t.next++
	newLeaf := &node{id: nid}

	// Perform the insertion, then validate the actual resulting rects;
	// undo on violation. Checking real geometry (rather than predicting
	// the division) catches nested same-axis leaves inside siblings
	// that the insertion shrinks.
	var undo func()
	if parent != nil && parent.dir == dir {
		idx := childIndex(parent, leaf)
		parent.children = append(parent.children, nil)
		copy(parent.children[idx+2:], parent.children[idx+1:])
		parent.children[idx+1] = newLeaf
		undo = func() {
			parent.children = append(parent.children[:idx+1], parent.children[idx+2:]...)
		}
	} else {
		// Replace the leaf in place with a split node so the parent's
		// child pointer stays valid: the old window moves into child
		// 0, starting flex. The container keeps the leaf's own fixed
		// size — its position relative to ITS parent is unaffected by
		// what's inside it.
		old := &node{id: leaf.id}
		leaf.id = 0
		leaf.dir = dir
		leaf.children = []*node{old, newLeaf}
		undo = func() {
			leaf.id = old.id
			leaf.dir = 0
			leaf.children = nil
		}
	}

	for lid, r := range t.ComputeRects(bounds) {
		l, _ := t.findLeaf(lid)
		if l.fixed > 0 {
			continue
		}
		if r.W < MinWidth || r.H < MinHeight {
			undo()
			t.next--
			return 0, ErrNoRoom
		}
	}
	return nid, nil
}

// Close removes window id, hands its space to its siblings, and
// returns the window that should receive focus (the previous sibling
// subtree's first leaf, or the new first sibling's). Returns
// ErrLastWindow when id is the only window.
func (t *Tree) Close(id LeafID) (LeafID, error) {
	leaf, parent := t.findLeaf(id)
	if leaf == nil {
		return 0, ErrNotFound
	}
	if parent == nil {
		return 0, ErrLastWindow
	}
	idx := childIndex(parent, leaf)
	parent.children = append(parent.children[:idx], parent.children[idx+1:]...)

	var focusNode *node
	if idx > 0 {
		focusNode = parent.children[idx-1]
	} else {
		focusNode = parent.children[0]
	}
	focusID := firstLeaf(focusNode).id

	// A split with one child dissolves: the child takes its place,
	// keeping the split node's own fixed size — its position relative
	// to ITS parent is unaffected by what's inside it.
	if len(parent.children) == 1 {
		only := parent.children[0]
		parent.id = only.id
		parent.dir = only.dir
		parent.children = only.children
	}
	return focusID, nil
}

// Only collapses the tree to just window id (vim ctrl+w o / :only).
func (t *Tree) Only(id LeafID) error {
	leaf, _ := t.findLeaf(id)
	if leaf == nil {
		return ErrNotFound
	}
	t.root = &node{id: leaf.id}
	return nil
}

// Cycle returns the window delta steps from id in tree order,
// wrapping (ctrl+w w). Unknown ids return id unchanged.
func (t *Tree) Cycle(id LeafID, delta int) LeafID {
	ls := t.Leaves()
	for i, l := range ls {
		if l == id {
			n := len(ls)
			return ls[((i+delta)%n+n)%n]
		}
	}
	return id
}

func childIndex(parent, child *node) int {
	for i, c := range parent.children {
		if c == child {
			return i
		}
	}
	return -1
}
