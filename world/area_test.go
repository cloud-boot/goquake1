// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"testing"

	"github.com/go-quake1/engine/server"
)

// New returns an empty World: nil root, zero nodes used.
func TestNew_EmptyWorld(t *testing.T) {
	w := New()
	if w == nil {
		t.Fatal("New returned nil")
	}
	if w.Root() != nil {
		t.Errorf("fresh World root: got %v want nil", w.Root())
	}
	if w.NumNodes() != 0 {
		t.Errorf("fresh World nodes: got %d want 0", w.NumNodes())
	}
}

// A balanced depth-4 build should allocate exactly 31 nodes
// (1 + 2 + 4 + 8 + 16) -- the upper bound on AreaNodes.
func TestClear_AllocatesBalancedTree(t *testing.T) {
	w := New()
	w.Clear([3]float32{-1024, -1024, -512}, [3]float32{1024, 1024, 512})
	want := 31
	if w.NumNodes() != want {
		t.Errorf("NumNodes after Clear: got %d want %d (depth-4 binary tree)", w.NumNodes(), want)
	}
	if w.Root() == nil {
		t.Fatal("Root is nil after Clear")
	}
}

// The root node of a square-base map (size_x == size_y) must split
// on Y (the upstream tiebreaker -- x>y is the condition for Axis=0,
// and ==y falls into the else branch).
func TestClear_RootAxis_TiebreakerY(t *testing.T) {
	w := New()
	w.Clear([3]float32{-100, -100, -100}, [3]float32{100, 100, 100})
	if w.Root().Axis != 1 {
		t.Errorf("equal-sides root: got Axis=%d want 1 (y, tiebreak)", w.Root().Axis)
	}
}

// X-elongated map: root must split on X.
func TestClear_RootAxis_XElongated(t *testing.T) {
	w := New()
	w.Clear([3]float32{-200, -100, 0}, [3]float32{200, 100, 0})
	if w.Root().Axis != 0 {
		t.Errorf("x-elongated root: got Axis=%d want 0 (x)", w.Root().Axis)
	}
}

// Y-elongated map: root splits on Y.
func TestClear_RootAxis_YElongated(t *testing.T) {
	w := New()
	w.Clear([3]float32{-100, -200, 0}, [3]float32{100, 200, 0})
	if w.Root().Axis != 1 {
		t.Errorf("y-elongated root: got Axis=%d want 1 (y)", w.Root().Axis)
	}
}

// Root.Dist should be the midpoint of mins[axis] + maxs[axis].
func TestClear_RootDistIsMidpoint(t *testing.T) {
	w := New()
	w.Clear([3]float32{-200, -100, 0}, [3]float32{600, 100, 0})
	// X-elongated -> axis=0, midpoint = (-200 + 600)/2 = 200.
	if w.Root().Axis != 0 {
		t.Fatalf("expected axis=0, got %d", w.Root().Axis)
	}
	if w.Root().Dist != 200 {
		t.Errorf("root Dist: got %v want 200", w.Root().Dist)
	}
}

// Leaf nodes (depth == AreaDepth) carry Axis=-1 and nil children.
// Walk to one leaf by following Children[0] (tyrquake's "upper
// half") repeatedly.
func TestClear_LeafSentinel(t *testing.T) {
	w := New()
	w.Clear([3]float32{-100, -100, 0}, [3]float32{100, 100, 0})
	node := w.Root()
	for d := 0; d < server.AreaDepth; d++ {
		if node.Children[0] == nil {
			t.Fatalf("Children[0] nil before reaching leaf at depth %d", d)
		}
		node = node.Children[0]
	}
	// Now at a leaf.
	if node.Axis != -1 {
		t.Errorf("leaf Axis: got %d want -1", node.Axis)
	}
	if node.Children[0] != nil || node.Children[1] != nil {
		t.Errorf("leaf children: got %v / %v want nil / nil", node.Children[0], node.Children[1])
	}
	if node.TriggerEdicts == nil || node.SolidEdicts == nil {
		t.Errorf("leaf edict lists must be initialised: trigger=%v solid=%v",
			node.TriggerEdicts, node.SolidEdicts)
	}
}

// Internal nodes carry initialised (empty) trigger + solid lists.
func TestClear_AllNodesHaveInitialisedLists(t *testing.T) {
	w := New()
	w.Clear([3]float32{-100, -100, 0}, [3]float32{100, 100, 0})
	// Walk every reachable node via DFS.
	var visit func(*AreaNode)
	visit = func(n *AreaNode) {
		if n == nil {
			return
		}
		if n.TriggerEdicts == nil {
			t.Error("found node with nil TriggerEdicts")
		}
		if n.SolidEdicts == nil {
			t.Error("found node with nil SolidEdicts")
		}
		visit(n.Children[0])
		visit(n.Children[1])
	}
	visit(w.Root())
}

// Splitting axis alternates with the rectangle aspect: after the
// first split, each child has half the longer axis -- in a square
// root, that flips the tiebreaker.
func TestClear_AxisFlipsBetweenLevels(t *testing.T) {
	w := New()
	// 200x200 (square) -> root Axis=1 (tiebreak y). Each child is
	// 200 wide x 100 tall -> Axis=0 (x is the longer side now).
	w.Clear([3]float32{-100, -100, 0}, [3]float32{100, 100, 0})
	if w.Root().Axis != 1 {
		t.Fatalf("root axis: got %d want 1", w.Root().Axis)
	}
	if w.Root().Children[0].Axis != 0 {
		t.Errorf("child[0] axis: got %d want 0 (x is now longer)", w.Root().Children[0].Axis)
	}
	if w.Root().Children[1].Axis != 0 {
		t.Errorf("child[1] axis: got %d want 0 (x is now longer)", w.Root().Children[1].Axis)
	}
}

// Re-calling Clear resets the pool entirely -- the second Clear
// must not accumulate nodes from the first.
func TestClear_RebuildResetsPool(t *testing.T) {
	w := New()
	w.Clear([3]float32{-100, -100, 0}, [3]float32{100, 100, 0})
	if w.NumNodes() != 31 {
		t.Fatalf("first Clear nodes: got %d want 31", w.NumNodes())
	}
	w.Clear([3]float32{-50, -50, 0}, [3]float32{50, 50, 0})
	if w.NumNodes() != 31 {
		t.Errorf("second Clear nodes: got %d want 31 (pool reset)", w.NumNodes())
	}
}

// Root.Dist should ALSO get refreshed -- catches a regression where
// Clear forgot to reset Dist along with the pool counter.
func TestClear_RebuildRefreshesDist(t *testing.T) {
	w := New()
	w.Clear([3]float32{-1000, -100, 0}, [3]float32{1000, 100, 0})
	if w.Root().Dist != 0 {
		t.Fatalf("baseline root Dist should be midpoint (0): %v", w.Root().Dist)
	}
	// x-elongated (200 wide, 100 tall) so axis=0; midpoint = (100+300)/2 = 200.
	w.Clear([3]float32{100, -50, 0}, [3]float32{300, 50, 0})
	if w.Root().Axis != 0 {
		t.Fatalf("rebuild axis: got %d want 0", w.Root().Axis)
	}
	if w.Root().Dist != 200 {
		t.Errorf("rebuild root Dist: got %v want 200", w.Root().Dist)
	}
}

// Pool-exhaustion guard: setting used=AreaNodes pretends the static
// budget is consumed. The next allocation must panic -- the C
// upstream calls SV_Error on the same condition; the Go port treats
// it as an unrecoverable invariant violation. The path is only
// reachable if someone mutates World.used directly (Clear resets it
// to 0); a healthy depth-4 build always fits the 32-slot budget.
func TestCreateAreaNode_PoolExhaustedPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on exhausted pool, got none")
		}
	}()
	w := New()
	w.used = server.AreaNodes
	_ = w.createAreaNode(0, [3]float32{0, 0, 0}, [3]float32{1, 1, 1})
}
