// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender

import (
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/render"
)

// ---------------------------------------------------------------------------
// SurfaceList primitives
// ---------------------------------------------------------------------------

func TestSurfaceList_AppendAndLen(t *testing.T) {
	var l SurfaceList
	if l.Len() != 0 {
		t.Fatalf("fresh list Len = %d, want 0", l.Len())
	}
	l.Append(SurfaceRef{FaceIdx: 7, LeafIdx: 3})
	l.Append(SurfaceRef{FaceIdx: 9, LeafIdx: 3})
	if l.Len() != 2 {
		t.Fatalf("Len after 2 appends = %d, want 2", l.Len())
	}
	if l.Refs[0].FaceIdx != 7 || l.Refs[1].FaceIdx != 9 {
		t.Fatalf("Refs = %+v, want [7 9]", l.Refs)
	}
}

func TestSurfaceList_ResetPreservesCapacity(t *testing.T) {
	l := SurfaceList{Refs: make([]SurfaceRef, 0, 8)}
	l.Append(SurfaceRef{FaceIdx: 1})
	l.Append(SurfaceRef{FaceIdx: 2})
	capBefore := cap(l.Refs)
	l.Reset()
	if l.Len() != 0 {
		t.Fatalf("after Reset Len = %d, want 0", l.Len())
	}
	if cap(l.Refs) != capBefore {
		t.Fatalf("Reset changed cap (%d -> %d); should reuse backing array", capBefore, cap(l.Refs))
	}
	// Reusing the list must work too.
	l.Append(SurfaceRef{FaceIdx: 99})
	if l.Len() != 1 || l.Refs[0].FaceIdx != 99 {
		t.Fatalf("post-Reset append broken: %+v", l.Refs)
	}
}

// ---------------------------------------------------------------------------
// WalkWorld - error paths
// ---------------------------------------------------------------------------

func TestWalkWorld_NilList(t *testing.T) {
	ctx := WalkContext{NumNodes: 1}
	err := WalkWorld(ctx, 0, [3]float32{}, render.Frustum{}, 1, nil)
	if !errors.Is(err, ErrWalkNilList) {
		t.Fatalf("err = %v, want ErrWalkNilList", err)
	}
}

func TestWalkWorld_RootRange(t *testing.T) {
	ctx := WalkContext{NumNodes: 3}
	var out SurfaceList
	if err := WalkWorld(ctx, -1, [3]float32{}, render.Frustum{}, 1, &out); !errors.Is(err, ErrWalkRootRange) {
		t.Fatalf("rootIdx=-1: err = %v, want ErrWalkRootRange", err)
	}
	if err := WalkWorld(ctx, 3, [3]float32{}, render.Frustum{}, 1, &out); !errors.Is(err, ErrWalkRootRange) {
		t.Fatalf("rootIdx=NumNodes: err = %v, want ErrWalkRootRange", err)
	}
}

// ---------------------------------------------------------------------------
// fakeBSP: a synthetic, hand-wired BSP for the walk tests.
// ---------------------------------------------------------------------------

// fakeBSP backs WalkContext with plain slices indexed by ID. Nodes
// and leaves share an ID space (so a NodeChildren entry can resolve
// to either; NodeKind tells the walker which). To keep dispatch
// trivial, IDs 0..numNodes-1 are nodes and numNodes..numNodes+numLeaves-1
// are leaves; the helper translates leaf indices into the leaves slice.
type fakeBSP struct {
	numNodes    int
	numLeaves   int
	nodePlanes  []render.Plane
	nodeKids    [][2]int
	nodeMins    [][3]float32
	nodeMaxs    [][3]float32
	nodeVis     []int32
	leafVis     []int32
	leafFaces   [][]int
	leafIsEmpty []bool
}

func (b *fakeBSP) leafIdx(id int) int { return id - b.numNodes }

func (b *fakeBSP) ctx() WalkContext {
	return WalkContext{
		NumNodes:  b.numNodes,
		NumLeaves: b.numLeaves,
		NodeKind: func(id int) NodeKind {
			if id < b.numNodes {
				return NodeKindInterior
			}
			if b.leafIsEmpty[b.leafIdx(id)] {
				return NodeKindEmpty
			}
			return NodeKindLeaf
		},
		NodeChildren: func(id int) [2]int { return b.nodeKids[id] },
		NodePlane:    func(id int) render.Plane { return b.nodePlanes[id] },
		NodeBBox: func(id int) (mins, maxs [3]float32) {
			return b.nodeMins[id], b.nodeMaxs[id]
		},
		NodeVisFrame: func(id int) int32 { return b.nodeVis[id] },
		LeafVisFrame: func(id int) int32 { return b.leafVis[b.leafIdx(id)] },
		LeafFaces:    func(id int) []int { return b.leafFaces[b.leafIdx(id)] },
	}
}

// hugeBox returns a culling bbox that always intersects any frustum.
// Used by every test except the explicit frustum-cull case.
func hugeBox() ([3]float32, [3]float32) {
	const big = float32(1e6)
	return [3]float32{-big, -big, -big}, [3]float32{big, big, big}
}

// noopFrustum returns a Frustum with normals pointing AWAY from the
// origin so every point lies in the positive half-space and BoxInFrustum
// always returns true (provided the box reaches into all positive half-
// spaces, hence hugeBox above). All 4 planes are identical; that's fine
// because BoxInFrustum is a conjunction.
func noopFrustum() render.Frustum {
	pl := render.Plane{
		Normal: [3]float32{1, 0, 0},
		Dist:   -float32(math.MaxFloat32 / 2),
	}
	return render.Frustum{pl, pl, pl, pl}
}

// alwaysOutsideFrustum returns a Frustum whose first plane has its
// inward normal far beyond the test's bbox -- BoxInFrustum returns
// false for any "normal" box.
func alwaysOutsideFrustum() render.Frustum {
	pl := render.Plane{
		Normal: [3]float32{1, 0, 0},
		Dist:   float32(math.MaxFloat32 / 2),
	}
	return render.Frustum{pl, pl, pl, pl}
}

// makeTwoLeafBSP builds:
//
//	     node 0 (plane: x = 0, normal +X)
//	    /         \
//	 leaf 1      leaf 2
//	(id 1)       (id 2)
//	faces:       faces:
//	[10,11]      [20]
//
// Leaf IDs are nodeCount(1) + leafIndex. So id 1 = leaf[0], id 2 = leaf[1].
func makeTwoLeafBSP() *fakeBSP {
	mins, maxs := hugeBox()
	return &fakeBSP{
		numNodes:    1,
		numLeaves:   2,
		nodePlanes:  []render.Plane{{Normal: [3]float32{1, 0, 0}, Dist: 0}},
		nodeKids:    [][2]int{{1, 2}}, // child[0]=front(leaf at id 1), child[1]=back(leaf at id 2)
		nodeMins:    [][3]float32{mins},
		nodeMaxs:    [][3]float32{maxs},
		nodeVis:     []int32{1},
		leafVis:     []int32{1, 1},
		leafFaces:   [][]int{{10, 11}, {20}},
		leafIsEmpty: []bool{false, false},
	}
}

func TestWalkWorld_TwoLeaf_AllVisible_FrontFirst(t *testing.T) {
	b := makeTwoLeafBSP()
	var out SurfaceList
	// Viewer at x=+5 -> in front of plane (normal +X, dist 0) ->
	// child[0] (leaf id 1, faces [10,11]) visited first.
	err := WalkWorld(b.ctx(), 0, [3]float32{5, 0, 0}, noopFrustum(), 1, &out)
	if err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{10, 11, 20}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("face order = %v, want %v", faceIDs(out.Refs), want)
	}
	// LeafIdx tagging:
	if out.Refs[0].LeafIdx != 1 || out.Refs[2].LeafIdx != 2 {
		t.Fatalf("leaf tagging = %+v", out.Refs)
	}
}

func TestWalkWorld_TwoLeaf_ViewerBehind_BackFirst(t *testing.T) {
	b := makeTwoLeafBSP()
	var out SurfaceList
	// Viewer at x=-5 -> behind plane -> child[1] (leaf id 2, face [20])
	// visited first; then child[0] (faces [10,11]).
	err := WalkWorld(b.ctx(), 0, [3]float32{-5, 0, 0}, noopFrustum(), 1, &out)
	if err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{20, 10, 11}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("face order = %v, want %v", faceIDs(out.Refs), want)
	}
}

func TestWalkWorld_TwoLeaf_ViewerOnPlane_FrontFirst(t *testing.T) {
	b := makeTwoLeafBSP()
	var out SurfaceList
	// Viewer exactly on the splitting plane (x=0). PlaneSide returns 0.
	// We treat 0 as "front" (matches tyrquake's `side = (dot >= 0) ? 0 : 1`),
	// so child[0] is visited first.
	err := WalkWorld(b.ctx(), 0, [3]float32{0, 0, 0}, noopFrustum(), 1, &out)
	if err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{10, 11, 20}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("face order = %v, want %v (on-plane viewer must visit child[0] first)", faceIDs(out.Refs), want)
	}
}

func TestWalkWorld_PVSCullsLeaf(t *testing.T) {
	b := makeTwoLeafBSP()
	// Stale-stamp leaf 2; only leaf 1's faces should appear.
	b.leafVis[1] = 0
	var out SurfaceList
	if err := WalkWorld(b.ctx(), 0, [3]float32{5, 0, 0}, noopFrustum(), 1, &out); err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{10, 11}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("face list = %v, want %v", faceIDs(out.Refs), want)
	}
}

func TestWalkWorld_PVSCullsSubtree(t *testing.T) {
	b := makeTwoLeafBSP()
	// Stale-stamp the root node; the whole subtree must drop out
	// before we even read leaf data.
	b.nodeVis[0] = 0
	var out SurfaceList
	if err := WalkWorld(b.ctx(), 0, [3]float32{5, 0, 0}, noopFrustum(), 1, &out); err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("subtree cull failed; got %v", faceIDs(out.Refs))
	}
}

func TestWalkWorld_FrustumCullsSubtree(t *testing.T) {
	b := makeTwoLeafBSP()
	var out SurfaceList
	// Use a frustum that rejects every reasonable box; the root node
	// fails the BoxInFrustum test before its children are visited.
	if err := WalkWorld(b.ctx(), 0, [3]float32{5, 0, 0}, alwaysOutsideFrustum(), 1, &out); err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("frustum cull failed; got %v", faceIDs(out.Refs))
	}
}

func TestWalkWorld_EmptyLeafIsSkipped(t *testing.T) {
	b := makeTwoLeafBSP()
	// Mark leaf 2 as empty (the outside-the-map sentinel kind).
	b.leafIsEmpty[1] = true
	var out SurfaceList
	if err := WalkWorld(b.ctx(), 0, [3]float32{5, 0, 0}, noopFrustum(), 1, &out); err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{10, 11}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("face list = %v, want %v (empty leaf 2 must contribute nothing)", faceIDs(out.Refs), want)
	}
}

// ---------------------------------------------------------------------------
// Deeper synthetic tree (3 levels) to exercise nested recursion.
// ---------------------------------------------------------------------------
//
//	         node 0  (plane: x = 0)
//	        /              \
//	    node 1              node 2
//	  (plane: y=0)        (plane: y=0)
//	   /     \              /     \
//	leaf3   leaf4        leaf5   leaf6
//	id=3    id=4         id=5    id=6
//	[100]   [101]        [102]   [103]
//
// Node IDs 0..2; leaf IDs 3..6 (because numNodes=3, leafIdx=id-3).
func makeThreeLevelBSP() *fakeBSP {
	mins, maxs := hugeBox()
	return &fakeBSP{
		numNodes:  3,
		numLeaves: 4,
		nodePlanes: []render.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0},
			{Normal: [3]float32{0, 1, 0}, Dist: 0},
			{Normal: [3]float32{0, 1, 0}, Dist: 0},
		},
		nodeKids: [][2]int{
			{1, 2}, // root: +X -> node 1, -X -> node 2
			{3, 4}, // node 1: +Y -> leaf 3, -Y -> leaf 4
			{5, 6}, // node 2: +Y -> leaf 5, -Y -> leaf 6
		},
		nodeMins:    [][3]float32{mins, mins, mins},
		nodeMaxs:    [][3]float32{maxs, maxs, maxs},
		nodeVis:     []int32{1, 1, 1},
		leafVis:     []int32{1, 1, 1, 1},
		leafFaces:   [][]int{{100}, {101}, {102}, {103}},
		leafIsEmpty: []bool{false, false, false, false},
	}
}

func TestWalkWorld_ThreeLevel_FrontFront(t *testing.T) {
	b := makeThreeLevelBSP()
	var out SurfaceList
	// Viewer at (+5, +5, 0): front of root (visit node 1 first), front
	// of node 1 (visit leaf 3 first). Then leaf 4. Then we descend into
	// node 2: still on +Y side -> leaf 5 first, leaf 6 last.
	err := WalkWorld(b.ctx(), 0, [3]float32{5, 5, 0}, noopFrustum(), 1, &out)
	if err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{100, 101, 102, 103}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("front-front order = %v, want %v", faceIDs(out.Refs), want)
	}
}

func TestWalkWorld_ThreeLevel_BackBack(t *testing.T) {
	b := makeThreeLevelBSP()
	var out SurfaceList
	// Viewer at (-5, -5, 0): behind root (visit node 2 first), behind
	// node 2 (leaf 6 first, leaf 5 second). Then node 1: behind ->
	// leaf 4 first, leaf 3 second.
	err := WalkWorld(b.ctx(), 0, [3]float32{-5, -5, 0}, noopFrustum(), 1, &out)
	if err != nil {
		t.Fatalf("WalkWorld: %v", err)
	}
	want := []int{103, 102, 101, 100}
	if !equalFaceIdx(out.Refs, want) {
		t.Fatalf("back-back order = %v, want %v", faceIDs(out.Refs), want)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func faceIDs(refs []SurfaceRef) []int {
	out := make([]int, len(refs))
	for i, r := range refs {
		out[i] = r.FaceIdx
	}
	return out
}

func equalFaceIdx(refs []SurfaceRef, want []int) bool {
	if len(refs) != len(want) {
		return false
	}
	for i, r := range refs {
		if r.FaceIdx != want[i] {
			return false
		}
	}
	return true
}
