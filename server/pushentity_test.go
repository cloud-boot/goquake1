// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/world"
)

// emptyWorld returns a brushmodel whose hull 0 is "all empty" so a
// trace through it never clips. Mirrors world.makeEmptyWorld (which
// is unexported there).
func emptyWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// worldWithWall returns a brushmodel whose hull 0 splits on PlaneX:
// children[0] = solid (+X), children[1] = empty (-X). Traces from
// x<0 toward x>0 impact at x=0.
func worldWithWall() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// corruptWorld returns a brushmodel whose hull 0 references an
// out-of-range plane index -- bsptrace.TraceHull returns
// ErrBadPlaneIndex which PushEntity is expected to propagate.
func corruptWorld() *model.BrushModel {
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

// SolidNot short-circuits: NewOrigin = Origin+Push, Fraction=1, no
// hit, no world clip, and the trace's EndPos equals the new origin.
func TestPushEntity_SolidNotShortCircuit(t *testing.T) {
	in := PushEntityIn{
		Origin:   [3]float32{1, 2, 3},
		Push:     [3]float32{4, 5, 6},
		MoveType: MoveTypeWalk,
		Solid:    SolidNot,
	}
	out, err := PushEntity(in, worldWithWall(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := [3]float32{5, 7, 9}
	if out.NewOrigin != want {
		t.Errorf("NewOrigin: got %v want %v", out.NewOrigin, want)
	}
	if out.Trace.Fraction != 1.0 {
		t.Errorf("Trace.Fraction: got %v want 1", out.Trace.Fraction)
	}
	if out.Trace.AllSolid {
		t.Error("Trace.AllSolid should be false on short-circuit")
	}
	if out.Trace.EndPos != want {
		t.Errorf("Trace.EndPos: got %v want %v", out.Trace.EndPos, want)
	}
	if out.HitEntity != -1 {
		t.Errorf("HitEntity: got %d want -1", out.HitEntity)
	}
	if out.HitWorld {
		t.Error("HitWorld should be false on short-circuit")
	}
}

// MoveTypeNoClip short-circuits even when Solid is not SolidNot.
// Same expectations as the SolidNot path.
func TestPushEntity_NoClipShortCircuit(t *testing.T) {
	in := PushEntityIn{
		Origin:   [3]float32{0, 0, 0},
		Push:     [3]float32{10, 0, 0},
		MoveType: MoveTypeNoClip,
		Solid:    SolidBBox, // still SOLID but NoClip wins
	}
	out, err := PushEntity(in, worldWithWall(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := [3]float32{10, 0, 0}
	if out.NewOrigin != want {
		t.Errorf("NewOrigin: got %v want %v", out.NewOrigin, want)
	}
	if out.Trace.Fraction != 1.0 {
		t.Errorf("Trace.Fraction: got %v want 1", out.Trace.Fraction)
	}
	if out.HitEntity != -1 {
		t.Errorf("HitEntity: got %d want -1", out.HitEntity)
	}
	if out.HitWorld {
		t.Error("HitWorld should be false on NoClip")
	}
}

// Normal solid entity through an empty world with no candidates:
// trace completes (Fraction=1), NewOrigin advances the full push
// vector, and HitEntity/HitWorld stay clean.
func TestPushEntity_CleanThroughEmptyWorld(t *testing.T) {
	in := PushEntityIn{
		Origin:   [3]float32{-10, 0, 0},
		Mins:     [3]float32{-1, -1, -1},
		Maxs:     [3]float32{1, 1, 1},
		Push:     [3]float32{5, 0, 0},
		MoveType: MoveTypeWalk,
		Solid:    SolidBBox,
	}
	out, err := PushEntity(in, emptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Trace.Fraction != 1.0 {
		t.Errorf("Fraction: got %v want 1", out.Trace.Fraction)
	}
	want := [3]float32{-5, 0, 0}
	if out.NewOrigin != want {
		t.Errorf("NewOrigin: got %v want %v", out.NewOrigin, want)
	}
	if out.HitEntity != -1 {
		t.Errorf("HitEntity: got %d want -1", out.HitEntity)
	}
	if out.HitWorld {
		t.Error("HitWorld should be false on clean trace")
	}
}

// Trace hits the world wall: Fraction < 1, NewOrigin pulled back to
// the impact, HitWorld=true, HitEntity=-1.
func TestPushEntity_HitsWorldWall(t *testing.T) {
	in := PushEntityIn{
		Origin:   [3]float32{-10, 0, 0},
		Mins:     [3]float32{0, 0, 0},
		Maxs:     [3]float32{0, 0, 0},
		Push:     [3]float32{20, 0, 0}, // would land at x=10, past the wall at x=0
		MoveType: MoveTypeWalk,
		Solid:    SolidBBox,
	}
	out, err := PushEntity(in, worldWithWall(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Trace.Fraction >= 1.0 {
		t.Errorf("Fraction should be <1 (hit wall): got %v", out.Trace.Fraction)
	}
	if !out.HitWorld {
		t.Error("HitWorld should be true")
	}
	if out.HitEntity != -1 {
		t.Errorf("HitEntity: got %d want -1 (world-only)", out.HitEntity)
	}
	if out.NewOrigin[0] >= 10 {
		t.Errorf("NewOrigin pulled back: got x=%v, expected <10", out.NewOrigin[0])
	}
	if out.NewOrigin != out.Trace.EndPos {
		t.Errorf("NewOrigin must equal Trace.EndPos: %v vs %v", out.NewOrigin, out.Trace.EndPos)
	}
}

// Trace hits a candidate (a bbox in front of the trace): HitEntity
// points to the candidate's index, HitWorld false.
func TestPushEntity_HitsCandidate(t *testing.T) {
	candidate := world.Target{
		Origin: [3]float32{20, 0, 0},
		Mins:   [3]float32{-5, -5, -5},
		Maxs:   [3]float32{5, 5, 5},
		Solid:  SolidBBox,
	}
	in := PushEntityIn{
		Origin:   [3]float32{0, 0, 0},
		Mins:     [3]float32{0, 0, 0},
		Maxs:     [3]float32{0, 0, 0},
		Push:     [3]float32{100, 0, 0}, // toward the candidate
		MoveType: MoveTypeWalk,
		Solid:    SolidBBox,
	}
	out, err := PushEntity(in, emptyWorld(), []world.Target{candidate})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Trace.Fraction >= 1.0 {
		t.Errorf("Fraction should be <1 (hit candidate): got %v", out.Trace.Fraction)
	}
	if out.HitEntity != 0 {
		t.Errorf("HitEntity: got %d want 0", out.HitEntity)
	}
	if out.HitWorld {
		t.Error("HitWorld should be false (candidate-only)")
	}
}

// FlyMissile movetype widens candidate bounds to MissileMonsterBounds
// (+-15) before the trace. Construct a candidate whose own bounds are
// tiny (would NOT clip a point trace passing 5 units off-axis); but
// the widened +-15 bounds DO see the trace. Compare against a Fly
// movetype (which leaves bounds alone) on the same setup.
//
// Observation rule: a Minkowski-diff boxhull trace that runs entirely
// INSIDE the candidate's widened box reports AllSolid + Fraction=1
// (start and end both classified as solid). The world.TraceMove
// running-trace rule promotes such a stack-trace to the result and
// sets EntityIdx -- which is what we use to distinguish "candidate
// saw the trace" (Fly: no, EntityIdx=-1; FlyMissile: yes, EntityIdx=0).
func TestPushEntity_FlyMissileWidensCandidateBounds(t *testing.T) {
	// Candidate at (10,0,0) with TINY bounds (+-0.5). The point trace
	// runs at y=5, which is OUTSIDE the +-0.5 box (no contact) but
	// INSIDE the +-15 widened box (contact for the whole trace).
	candidate := world.Target{
		Origin: [3]float32{10, 0, 0},
		Mins:   [3]float32{-0.5, -0.5, -0.5},
		Maxs:   [3]float32{0.5, 0.5, 0.5},
		Solid:  SolidBBox,
	}
	makeIn := func(mt MoveType) PushEntityIn {
		return PushEntityIn{
			Origin:   [3]float32{5, 5, 0}, // y=5, inside +-15 but outside +-0.5
			Mins:     [3]float32{0, 0, 0},
			Maxs:     [3]float32{0, 0, 0},
			Push:     [3]float32{1, 0, 0}, // small push, both ends inside widened box
			MoveType: mt,
			Solid:    SolidBBox,
		}
	}

	// Fly: tiny candidate is too narrow at y=5, trace doesn't see it.
	flyOut, err := PushEntity(makeIn(MoveTypeFly), emptyWorld(), []world.Target{candidate})
	if err != nil {
		t.Fatalf("Fly: %v", err)
	}
	if flyOut.HitEntity != -1 {
		t.Errorf("Fly: tiny candidate should miss, HitEntity=%d want -1", flyOut.HitEntity)
	}
	if flyOut.Trace.AllSolid {
		t.Error("Fly: trace should not be AllSolid (clean pass)")
	}

	// FlyMissile: candidate bounds widen to +-15; the trace runs
	// entirely INSIDE that widened box -> StartSolid + AllSolid +
	// EntityIdx = 0 (promoted by world.TraceMove's running rule).
	missileOut, err := PushEntity(makeIn(MoveTypeFlyMissile), emptyWorld(), []world.Target{candidate})
	if err != nil {
		t.Fatalf("FlyMissile: %v", err)
	}
	if missileOut.HitEntity != 0 {
		t.Errorf("FlyMissile: widened candidate should clip, HitEntity=%d want 0", missileOut.HitEntity)
	}
	if !missileOut.Trace.AllSolid {
		t.Error("FlyMissile: trace inside widened box should be AllSolid")
	}
}

// FlyMissile with no candidates: the widen loop runs over an empty
// slice (so the result is the same as MoveTypeFly). Just a sanity
// pass that the widen path doesn't panic on empty input.
func TestPushEntity_FlyMissileNoCandidates(t *testing.T) {
	in := PushEntityIn{
		Origin:   [3]float32{-10, 0, 0},
		Push:     [3]float32{5, 0, 0},
		MoveType: MoveTypeFlyMissile,
		Solid:    SolidBBox,
	}
	out, err := PushEntity(in, emptyWorld(), nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Trace.Fraction != 1.0 {
		t.Errorf("clean trace expected, Fraction=%v", out.Trace.Fraction)
	}
	if out.NewOrigin != [3]float32{-5, 0, 0} {
		t.Errorf("NewOrigin: got %v want (-5,0,0)", out.NewOrigin)
	}
}

// World error propagates: corrupt world hull -> bsptrace error
// surfaces out of PushEntity.
func TestPushEntity_WorldErrorPropagates(t *testing.T) {
	in := PushEntityIn{
		Origin:   [3]float32{0, 0, 0},
		Push:     [3]float32{10, 0, 0},
		MoveType: MoveTypeWalk,
		Solid:    SolidBBox,
	}
	out, err := PushEntity(in, corruptWorld(), nil)
	if err == nil {
		t.Fatal("expected error from corrupt world hull")
	}
	// On error, PushEntity returns a zero-value PushEntityOut.
	if out.NewOrigin != ([3]float32{}) {
		t.Errorf("on error NewOrigin should be zero: got %v", out.NewOrigin)
	}
}

// EntityKey is carried through the input snapshot but not consumed
// by PushEntity (it's there for the caller's AreaQuery filtering).
// Verify two calls with different EntityKey values produce identical
// PushEntityOut for the same physical inputs.
func TestPushEntity_EntityKeyIgnored(t *testing.T) {
	mk := func(k world.Key) PushEntityIn {
		return PushEntityIn{
			Origin:    [3]float32{-5, 0, 0},
			Push:      [3]float32{10, 0, 0},
			MoveType:  MoveTypeWalk,
			Solid:     SolidBBox,
			EntityKey: k,
		}
	}
	w := emptyWorld()
	a, errA := PushEntity(mk(7), w, nil)
	b, errB := PushEntity(mk(42), w, nil)
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errs: %v %v", errA, errB)
	}
	if a != b {
		t.Errorf("EntityKey leak: %+v vs %+v", a, b)
	}
}
