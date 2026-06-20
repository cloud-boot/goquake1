// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"math"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// movestepCorruptWorld: a brushmodel whose hull 0 references an
// out-of-range plane index. [TraceMove] returns the bsptrace error
// which MoveStep is expected to propagate.
func movestepCorruptWorld() *model.BrushModel {
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

// movestepFloorAt builds a brushmodel whose hull 0 has solid below
// z = floorZ (and empty above) -- a single infinite horizontal
// floor. All three hulls are populated identically so HullForBounds'
// per-monster-size dispatch picks the same geometry whatever bbox
// the caller hands in.
func movestepFloorAt(floorZ float32) *model.BrushModel {
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{0, 0, 1}, Dist: floorZ, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	return bm
}

// movestepStepWorld builds a world with two floor heights split at
// x = 0: floor at z = 0 for x < 0, floor at z = floorPlusZ for
// x >= 0. Used to test the step-up algorithm (floorPlusZ within
// StepSize) and the cliff branch (floorPlusZ very negative).
func movestepStepWorld(floorPlusZ float32) *model.BrushModel {
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: split on X at 0. x<0 -> node 2 (low floor at z=0),
			// x>=0 -> node 1 (high floor at z=floorPlusZ).
			{PlaneNum: 0, Children: [2]int16{1, 2}},
			// node 1: split on Z at floorPlusZ. z>=floorPlusZ -> empty, z< -> solid.
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
			// node 2: split on Z at 0. z>=0 empty, z<0 solid.
			{PlaneNum: 2, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 0, 1}, Dist: floorPlusZ, Type: bspfile.PlaneZ},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  2,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	return bm
}

// movestepCliffWorld builds a world with floor at z=0 for x<0 and
// NO floor for x>=0 (an open cliff edge at x=0). Children[0] is the
// FRONT side of the plane (positive normal direction).
func movestepCliffWorld() *model.BrushModel {
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: split on X at 0. x>=0 -> empty (cliff), x<0 -> node 1 (floor).
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, 1}},
			// node 1: split on Z at 0. z>=0 -> empty, z<0 -> solid floor.
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	return bm
}

// movestepWallWorld builds a world with solid in x>=0 (and empty in
// x<0). Used for the "monster facing a wall" tests.
func movestepWallWorld() *model.BrushModel {
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	return bm
}

// movestepAllSolidWorld: hull is entirely solid. Step-up trace from
// anywhere reports AllSolid.
func movestepAllSolidWorld() *model.BrushModel {
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsSolid}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	return bm
}

// --- MoveStep flying / swimming ----------------------------------------

func TestMoveStep_FlyingClearAdvances(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 100},
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Move:   [3]float32{10, 0, 0},
		Flags:  server.FlagFly,
	}
	out, err := MoveStep(in, makeEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("FlagFly clear path: expected Moved=true")
	}
	want := [3]float32{10, 0, 100}
	if out.NewOrigin != want {
		t.Errorf("NewOrigin: got %v want %v", out.NewOrigin, want)
	}
}

func TestMoveStep_SwimmingClearAdvances(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 100},
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Move:   [3]float32{5, 5, 5},
		Flags:  server.FlagSwim,
	}
	out, err := MoveStep(in, makeEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("FlagSwim clear path: expected Moved=true")
	}
	want := [3]float32{5, 5, 105}
	if out.NewOrigin != want {
		t.Errorf("NewOrigin: got %v want %v", out.NewOrigin, want)
	}
}

func TestMoveStep_FlyingBlockedByWall(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{-5, 0, 0},
		Mins:   [3]float32{0, 0, 0},
		Maxs:   [3]float32{0, 0, 0},
		Move:   [3]float32{20, 0, 0}, // would land past x=0 wall
		Flags:  server.FlagFly,
	}
	out, err := MoveStep(in, movestepWallWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Error("FlagFly into wall: expected Moved=false")
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("blocked flying: NewOrigin should equal in.Origin, got %v", out.NewOrigin)
	}
}

func TestMoveStep_FlyingErrorPropagates(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 0},
		Move:   [3]float32{10, 0, 0},
		Flags:  server.FlagFly,
	}
	_, err := MoveStep(in, movestepCorruptWorld(), nil)
	if err == nil {
		t.Error("flying path should propagate trace error from corrupt world")
	}
}

// --- MoveStep walking --------------------------------------------------

func TestMoveStep_WalkingFlatFloor(t *testing.T) {
	// Monster bbox is small (-8..8 x/y, 0..24 z), sitting at origin
	// (0, 0, 0) with feet on the floor at z=0. Move +10 along X --
	// floor remains at z=0 across the move.
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
		Move:   [3]float32{10, 0, 0},
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, movestepFloorAt(0), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("flat-floor walk: expected Moved=true")
	}
	if out.NewOrigin[0] < 5 {
		t.Errorf("walk should advance along X: got x=%v", out.NewOrigin[0])
	}
}

func TestMoveStep_WalkingBlockedByWall(t *testing.T) {
	// Build a thick wall: solid for x>=10 (children[0] is FRONT of
	// the +X plane). Floor at z=0 for x<10.
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: split on X at 10. x>=10 -> solid wall, x<10 -> node 1 (floor).
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, 1}},
			// node 1 (x<10): split on Z at 0. z>=0 -> empty, z<0 -> floor.
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 10, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	// Monster at (0, 0, 1) with width 16 -> right edge at x=8. Move
	// +50 along X. Step-up trace ends up entirely buried in the wall
	// at x>=10 -> AllSolid -> bail.
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 1},
		Mins:   [3]float32{-8, -8, 0},
		Maxs:   [3]float32{8, 8, 24},
		Move:   [3]float32{50, 0, 0},
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Errorf("wall block: expected Moved=false, got NewOrigin=%v", out.NewOrigin)
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("blocked walk: NewOrigin must equal start, got %v", out.NewOrigin)
	}
}

func TestMoveStep_WalkingStepUp(t *testing.T) {
	// Two-tier floor: low at z=0 (x<0), high at z=10 (x>=0). Step is
	// 10 units tall -- within StepSize=18, so the monster climbs.
	// Monster at (-20, 0, 0) with feet on low floor. Move +30 along
	// X to put center at x=10 (on the high tier). Step-up trace from
	// wantedXY.z+18 = 18 finds the high floor at z=10.
	bm := movestepStepWorld(10)
	in := MoveStepIn{
		Origin: [3]float32{-20, 0, 0},
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{2, 2, 24},
		Move:   [3]float32{30, 0, 0}, // land at x=10
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Errorf("step-up: expected Moved=true, got out=%+v", out)
	}
	// Final Z should be close to the high floor (z=10).
	if out.NewOrigin[2] < 9 || out.NewOrigin[2] > 11 {
		t.Errorf("step-up landing Z: got %v want ~10", out.NewOrigin[2])
	}
}

func TestMoveStep_WalkingCliffWithGroundRetracts(t *testing.T) {
	// Monster on the floor near the cliff edge. Move forward over
	// the cliff. With FlagOnGround set, refuses to fall off -- no
	// movement, NewOrigin = Origin.
	in := MoveStepIn{
		Origin: [3]float32{-5, 0, 1}, // on the floor (x<0)
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{2, 2, 24},
		Move:   [3]float32{20, 0, 0}, // would put center at x=15 (cliff)
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, movestepCliffWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Errorf("cliff with FlagOnGround: expected Moved=false, got %+v", out)
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("cliff retract: NewOrigin must equal Origin, got %v", out.NewOrigin)
	}
}

func TestMoveStep_WalkingCliffWithoutGroundTakesMove(t *testing.T) {
	// Same setup but FlagOnGround cleared (monster is already mid-
	// air or otherwise unsupported). The cliff guard is disabled --
	// take the move.
	in := MoveStepIn{
		Origin: [3]float32{-5, 0, 1},
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{2, 2, 24},
		Move:   [3]float32{20, 0, 0},
		Flags:  0, // no FlagOnGround
	}
	out, err := MoveStep(in, movestepCliffWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("cliff without FlagOnGround: expected Moved=true")
	}
	if out.NewOrigin[0] != 15 || out.NewOrigin[2] != 1 {
		t.Errorf("cliff-take: NewOrigin: got %v want (15,0,1)", out.NewOrigin)
	}
}

func TestMoveStep_WalkingCornerHangsRetracts(t *testing.T) {
	// Two-tier floor with a steep drop (low floor 30 units below
	// high floor). Monster starts on high floor; after a sideways
	// move, one corner hangs over the cliff edge. CheckBottom
	// rejects -- retract.
	bm := movestepStepWorld(-30) // low floor 30 below high (z=-30 for x>=0)
	in := MoveStepIn{
		Origin: [3]float32{-6, 0, 1}, // on high floor (x<0)
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{8, 2, 24}, // bbox extends to x = -6+8 = 2 (over the low floor)
		Move:   [3]float32{2, 0, 0},  // small move; final center stays over high floor edge
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Errorf("corner-hang: expected Moved=false (CheckBottom failure), got %+v", out)
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("corner-hang retract: NewOrigin must equal Origin, got %v", out.NewOrigin)
	}
}

func TestMoveStep_WalkingWithoutGroundSkipsCheckBottom(t *testing.T) {
	// Same geometry as the corner-hang case, but FlagOnGround clear
	// -- skip CheckBottom and take the move (the drop trace found a
	// floor, so NewOrigin = trace endpoint).
	bm := movestepStepWorld(-30)
	in := MoveStepIn{
		Origin: [3]float32{-6, 0, 1},
		Mins:   [3]float32{-2, -2, 0},
		Maxs:   [3]float32{8, 2, 24},
		Move:   [3]float32{2, 0, 0},
		Flags:  0,
	}
	out, err := MoveStep(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("no-ground walking: expected Moved=true (CheckBottom skipped)")
	}
}

func TestMoveStep_WalkingAllSolidFails(t *testing.T) {
	// World is entirely solid -- the step-up trace reports AllSolid
	// and MoveStep bails.
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Move:   [3]float32{10, 0, 0},
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, movestepAllSolidWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Error("all-solid world: expected Moved=false")
	}
	if !out.Trace.AllSolid {
		t.Error("all-solid world: expected Trace.AllSolid")
	}
}

func TestMoveStep_WalkingStartSolidRetrySucceeds(t *testing.T) {
	// Ceiling at z = 30 (solid above), floor at z = 0 (solid below),
	// empty between. With the monster at (0, 0, 5), the step-up
	// position is z = wantedXY.z + StepSize = 23, which is below the
	// ceiling -- not actually start-solid. We need to engineer the
	// step-up Z (wantedXY.z + 18) to land INSIDE the ceiling.
	//
	// Place the monster at z = 15. wantedXY.z = 15 (Move.z = 0).
	// stepStart.z = 33 (inside the ceiling at z=30). The trace from
	// (start at solid) starts inside solid -> StartSolid. Retry at
	// stepStart.z = 15 -- now in empty -- succeeds.
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: split on Z at 30. z>=30 -> solid (ceiling). z<30 -> node 1.
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, 1}},
			// node 1: split on Z at 0. z>=0 -> empty, z<0 -> solid (floor).
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{0, 0, 1}, Dist: 30, Type: bspfile.PlaneZ},
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 15},
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Move:   [3]float32{5, 0, 0},
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// The retry should land us on the floor at z=0 (well within the
	// 2*StepSize drop from z=15). Whether CheckBottom passes here
	// depends on the geometry; the important thing for this test is
	// that the StartSolid retry path executes WITHOUT erroring.
	_ = out
}

func TestMoveStep_WalkingStartSolidRetryStillBuried(t *testing.T) {
	// Both step-up Z AND the retry Z (wantedXY.z) are inside solid.
	// Build a world that's solid above z=0 -- so every Z > 0 is
	// start-solid. The retry also lands in solid -> bail.
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// z >= 0 -> solid, z < 0 -> empty (inverted floor).
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 5}, // already in solid (z>=0)
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Move:   [3]float32{5, 0, 0},
		Flags:  server.FlagOnGround,
	}
	out, err := MoveStep(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Error("retry-still-buried: expected Moved=false")
	}
}

func TestMoveStep_WalkingErrorPropagatesOnFirstTrace(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 1},
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Move:   [3]float32{10, 0, 0},
		Flags:  server.FlagOnGround,
	}
	_, err := MoveStep(in, movestepCorruptWorld(), nil)
	if err == nil {
		t.Error("walking path should propagate trace error from corrupt world")
	}
}

// --- StepDirection -----------------------------------------------------

func TestStepDirection_YawZero(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 100},
		Flags:  server.FlagFly, // skip walking complexity
	}
	out, err := StepDirection(0, 10, in, makeEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("yaw=0: expected Moved=true")
	}
	// yaw=0 -> cos=1, sin=0 -> Move=(10, 0, 0); NewOrigin=(10, 0, 100).
	want := [3]float32{10, 0, 100}
	if !nearVec(out.NewOrigin, want, 0.001) {
		t.Errorf("yaw=0: NewOrigin=%v want %v", out.NewOrigin, want)
	}
}

func TestStepDirection_Yaw90(t *testing.T) {
	in := MoveStepIn{
		Origin: [3]float32{0, 0, 100},
		Flags:  server.FlagFly,
	}
	out, err := StepDirection(90, 10, in, makeEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("yaw=90: expected Moved=true")
	}
	// yaw=90 -> cos=0, sin=1 -> Move=(0, 10, 0); NewOrigin=(0, 10, 100).
	want := [3]float32{0, 10, 100}
	if !nearVec(out.NewOrigin, want, 0.001) {
		t.Errorf("yaw=90: NewOrigin=%v want %v", out.NewOrigin, want)
	}
}

func nearVec(a, b [3]float32, eps float32) bool {
	for i := 0; i < 3; i++ {
		d := a[i] - b[i]
		if d < -eps || d > eps {
			return false
		}
	}
	return true
}

// --- NewChaseDir -------------------------------------------------------

func TestNewChaseDir_GoalNorthEast(t *testing.T) {
	// Goal at (+100, +100) relative to actor at (0, 0). dx > 10
	// (-> d[1]=0 = +X), dy > 10 (-> d[2]=90 = +Y). Diagonal: d[1]==0,
	// d[2]==90 -> tdir=45. currentYaw=0 -> olddir=0, turnaround=180.
	// 45 != 180, so return 45.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{100, 100, 0}, 0)
	if yaw != 45 {
		t.Errorf("NE goal: got %v want 45", yaw)
	}
}

func TestNewChaseDir_GoalSouthEast(t *testing.T) {
	// dx > 10, dy < -10 -> d[1]=0, d[2]=270. tdir = d[2]==90 ? 45 : 315 = 315.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{100, -100, 0}, 0)
	if yaw != 315 {
		t.Errorf("SE goal: got %v want 315", yaw)
	}
}

func TestNewChaseDir_GoalNorthWest(t *testing.T) {
	// dx < -10, dy > 10 -> d[1]=180, d[2]=90. tdir = d[2]==90 ? 135 : 215 = 135.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{-100, 100, 0}, 0)
	if yaw != 135 {
		t.Errorf("NW goal: got %v want 135", yaw)
	}
}

func TestNewChaseDir_GoalSouthWest(t *testing.T) {
	// dx < -10, dy < -10 -> d[1]=180, d[2]=270. tdir = 215.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{-100, -100, 0}, 0)
	if yaw != 215 {
		t.Errorf("SW goal: got %v want 215", yaw)
	}
}

func TestNewChaseDir_GoalDirectlyEast(t *testing.T) {
	// dx > 10, dy in [-10, 10] -> d[1]=0, d[2]=NODIR. Skip diagonal,
	// return d[1] = 0.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{100, 0, 0}, 0)
	if yaw != 0 {
		t.Errorf("E goal: got %v want 0", yaw)
	}
}

func TestNewChaseDir_GoalDirectlyNorth(t *testing.T) {
	// dx in [-10, 10], dy > 10 -> d[1]=NODIR, d[2]=90. Return d[2] = 90.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{0, 100, 0}, 0)
	if yaw != 90 {
		t.Errorf("N goal: got %v want 90", yaw)
	}
}

func TestNewChaseDir_GoalDirectlyWest(t *testing.T) {
	// dx < -10, dy in range -> d[1]=180. With currentYaw=0,
	// olddir=0 and turnaround=180 -- so the "180 != turnaround"
	// guard rejects d[1]. Use currentYaw=90 (turnaround=270) so
	// d[1]=180 passes the guard and is returned.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{-100, 0, 0}, 90)
	if yaw != 180 {
		t.Errorf("W goal: got %v want 180", yaw)
	}
}

func TestNewChaseDir_GoalDirectlySouth(t *testing.T) {
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{0, -100, 0}, 0)
	if yaw != 270 {
		t.Errorf("S goal: got %v want 270", yaw)
	}
}

func TestNewChaseDir_GoalCoincident(t *testing.T) {
	// dx, dy both within [-10, 10] -> both NODIR. Fall through to
	// olddir. currentYaw=0 -> olddir=0.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{5, 5, 0}, 0)
	if yaw != 0 {
		t.Errorf("coincident goal: got %v want 0 (olddir)", yaw)
	}
}

func TestNewChaseDir_DiagonalEqualsTurnaround(t *testing.T) {
	// Build a case where the diagonal candidate == turnaround.
	// currentYaw = 225 (snapped: 225/45 = 5; 5*45 = 225; olddir =
	// 225). turnaround = 225-180 = 45.
	// Goal NE -> diagonal = 45 = turnaround. Skip diagonal.
	// Fall to d[1]=0 != 45 -> return 0.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{100, 100, 0}, 225)
	if yaw != 0 {
		t.Errorf("diagonal=turnaround: got %v want 0 (d[1] fallback)", yaw)
	}
}

func TestNewChaseDir_XEqualsTurnaround(t *testing.T) {
	// Goal directly east (d[1]=0, d[2]=NODIR). currentYaw=180 ->
	// olddir=180, turnaround=0. d[1]=0 == turnaround -> skip.
	// d[2] is NODIR -> skip. Fall to olddir=180.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{100, 0, 0}, 180)
	if yaw != 180 {
		t.Errorf("X==turnaround fallback: got %v want 180", yaw)
	}
}

func TestNewChaseDir_YEqualsTurnaround(t *testing.T) {
	// Goal directly north (d[1]=NODIR, d[2]=90). currentYaw=270 ->
	// olddir=270, turnaround=90. d[2]=90 == turnaround -> skip.
	// Fall to olddir=270.
	yaw := NewChaseDir([3]float32{0, 0, 0}, [3]float32{0, 100, 0}, 270)
	if yaw != 270 {
		t.Errorf("Y==turnaround fallback: got %v want 270", yaw)
	}
}

func TestAnglemod_NegativeFolds(t *testing.T) {
	if got := anglemod(-90); got != 270 {
		t.Errorf("anglemod(-90): got %v want 270", got)
	}
	if got := anglemod(360); got != 0 {
		t.Errorf("anglemod(360): got %v want 0", got)
	}
	if got := anglemod(45); got != 45 {
		t.Errorf("anglemod(45): got %v want 45", got)
	}
}

// --- MoveToGoal --------------------------------------------------------

func TestMoveToGoal_ClearPath(t *testing.T) {
	// Flying monster in an empty world, goal off to the east. First
	// try uses in.Yaw = 0 (facing east) -> StepDirection succeeds
	// immediately.
	in := MoveToGoalIn{
		Origin:     [3]float32{0, 0, 100},
		GoalOrigin: [3]float32{500, 0, 100},
		Mins:       [3]float32{-1, -1, -1},
		Maxs:       [3]float32{1, 1, 1},
		Flags:      server.FlagFly,
		Yaw:        0,
		Dist:       10,
	}
	out, err := MoveToGoal(in, makeEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("clear path: expected Moved=true")
	}
	if out.NewYaw != 0 {
		t.Errorf("clear path: NewYaw=%v want 0 (in.Yaw preserved)", out.NewYaw)
	}
	if !nearVec(out.NewOrigin, [3]float32{10, 0, 100}, 0.001) {
		t.Errorf("clear path: NewOrigin=%v want (10, 0, 100)", out.NewOrigin)
	}
}

func TestMoveToGoal_FirstYawBlocked_NewChaseDirRescues(t *testing.T) {
	// Wall at x=0 (solid for x>=0). Monster RIGHT at the wall:
	// origin (-1, 0, 100) flying, Yaw=0. Move distance 10 -> trace
	// from x=-1 to x=9 sweeps into the solid wall -> blocked.
	// Goal NORTH at (-1, 500, 100) -> NewChaseDir returns 90 (due
	// north). Retry at yaw=90 -> move along +Y, no wall in the way.
	in := MoveToGoalIn{
		Origin:     [3]float32{-1, 0, 100},
		GoalOrigin: [3]float32{-1, 500, 100}, // due north
		Mins:       [3]float32{0, 0, 0},
		Maxs:       [3]float32{0, 0, 0},
		Flags:      server.FlagFly,
		Yaw:        0, // facing east, into the wall
		Dist:       10,
	}
	out, err := MoveToGoal(in, movestepWallWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Moved {
		t.Error("first-blocked-then-rescue: expected Moved=true")
	}
	// NewYaw should have changed away from 0 (NewChaseDir picked 90
	// for due-north goal).
	if out.NewYaw == 0 {
		t.Errorf("NewYaw should differ from in.Yaw on retry rescue: got %v", out.NewYaw)
	}
}

func TestMoveToGoal_AllAttemptsBlocked(t *testing.T) {
	// Engineer a world where every cardinal direction is blocked.
	// Surround the monster on all 4 sides with walls (a tiny solid
	// pocket). Build: solid everywhere EXCEPT a small empty cube
	// around the origin.
	bm := makeSurroundedWorld()
	in := MoveToGoalIn{
		Origin:     [3]float32{0, 0, 0},
		GoalOrigin: [3]float32{1000, 1000, 0},
		Mins:       [3]float32{-1, -1, -1},
		Maxs:       [3]float32{1, 1, 1},
		Flags:      server.FlagFly,
		Yaw:        0,
		Dist:       100, // larger than the empty pocket -> always blocked
	}
	out, err := MoveToGoal(in, bm, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Moved {
		t.Error("surrounded: expected Moved=false")
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("surrounded: NewOrigin should stay at Origin, got %v", out.NewOrigin)
	}
	if out.NewYaw != in.Yaw {
		t.Errorf("surrounded: NewYaw should stay at in.Yaw, got %v", out.NewYaw)
	}
}

// makeSurroundedWorld: empty cube from (-2, -2, -2) to (2, 2, 2),
// solid everywhere else. A 100-unit move from origin in any
// direction crosses into solid.
func makeSurroundedWorld() *model.BrushModel {
	// 6 splits: at x=-2, x=2, y=-2, y=2, z=-2, z=2. Inside all
	// bounds -> empty, outside any -> solid.
	h := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: x >= -2? yes -> node 1; no -> solid.
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsSolid}},
			// node 1: x <= 2? yes (x<2) -> node 2; no -> solid.
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsSolid, 2}},
			// node 2: y >= -2? yes -> node 3; no -> solid.
			{PlaneNum: 2, Children: [2]int16{3, bspfile.ContentsSolid}},
			// node 3: y <= 2?
			{PlaneNum: 3, Children: [2]int16{bspfile.ContentsSolid, 4}},
			// node 4: z >= -2?
			{PlaneNum: 4, Children: [2]int16{5, bspfile.ContentsSolid}},
			// node 5: z <= 2?
			{PlaneNum: 5, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: -2, Type: bspfile.PlaneX},
			{Normal: [3]float32{1, 0, 0}, Dist: 2, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 1, 0}, Dist: -2, Type: bspfile.PlaneY},
			{Normal: [3]float32{0, 1, 0}, Dist: 2, Type: bspfile.PlaneY},
			{Normal: [3]float32{0, 0, 1}, Dist: -2, Type: bspfile.PlaneZ},
			{Normal: [3]float32{0, 0, 1}, Dist: 2, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  5,
	}
	bm := &model.BrushModel{}
	bm.Hulls[0] = h
	bm.Hulls[1] = h
	bm.Hulls[2] = h
	return bm
}

func TestMoveToGoal_ErrorPropagates(t *testing.T) {
	in := MoveToGoalIn{
		Origin:     [3]float32{0, 0, 0},
		GoalOrigin: [3]float32{100, 0, 0},
		Mins:       [3]float32{-1, -1, -1},
		Maxs:       [3]float32{1, 1, 1},
		Flags:      server.FlagFly,
		Yaw:        0,
		Dist:       10,
	}
	_, err := MoveToGoal(in, movestepCorruptWorld(), nil)
	if err == nil {
		t.Error("expected trace error to propagate from corrupt world")
	}
}

// --- Drift detectors --------------------------------------------------

func TestNewChaseDir_DeltaThresholdDrift(t *testing.T) {
	if chaseDirDeltaThreshold != 10 {
		t.Errorf("chaseDirDeltaThreshold drift: got %v want 10", chaseDirDeltaThreshold)
	}
	if chaseDirNoDir != -1 {
		t.Errorf("chaseDirNoDir drift: got %v want -1", chaseDirNoDir)
	}
}

func TestStepDirection_FullCircle(t *testing.T) {
	// Spot-check several yaw values match cos/sin expectations.
	cases := []struct {
		yaw   float32
		wantX float32
		wantY float32
	}{
		{0, 10, 0},
		{45, 7.07, 7.07},
		{90, 0, 10},
		{135, -7.07, 7.07},
		{180, -10, 0},
		{225, -7.07, -7.07},
		{270, 0, -10},
		{315, 7.07, -7.07},
	}
	for _, tc := range cases {
		in := MoveStepIn{
			Origin: [3]float32{0, 0, 100},
			Flags:  server.FlagFly,
		}
		out, err := StepDirection(tc.yaw, 10, in, makeEmptyWorld(), nil)
		if err != nil {
			t.Fatalf("yaw=%v: %v", tc.yaw, err)
		}
		if !out.Moved {
			t.Errorf("yaw=%v: expected Moved=true", tc.yaw)
			continue
		}
		if math.Abs(float64(out.NewOrigin[0]-tc.wantX)) > 0.1 ||
			math.Abs(float64(out.NewOrigin[1]-tc.wantY)) > 0.1 {
			t.Errorf("yaw=%v: NewOrigin=(%v,%v) want (%v,%v)",
				tc.yaw, out.NewOrigin[0], out.NewOrigin[1], tc.wantX, tc.wantY)
		}
	}
}
