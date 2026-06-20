// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsptrace

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/bspfile"
)

// makeAxialHull builds a tiny synthetic Hull with one axial split
// plane: nodes[0] splits at x = splitX. Going left (x < split) -> solid,
// right -> empty.
func makeAxialHull(splitX float32) *Hull {
	return &Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: plane 0, children = (empty leaf on +x side,
			// solid leaf on -x side).
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: splitX, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
}

// makeNonAxialHull builds a Hull with a single diagonal split plane
// (PlaneAnyZ -- the renderer's hint that the normal isn't an axis).
// Plane normal (1,1,1)/sqrt(3), distance = 5 -> the +side leaf is
// empty, -side is solid.
func makeNonAxialHull() *Hull {
	return &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{
				Normal: [3]float32{0.5773, 0.5773, 0.5773},
				Dist:   5,
				Type:   bspfile.PlaneAnyZ,
			},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
}

// --- axial-plane walk ------------------------------------------------------

func TestHullPointContents_Axial_EmptySide(t *testing.T) {
	h := makeAxialHull(10)
	got, err := HullPointContents(h, 0, [3]float32{20, 0, 0}) // x > 10 -> empty
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsEmpty {
		t.Errorf("got %d want %d (EMPTY)", got, bspfile.ContentsEmpty)
	}
}

func TestHullPointContents_Axial_SolidSide(t *testing.T) {
	h := makeAxialHull(10)
	got, err := HullPointContents(h, 0, [3]float32{5, 0, 0}) // x < 10 -> solid
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsSolid {
		t.Errorf("got %d want %d (SOLID)", got, bspfile.ContentsSolid)
	}
}

func TestHullPointContents_Axial_OnPlane(t *testing.T) {
	// dist == 0 -> not < 0 -> follows child[0] which is EMPTY.
	h := makeAxialHull(10)
	got, _ := HullPointContents(h, 0, [3]float32{10, 0, 0})
	if got != bspfile.ContentsEmpty {
		t.Errorf("on-plane: got %d want EMPTY (children[0] path)", got)
	}
}

// --- non-axial plane walk -------------------------------------------------

func TestHullPointContents_NonAxial(t *testing.T) {
	h := makeNonAxialHull()
	// dot((10,10,10), (0.5773,0.5773,0.5773)) - 5 = 17.3 - 5 = 12.3 > 0 -> EMPTY
	got, _ := HullPointContents(h, 0, [3]float32{10, 10, 10})
	if got != bspfile.ContentsEmpty {
		t.Errorf("non-axial +side: got %d want EMPTY", got)
	}
	// (0,0,0): dot = 0 - 5 = -5 < 0 -> SOLID
	got, _ = HullPointContents(h, 0, [3]float32{0, 0, 0})
	if got != bspfile.ContentsSolid {
		t.Errorf("non-axial -side: got %d want SOLID", got)
	}
}

// --- multi-level walk (two splits) ----------------------------------------

func TestHullPointContents_MultiLevel(t *testing.T) {
	// node 0: split at x=0; +x side goes to node 1, -x side is solid.
	// node 1: split at y=0; +y side is sky, -y side is empty.
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsSolid}}, // children[0]=+side=node1, children[1]=-side=SOLID
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsSky, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 1, 0}, Dist: 0, Type: bspfile.PlaneY},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	// (10, 10, 0) -> +x -> node 1 -> +y -> SKY
	got, _ := HullPointContents(h, 0, [3]float32{10, 10, 0})
	if got != bspfile.ContentsSky {
		t.Errorf("+x+y: got %d want SKY", got)
	}
	// (10, -10, 0) -> +x -> node 1 -> -y -> EMPTY
	got, _ = HullPointContents(h, 0, [3]float32{10, -10, 0})
	if got != bspfile.ContentsEmpty {
		t.Errorf("+x-y: got %d want EMPTY", got)
	}
	// (-10, 0, 0) -> -x -> SOLID
	got, _ = HullPointContents(h, 0, [3]float32{-10, 0, 0})
	if got != bspfile.ContentsSolid {
		t.Errorf("-x: got %d want SOLID", got)
	}
}

// --- error paths -----------------------------------------------------------

func TestHullPointContents_NilHull(t *testing.T) {
	if _, err := HullPointContents(nil, 0, [3]float32{}); err == nil {
		t.Error("expected nil-hull error")
	}
}

func TestHullPointContents_NodeOutOfRange_BelowFirst(t *testing.T) {
	h := makeAxialHull(0)
	h.FirstClipNode = 5
	h.LastClipNode = 10
	if _, err := HullPointContents(h, 0, [3]float32{}); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

func TestHullPointContents_NodeOutOfRange_AboveLast(t *testing.T) {
	h := makeAxialHull(0)
	h.LastClipNode = 0
	if _, err := HullPointContents(h, 99, [3]float32{}); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

func TestHullPointContents_NodeIndexPastSlice(t *testing.T) {
	// FirstClipNode + LastClipNode allow nodenum=2, but ClipNodes
	// slice only has 1 entry.
	h := makeAxialHull(0)
	h.LastClipNode = 10 // allows nodenum up to 10 by the range check
	if _, err := HullPointContents(h, 2, [3]float32{}); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

func TestHullPointContents_BadPlaneIndex(t *testing.T) {
	h := makeAxialHull(0)
	h.ClipNodes[0].PlaneNum = 99 // past planes slice length
	if _, err := HullPointContents(h, 0, [3]float32{}); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex", err)
	}
}

func TestHullPointContents_NegativePlaneIndex(t *testing.T) {
	h := makeAxialHull(0)
	h.ClipNodes[0].PlaneNum = -1
	if _, err := HullPointContents(h, 0, [3]float32{}); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex", err)
	}
}

// --- start-already-a-leaf shortcut ---------------------------------------

func TestHullPointContents_StartAtLeaf(t *testing.T) {
	// nodenum starts negative -> the loop doesn't run; returns the
	// input contents tag verbatim. tyrquake matches this behaviour
	// since the loop condition is `while (num >= 0)`.
	h := makeAxialHull(0)
	got, err := HullPointContents(h, bspfile.ContentsLava, [3]float32{})
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsLava {
		t.Errorf("start-at-leaf: got %d want %d", got, bspfile.ContentsLava)
	}
}

// --- TraceHull -----------------------------------------------------------

func TestDefaultTrace(t *testing.T) {
	tr := DefaultTrace()
	if !tr.AllSolid {
		t.Error("AllSolid should default true")
	}
	if tr.Fraction != 1.0 {
		t.Errorf("Fraction default: %v", tr.Fraction)
	}
}

// p1 and p2 both on the +side (empty) of an axial split -- clean trace.
func TestTraceHull_BothEndpointsEmpty(t *testing.T) {
	h := makeAxialHull(0)
	tr := DefaultTrace()
	ok, err := TraceHull(h, 0, [3]float32{10, 0, 0}, [3]float32{20, 0, 0}, &tr)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if tr.AllSolid || tr.StartSolid || !tr.InOpen {
		t.Errorf("flags: %+v", tr)
	}
}

// p1 and p2 both on the -side (solid) -- AllSolid + StartSolid stay set.
func TestTraceHull_BothEndpointsSolid(t *testing.T) {
	h := makeAxialHull(0)
	tr := DefaultTrace()
	ok, err := TraceHull(h, 0, [3]float32{-10, 0, 0}, [3]float32{-20, 0, 0}, &tr)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !tr.AllSolid || !tr.StartSolid {
		t.Errorf("AllSolid + StartSolid should remain true on all-in-solid: %+v", tr)
	}
}

// Trace that crosses the plane from +side to -side: must hit the plane.
func TestTraceHull_CrossesPlane(t *testing.T) {
	// Plane at x=10, +side empty, -side solid.
	// Walk from x=20 (empty) to x=0 (solid) -- hits at x=10.
	h := makeAxialHull(10)
	tr := DefaultTrace()
	ok, err := TraceHull(h, 0, [3]float32{20, 0, 0}, [3]float32{0, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("ok=true on impact; should be false (trace did not complete to p2)")
	}
	if tr.Fraction >= 1.0 {
		t.Errorf("Fraction: %v should be < 1", tr.Fraction)
	}
	if tr.AllSolid {
		t.Error("AllSolid should be cleared after walking through empty space")
	}
	// Impact's plane normal should match the split plane (we hit the
	// +side, so side==0 -> Plane.Normal unchanged).
	if tr.Plane.Normal[0] != 1 {
		t.Errorf("Plane.Normal: %v", tr.Plane.Normal)
	}
}

// Trace from -side (solid) to +side (empty): the recursive walker
// enters from the -side, marks startsolid, then crosses to +side.
func TestTraceHull_FromSolidToEmpty(t *testing.T) {
	h := makeAxialHull(10)
	tr := DefaultTrace()
	_, err := TraceHull(h, 0, [3]float32{0, 0, 0}, [3]float32{20, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.StartSolid {
		t.Error("StartSolid should be true (started in solid)")
	}
}

// Trace through a non-axial diagonal plane.
func TestTraceHull_NonAxial(t *testing.T) {
	h := makeNonAxialHull()
	tr := DefaultTrace()
	// Walk from (10,10,10) [empty] to (0,0,0) [solid] -- crosses plane.
	_, err := TraceHull(h, 0, [3]float32{10, 10, 10}, [3]float32{0, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Fraction >= 1.0 {
		t.Errorf("expected impact, fraction=%v", tr.Fraction)
	}
}

// Multi-level tree: trace crosses two consecutive boundaries.
func TestTraceHull_MultiLevel(t *testing.T) {
	// node 0: x=0 split, +x -> node 1, -x -> solid.
	// node 1: y=0 split, +y -> empty, -y -> water.
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsSolid}},
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsWater}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 1, 0}, Dist: 0, Type: bspfile.PlaneY},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	tr := DefaultTrace()
	// (10,10,0) [empty, +x+y] -> (10,-10,0) [water, +x-y]: passes through both empty + water.
	ok, err := TraceHull(h, 0, [3]float32{10, 10, 0}, [3]float32{10, -10, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("trace should complete (water is not solid)")
	}
	if !tr.InOpen || !tr.InWater {
		t.Errorf("flags: %+v want InOpen + InWater", tr)
	}
}

// Error path: nil hull / nil trace.
func TestTraceHull_NilHull(t *testing.T) {
	tr := DefaultTrace()
	if _, err := TraceHull(nil, 0, [3]float32{}, [3]float32{}, &tr); err == nil {
		t.Error("expected nil-hull error")
	}
}

func TestTraceHull_NilTrace(t *testing.T) {
	h := makeAxialHull(0)
	if _, err := TraceHull(h, 0, [3]float32{}, [3]float32{}, nil); err == nil {
		t.Error("expected nil-trace error")
	}
}

// Error path: corrupted plane index inside the recursion.
func TestTraceHull_BadPlaneIndex(t *testing.T) {
	h := makeAxialHull(0)
	h.ClipNodes[0].PlaneNum = 99
	tr := DefaultTrace()
	// Need a trace that actually visits the node (both endpoints
	// straddling); simplest is a trace that splits.
	if _, err := TraceHull(h, 0, [3]float32{-10, 0, 0}, [3]float32{10, 0, 0}, &tr); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex", err)
	}
}

// Error path: corrupted node index inside the recursion.
func TestTraceHull_BadNodeIndex(t *testing.T) {
	h := makeAxialHull(0)
	h.ClipNodes[0].Children = [2]int16{99, 99} // both children point past the slice
	tr := DefaultTrace()
	if _, err := TraceHull(h, 0, [3]float32{20, 0, 0}, [3]float32{30, 0, 0}, &tr); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

// Test the back-off-from-solid loop: build a hull where the
// far-side is solid AND mid lands inside solid space due to
// epsilon bias. The loop should back off frac by 0.1 until it
// escapes, OR give up + return false with EndPos at the final mid.
// This is genuinely hard to provoke with a simple synthetic hull;
// pin the contract by walking onto a side whose contents we control.
func TestTraceHull_ImpactWithFarSideSolid(t *testing.T) {
	h := makeAxialHull(0) // +x empty, -x solid
	tr := DefaultTrace()
	// Trace from x=10 to x=-10 -- crosses plane at x=0.
	// After crossing, far side is solid -> impact recorded.
	_, err := TraceHull(h, 0, [3]float32{10, 0, 0}, [3]float32{-10, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Fraction >= 1.0 {
		t.Errorf("expected impact, fraction=%v", tr.Fraction)
	}
}

// Cover line 164-166: err propagation from the near-side recursion.
// Build a 3-level hull where the near-side recursion descends two
// more levels and hits a bad PlaneNum.
func TestTraceHull_ErrFromNearRecursion(t *testing.T) {
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: x=0, +x -> node 1, -x -> EMPTY
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsEmpty}},
			// node 1: x=5, +x -> node 2 (bad), -x -> EMPTY
			{PlaneNum: 1, Children: [2]int16{2, bspfile.ContentsEmpty}},
			// node 2: BAD plane index
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{1, 0, 0}, Dist: 5, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  2,
	}
	tr := DefaultTrace()
	// Trace from (10,0,0) to (-10,0,0): approaches from +side
	// (side=0 at node 0), near=children[0]=node 1, recurse, hits
	// node 2's bad plane via near-side propagation.
	if _, err := TraceHull(h, 0, [3]float32{10, 0, 0}, [3]float32{-10, 0, 0}, &tr); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex (from near recursion)", err)
	}
}

// Cover the line-107 guard: nodenum is inside the range
// FirstClipNode..LastClipNode but past the ClipNodes slice itself.
func TestTraceHull_NodeIndexPastClipNodes(t *testing.T) {
	h := makeAxialHull(0)
	h.LastClipNode = 50 // allow nodenum up to 50
	tr := DefaultTrace()
	// Force the walker to land on a child=10 (past the 1-entry slice).
	h.ClipNodes[0].Children = [2]int16{10, bspfile.ContentsSolid}
	if _, err := TraceHull(h, 0, [3]float32{20, 0, 0}, [3]float32{30, 0, 0}, &tr); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

// Cover the side==1 branch (impact on -side -> plane normal flipped).
// Build a hull where +x is SOLID, -x is EMPTY (the inverse of the
// makeAxialHull helper), then trace from -x to +x: the walker enters
// from the -side (side=1) and hits the impact going to the +side.
func TestTraceHull_SideOneFlipsPlaneNormal(t *testing.T) {
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			// children[0] = +side = SOLID, children[1] = -side = EMPTY
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 10, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	tr := DefaultTrace()
	// From (0,0,0) [empty, -side] to (20,0,0) [solid, +side] -- hits the wall.
	_, err := TraceHull(h, 0, [3]float32{0, 0, 0}, [3]float32{20, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Fraction >= 1.0 {
		t.Errorf("expected impact, fraction=%v", tr.Fraction)
	}
	// side==1 path -> Plane.Normal is flipped (-1,0,0) and Dist
	// flipped (-10).
	if tr.Plane.Normal[0] != -1 || tr.Plane.Dist != -10 {
		t.Errorf("Plane should be flipped on side==1: normal=%v dist=%v", tr.Plane.Normal, tr.Plane.Dist)
	}
}

// Cover ErrBadPlaneIndex propagation from inside the recursive walk
// AFTER a successful first descent (line 175 path).
func TestTraceHull_BadPlaneInRecursion(t *testing.T) {
	// 2-node tree where node 1 has a bad plane. Trace forces a
	// crossing at node 0 -> descend to node 1 -> recurse there,
	// trip ErrBadPlaneIndex.
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: x=0 split, +side -> node 1, -side -> EMPTY
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsEmpty}},
			// node 1: corrupt PlaneNum
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	tr := DefaultTrace()
	// (-10,0,0) [-side -> EMPTY] to (10,0,0) [+side -> node 1, bad plane]
	if _, err := TraceHull(h, 0, [3]float32{-10, 0, 0}, [3]float32{10, 0, 0}, &tr); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex", err)
	}
}

// Cover the !ok early-out at line 167 (the near-side recursion
// returned false). Construct a tree where the near-side trace
// terminates without completing -- a chained impact.
func TestTraceHull_NearSideTerminates(t *testing.T) {
	// 2-node tree:
	//   node 0: x=0 split, +x -> node 1, -x -> EMPTY
	//   node 1: x=5 split, +x -> SOLID, -x -> SOLID  (both children solid)
	// Trace from (-10,0,0) to (10,0,0): crosses node 0, walks +side
	// (node 1) -> both children solid -> trace terminates early.
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsEmpty}},
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{1, 0, 0}, Dist: 5, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	tr := DefaultTrace()
	_, err := TraceHull(h, 0, [3]float32{-10, 0, 0}, [3]float32{10, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
}

// Test frac clamps: dist1 tiny + dist2 huge -> frac just barely
// negative -> clamped to 0. Construct: p1 just on the -side of
// plane x=0, p2 far on the +side.
func TestTraceHull_FracClampLow(t *testing.T) {
	h := makeAxialHull(0) // +x EMPTY, -x SOLID
	tr := DefaultTrace()
	// p1 = (-0.001, 0, 0) just inside solid, p2 = (1000, 0, 0) far in empty.
	// dist1 = -0.001, dist2 = 1000.
	// frac = (dist1 + DistEpsilon) / (dist1 - dist2)
	//      = (-0.001 + 0.03125) / (-0.001 - 1000)
	//      = 0.030 / -1000.001 = very small negative -> clamp to 0.
	_, err := TraceHull(h, 0, [3]float32{-0.001, 0, 0}, [3]float32{1000, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.StartSolid {
		t.Error("startsolid expected")
	}
}

// Test frac>1 clamp: dist1 huge positive, dist2 just barely
// positive then turning negative. Construct so dist1 ~ 10,
// dist2 ~ 0.5 -- frac = (10 - 0.03125)/(10 - 0.5) > 1 -> clamp.
// Need dist1 and dist2 to have opposite signs to enter the
// straddle path; this construction won't enter it. Skip explicit
// frac>1 unless straightforward.

// Cover the "far side solid + AllSolid still true" early-out
// (line 182). Both endpoints in solid space, recursion never
// flipped AllSolid to false. Build a single-node hull whose ONLY
// child is SOLID, then trace through it.
func TestTraceHull_AllSolidPreservedOnImpact(t *testing.T) {
	// Hull where both children of node 0 are SOLID. Any straddling
	// trace through this hull stays in solid throughout -- AllSolid
	// should remain true + the impact early-out fires.
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	tr := DefaultTrace()
	_, err := TraceHull(h, 0, [3]float32{-10, 0, 0}, [3]float32{10, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.AllSolid {
		t.Error("AllSolid should remain true throughout all-solid trace")
	}
}

// --- DistEpsilon constant ------------------------------------------------

func TestDistEpsilonValue(t *testing.T) {
	if DistEpsilon != 0.03125 {
		t.Errorf("DistEpsilon drift: %v", DistEpsilon)
	}
}
