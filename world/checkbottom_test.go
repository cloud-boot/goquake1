// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// makeWorldWithFloor returns a brushmodel whose hull 0 has a single
// horizontal plane at z=0: z >= 0 is empty (the open half-space
// above), z < 0 is solid (the floor extending infinitely downward).
// Down-traces from z > 0 impact at z=0 (pulled in by DistEpsilon).
func makeWorldWithFloor() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// makeWorldWithStep returns a brushmodel whose hull 0 has solid in
// the half-space x < 0 with floor at z=0, and a much lower floor for
// x >= 0 (effectively no floor for that half within the trace range).
// Built as a 2-plane BSP: first split on X, then on Z.
//
// (-, _): split on Z=0, below=solid (floor), above=empty.
// (+, _): always empty (no floor in 2*StepSize range).
func makeWorldWithStep() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: split on X at 0. x>=0 -> empty (no floor). x<0 -> node 1.
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, 1}},
			// node 1: split on Z at 0. z>=0 -> empty. z<0 -> solid.
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	return bm
}

// FlatFloorSupported: entity standing on a flat infinite floor --
// all four corners impact the same plane at z=0, the center floor
// matches every corner floor, so mid - corner = 0 < StepSize.
func TestCheckBottom_FlatFloorSupported(t *testing.T) {
	in := CheckBottomIn{
		Origin: [3]float32{0, 0, 1}, // 1 unit above the floor
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
	}
	ok, err := CheckBottom(in, makeWorldWithFloor(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Error("expected CheckBottom = true on a flat floor")
	}
}

// CenterHovering: the entity is far above any floor -- the center
// down-trace doesn't hit anything within 2*StepSize, so CheckBottom
// must return false WITHOUT running the corner traces.
func TestCheckBottom_CenterHovering(t *testing.T) {
	in := CheckBottomIn{
		Origin: [3]float32{0, 0, 1000}, // 1000 units above the floor
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
	}
	ok, err := CheckBottom(in, makeWorldWithFloor(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("expected CheckBottom = false when hovering")
	}
}

// CenterHoveringEmptyWorld: no world floor at all. Center trace
// has Fraction = 1.0 (clean drop). Return false.
func TestCheckBottom_CenterHoveringEmptyWorld(t *testing.T) {
	in := CheckBottomIn{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
	}
	ok, err := CheckBottom(in, makeEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("expected CheckBottom = false in an empty world")
	}
}

// CornerOverCliff: half the entity straddles a cliff edge. The
// center trace hits a floor; one corner trace falls past the
// cliff. Return false.
func TestCheckBottom_CornerOverCliff(t *testing.T) {
	// makeWorldWithStep has floor for x < 0 only. Position the entity
	// straddling the x=0 line: center at x=0, mins x = -8, maxs x = 8.
	// Center trace: x=0 trace point. (+0, _, _) is in the empty half
	// (no floor) -> center trace falls past 2*StepSize -> Fraction=1
	// -> CenterHovering branch -> return false.
	//
	// To test the CORNER cliff branch, we need the CENTER trace to
	// hit a floor while at least one CORNER trace falls. Position
	// the entity off-center so the center is over the floor and ONE
	// corner is over the cliff.
	in := CheckBottomIn{
		Origin: [3]float32{-4, 0, 1}, // center at x=-4 (over floor)
		Mins:   [3]float32{-2, -8, 0},
		Maxs:   [3]float32{10, 8, 24},
		// Center XY: ((-4+-2)+(-4+10))/2 = 0. So center XY x = 0.
		// Hmm, center is at the x=0 boundary. The trace down at x=0
		// may hit the floor (the boundary case). Let me re-think.
	}
	// Adjust: center XY should be over the floor. Put origin further -X.
	in.Origin = [3]float32{-6, 0, 1}
	in.Mins = [3]float32{-2, -8, 0}
	in.Maxs = [3]float32{8, 8, 24}
	// Center XY: ((-6-2)+(-6+8))/2 = (-8+2)/2 = -3 (over floor, x<0).
	// Corner (+,_): x = -6+8 = 2 (cliff, x>0).
	// Corner (-,_): x = -6-2 = -8 (over floor).
	ok, err := CheckBottom(in, makeWorldWithStep(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("expected CheckBottom = false when a corner hangs over a cliff")
	}
}

// CornerTooFarBelow: all four corners hit a floor, but one corner's
// floor is more than StepSize below the center's floor. Build a
// world where the center hits floor at z=0 but one corner's floor is
// at z=-30 (more than StepSize=18 below).
func TestCheckBottom_CornerTooFarBelow(t *testing.T) {
	// Build a brushmodel with two floor levels split by x=0:
	// x < 0 -> floor at z=0; x >= 0 -> floor at z=-30.
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: split on X at 0. x<0 -> node 1 (high floor), x>=0 -> node 2 (low floor).
			{PlaneNum: 0, Children: [2]int16{2, 1}},
			// node 1: split on Z at 0. z>=0 empty, z<0 solid. (high floor at z=0)
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
			// node 2: split on Z at -30. z>=-30 empty, z<-30 solid. (low floor at z=-30)
			{PlaneNum: 2, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
			{Normal: [3]float32{0, 0, 1}, Dist: -30, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  2,
	}
	// Position the entity straddling x=0 (center over the high floor,
	// one corner over the low floor).
	in := CheckBottomIn{
		Origin: [3]float32{-4, 0, 1}, // center XY x = ((-4-2)+(-4+8))/2 = -1 (high floor)
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{8, 2, 24},
		// Corner (+,+): x = -4+8 = 4 (low floor at z=-30).
		// mid (center floor) ≈ 0, corner_z ≈ -30 -> diff = 30 > StepSize(18).
	}
	ok, err := CheckBottom(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Error("expected CheckBottom = false when a corner is more than StepSize below center")
	}
}

// CornerWithinStepSize: similar to CornerTooFarBelow but the low
// floor is only 10 units below (within StepSize=18). All four
// corners are supported.
func TestCheckBottom_CornerWithinStepSize(t *testing.T) {
	// Two floors: high at z=0 (x<0), low at z=-10 (x>=0).
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{2, 1}},
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
			{PlaneNum: 2, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
			{Normal: [3]float32{0, 0, 1}, Dist: -10, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  2,
	}
	in := CheckBottomIn{
		Origin: [3]float32{-4, 0, 1},
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{8, 2, 24},
	}
	ok, err := CheckBottom(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Error("expected CheckBottom = true when corner is within StepSize")
	}
}

// CenterTraceErrorPropagates: a corrupt world hull surfaces the
// trace error out of CheckBottom.
func TestCheckBottom_CenterTraceErrorPropagates(t *testing.T) {
	in := CheckBottomIn{
		Origin: [3]float32{0, 0, 1},
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
	}
	ok, err := CheckBottom(in, checkBottomCorruptWorld(), nil)
	if err == nil {
		t.Error("expected error from corrupt hull")
	}
	if ok {
		t.Error("on error, returned bool must be false")
	}
}

// CandidateErrorOnCenterTrace: a SOLID_BSP candidate with no
// BrushModel surfaces the bsptrace error out of the FIRST TraceMove
// call (the center trace). The corner-trace error branch is
// structurally unreachable in CheckBottom's call shape -- the same
// (worldmodel + candidates) is passed to every TraceMove call, so
// any error fires on the center trace and bails out. See the
// comment block in checkbottom.go for the proof.
func TestCheckBottom_CandidateErrorOnCenterTrace(t *testing.T) {
	corrupt := Target{
		Origin: [3]float32{0, 0, 0},
		Solid:  server.SolidBSP, // no BrushModel -> ErrSolidBSPNeedsBrushModel
	}
	in := CheckBottomIn{
		Origin: [3]float32{0, 0, 1},
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
	}
	ok, err := CheckBottom(in, makeWorldWithFloor(), []Target{corrupt})
	if err == nil {
		t.Error("expected candidate-error to propagate from the center trace")
	}
	if ok {
		t.Error("on error, returned bool must be false")
	}
}

// EntityKeyIgnored: like the PushEntity/FlyMove versions, EntityKey
// is carried through but not consumed by CheckBottom.
func TestCheckBottom_EntityKeyIgnored(t *testing.T) {
	mk := func(k Key) CheckBottomIn {
		return CheckBottomIn{
			Origin:    [3]float32{0, 0, 1},
			Mins:      [3]float32{-8, -8, 0},
			Maxs:      [3]float32{8, 8, 24},
			EntityKey: k,
		}
	}
	w := makeWorldWithFloor()
	a, errA := CheckBottom(mk(7), w, nil)
	b, errB := CheckBottom(mk(42), w, nil)
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errs: %v %v", errA, errB)
	}
	if a != b {
		t.Errorf("EntityKey leak: %v vs %v", a, b)
	}
}

// StepSizeConstantDriftDetector: pin the C upstream's literal so a
// sloppy refactor doesn't quietly change behaviour.
func TestCheckBottom_StepSizeDrift(t *testing.T) {
	if StepSize != 18.0 {
		t.Errorf("StepSize drift: got %v want 18.0", float32(StepSize))
	}
}

// checkBottomCorruptWorld returns a brushmodel whose hull 0 references
// an out-of-range plane index -- [TraceMove] returns ErrBadPlaneIndex
// which CheckBottom is expected to propagate.
func checkBottomCorruptWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	corrupt := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm.Hulls[0] = corrupt
	bm.Hulls[1] = corrupt
	bm.Hulls[2] = corrupt
	return bm
}
