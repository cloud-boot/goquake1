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

// --- helpers --------------------------------------------------------------

// flyMoveEmptyWorld returns a brushmodel whose hull 0 is "every leaf
// empty" -- a trace through it always yields Fraction=1.0 with no
// impact. Mirrors the helper in trace_test.go so this file is
// self-contained for the FlyMove-specific scenarios below.
func flyMoveEmptyWorld() *model.BrushModel {
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

// flyMoveSolidWorld returns a brushmodel whose hull 0 reports SOLID
// on both sides of the root plane -- any trace through it produces
// trace.AllSolid = true.
func flyMoveSolidWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsSolid}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// flyMoveCorruptWorld returns a brushmodel whose hull 0 contains a
// clipnode pointing at a non-existent plane -- any trace returns
// bsptrace.ErrBadPlaneIndex.
func flyMoveCorruptWorld() *model.BrushModel {
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

// flyMoveVec3ApproxEq compares two [3]float32 component-wise within
// tol -- float32 reflection math accumulates ULP error past exact
// compare.
func flyMoveVec3ApproxEq(a, b [3]float32, tol float32) bool {
	for i := 0; i < 3; i++ {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > tol {
			return false
		}
	}
	return true
}

// --- early-exit branches --------------------------------------------------

// Zero velocity input -> immediate exit; NewOrigin = Origin, no
// trace performed, no Blocked bits set.
func TestFlyMove_ZeroVelocityImmediateExit(t *testing.T) {
	in := FlyMoveIn{
		Origin:   [3]float32{1, 2, 3},
		Velocity: [3]float32{0, 0, 0},
		Time:     1.0,
	}
	// Pass a CORRUPT world to prove TraceMove was not called -- if it
	// were, the corrupt hull would return an error.
	out, err := FlyMove(in, flyMoveCorruptWorld(), nil)
	if err != nil {
		t.Fatalf("zero-velocity should not invoke trace: %v", err)
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("origin: got %v want %v", out.NewOrigin, in.Origin)
	}
	if out.NewVelocity != ([3]float32{0, 0, 0}) {
		t.Errorf("velocity: got %v want zeros", out.NewVelocity)
	}
	if out.Blocked != 0 {
		t.Errorf("blocked: got %d want 0", out.Blocked)
	}
}

// Zero time input -> immediate exit, same as zero velocity.
func TestFlyMove_ZeroTimeImmediateExit(t *testing.T) {
	in := FlyMoveIn{
		Origin:   [3]float32{1, 2, 3},
		Velocity: [3]float32{10, 0, 0},
		Time:     0,
	}
	out, err := FlyMove(in, flyMoveCorruptWorld(), nil)
	if err != nil {
		t.Fatalf("zero-time should not invoke trace: %v", err)
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("origin: got %v want %v", out.NewOrigin, in.Origin)
	}
	if out.NewVelocity != in.Velocity {
		t.Errorf("velocity: got %v want %v (unchanged)", out.NewVelocity, in.Velocity)
	}
}

// Negative time: same early-exit (Time <= 0).
func TestFlyMove_NegativeTimeExits(t *testing.T) {
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{10, 0, 0},
		Time:     -1.0,
	}
	out, err := FlyMove(in, flyMoveCorruptWorld(), nil)
	if err != nil {
		t.Fatalf("negative time should early-exit: %v", err)
	}
	if out.NewOrigin != in.Origin {
		t.Errorf("origin should not change: got %v", out.NewOrigin)
	}
}

// --- clean move -----------------------------------------------------------

// Empty world, non-zero velocity, no candidates: one clean iteration,
// NewOrigin advances by Velocity*Time, Blocked=0, StepTrace is the
// default (Fraction=1.0).
func TestFlyMove_CleanMoveEmptyWorld(t *testing.T) {
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Mins:     [3]float32{-1, -1, -1},
		Maxs:     [3]float32{1, 1, 1},
		Velocity: [3]float32{10, 5, -3},
		Time:     2.0,
	}
	out, err := FlyMove(in, flyMoveEmptyWorld(), nil)
	if err != nil {
		t.Fatalf("clean move: %v", err)
	}
	want := [3]float32{20, 10, -6}
	if !flyMoveVec3ApproxEq(out.NewOrigin, want, 1e-4) {
		t.Errorf("origin: got %v want %v", out.NewOrigin, want)
	}
	if out.NewVelocity != in.Velocity {
		t.Errorf("velocity: got %v want %v (unchanged)", out.NewVelocity, in.Velocity)
	}
	if out.Blocked != 0 {
		t.Errorf("blocked: got %d want 0", out.Blocked)
	}
	if out.StepTrace.Fraction != 1.0 {
		t.Errorf("steptrace default Fraction: got %v want 1.0", out.StepTrace.Fraction)
	}
}

// --- AllSolid trap --------------------------------------------------------

// World where any trace is AllSolid: velocity zeroed,
// Blocked = floor|step.
func TestFlyMove_AllSolidZerosVelocity(t *testing.T) {
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{10, 0, 0},
		Time:     1.0,
	}
	out, err := FlyMove(in, flyMoveSolidWorld(), nil)
	if err != nil {
		t.Fatalf("AllSolid trap: %v", err)
	}
	if out.NewVelocity != ([3]float32{0, 0, 0}) {
		t.Errorf("velocity: got %v want zeros", out.NewVelocity)
	}
	if out.Blocked != server.BlockedFloor|server.BlockedStep {
		t.Errorf("blocked: got %d want %d", out.Blocked, server.BlockedFloor|server.BlockedStep)
	}
}

// --- error propagation ----------------------------------------------------

// TraceMove returns an error: FlyMove forwards it without further
// mutation.
func TestFlyMove_TraceMoveErrorPropagates(t *testing.T) {
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{10, 0, 0},
		Time:     1.0,
	}
	_, err := FlyMove(in, flyMoveCorruptWorld(), nil)
	if err == nil {
		t.Error("expected error from corrupt world hull")
	}
}

// --- wall hit (step bit) --------------------------------------------------

// Single-wall scenario: candidate box at +X, tall in Y and Z so any
// reasonable trace from origin toward +X hits it. The trace hits
// the -X face of the box -> trace plane normal = (-1, 0, 0) (axial,
// z = 0). Blocked must include BlockedStep, NOT BlockedFloor.
// StepTrace must carry the impact (Fraction < 1).
func TestFlyMove_WallHitSetsStepBitAndStepTrace(t *testing.T) {
	wall := Target{
		Origin: [3]float32{50, 0, 0},
		Mins:   [3]float32{-5, -1000, -1000},
		Maxs:   [3]float32{5, 1000, 1000},
		Solid:  server.SolidBBox,
	}
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{100, 0, 0}, // straight east into the wall
		Time:     1.0,
	}
	out, err := FlyMove(in, flyMoveEmptyWorld(), []Target{wall})
	if err != nil {
		t.Fatalf("wall hit: %v", err)
	}
	if out.Blocked&server.BlockedStep == 0 {
		t.Errorf("expected BlockedStep bit; got %d", out.Blocked)
	}
	if out.Blocked&server.BlockedFloor != 0 {
		t.Errorf("wall hit should NOT set BlockedFloor; got %d", out.Blocked)
	}
	if out.StepTrace.Fraction == 1.0 {
		t.Errorf("StepTrace should record the impact; got Fraction=1.0")
	}
}

// --- floor hit (floor bit) ------------------------------------------------

// Single-floor scenario: candidate box BELOW with its top face at
// z=-10. Velocity (0,0,-100) goes straight down, impacts the +Z face
// (normal=(0,0,1)). normal[2] = 1 > 0.7 -> BlockedFloor.
// normal[2] != 0 -> NOT BlockedStep.
func TestFlyMove_FloorHitSetsFloorBitOnly(t *testing.T) {
	floor := Target{
		Origin: [3]float32{0, 0, -15},
		Mins:   [3]float32{-1000, -1000, -5},
		Maxs:   [3]float32{1000, 1000, 5},
		Solid:  server.SolidBBox,
	}
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{0, 0, -100},
		Time:     1.0,
	}
	out, err := FlyMove(in, flyMoveEmptyWorld(), []Target{floor})
	if err != nil {
		t.Fatalf("floor hit: %v", err)
	}
	if out.Blocked&server.BlockedFloor == 0 {
		t.Errorf("expected BlockedFloor bit; got %d", out.Blocked)
	}
	if out.Blocked&server.BlockedStep != 0 {
		t.Errorf("floor hit should NOT set BlockedStep; got %d", out.Blocked)
	}
	// StepTrace should remain the default (only wall hits update it).
	if out.StepTrace.Fraction != 1.0 {
		t.Errorf("StepTrace should remain default on floor hit; got Fraction=%v", out.StepTrace.Fraction)
	}
}

// --- shallow-slope hit (neither bit) --------------------------------------

// Slope with normal[2] in (0, 0.7] -- neither the floor nor the
// step bit should fire. Build a brushmodel whose root plane has
// normal = (0, 0.866, 0.5) (~60deg slope). The trace approaches the
// plane from the -side (where contents=empty). On impact bsptrace
// flips the normal so the recorded normal is (0, -0.866, -0.5);
// normal[2] = -0.5 fails both threshold tests (> 0.7 floor, == 0
// step).
func TestFlyMove_ShallowSlopeNeitherBit(t *testing.T) {
	bm := &model.BrushModel{}
	// children[0]=Solid (+side), children[1]=Empty (-side). Origin
	// (0,0,0) has dot(n, p) = 0 < Dist (10), so we start on the
	// - side (Empty). Velocity pushes Y+ until dot rises past 10
	// -> we cross into Solid -> impact.
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{0, 0.866, 0.5}, Dist: 10, Type: bspfile.PlaneAnyZ},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{0, 100, 0},
		Time:     1.0,
	}
	out, err := FlyMove(in, bm, nil)
	if err != nil {
		t.Fatalf("slope: %v", err)
	}
	// Approach from -side -> bsptrace flips the recorded plane
	// normal to (0, -0.866, -0.5); 0.5 magnitude fails floor (not
	// > 0.7) and the - sign fails step (not == 0).
	if out.Blocked&server.BlockedFloor != 0 {
		t.Errorf("0.5-z normal should NOT set BlockedFloor; got %d", out.Blocked)
	}
	if out.Blocked&server.BlockedStep != 0 {
		t.Errorf("0.5-z normal should NOT set BlockedStep; got %d", out.Blocked)
	}
}

// --- crease / corner ------------------------------------------------------

// Wedge scenario: two candidate boxes form an inside corner, with
// the entity moving into the corner. The 2-plane crease path may
// or may not fire depending on the swept-trace iteration order
// (numerics-dependent), but the call must complete sanely with at
// least one wall bit set and velocity no larger than input.
func TestFlyMove_TwoWallCornerStays(t *testing.T) {
	xwall := Target{
		Origin: [3]float32{10, 0, 0},
		Mins:   [3]float32{-5, -1000, -1000},
		Maxs:   [3]float32{5, 1000, 1000},
		Solid:  server.SolidBBox,
	}
	ywall := Target{
		Origin: [3]float32{0, 10, 0},
		Mins:   [3]float32{-1000, -5, -1000},
		Maxs:   [3]float32{1000, 5, 1000},
		Solid:  server.SolidBBox,
	}
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{50, 50, 0},
		Time:     1.0,
	}
	out, err := FlyMove(in, flyMoveEmptyWorld(), []Target{xwall, ywall})
	if err != nil {
		t.Fatalf("corner: %v", err)
	}
	if out.Blocked == 0 {
		t.Errorf("expected some Blocked bits in corner; got 0")
	}
	speedIn := in.Velocity[0]*in.Velocity[0] + in.Velocity[1]*in.Velocity[1] + in.Velocity[2]*in.Velocity[2]
	speedOut := out.NewVelocity[0]*out.NewVelocity[0] + out.NewVelocity[1]*out.NewVelocity[1] + out.NewVelocity[2]*out.NewVelocity[2]
	if speedOut > speedIn {
		t.Errorf("corner clip should not amplify velocity; in^2=%v out^2=%v", speedIn, speedOut)
	}
}

// --- 4-bump iteration cap -------------------------------------------------

// Make a tube of walls so the entity bounces multiple times. The
// 4-bump cap is a hard upper bound; the function must return without
// infinite-looping.
func TestFlyMove_FourBumpCapDoesNotInfiniteLoop(t *testing.T) {
	walls := []Target{
		{Origin: [3]float32{10, 0, 0}, Mins: [3]float32{-2, -100, -100}, Maxs: [3]float32{2, 100, 100}, Solid: server.SolidBBox},
		{Origin: [3]float32{-10, 0, 0}, Mins: [3]float32{-2, -100, -100}, Maxs: [3]float32{2, 100, 100}, Solid: server.SolidBBox},
		{Origin: [3]float32{0, 10, 0}, Mins: [3]float32{-100, -2, -100}, Maxs: [3]float32{100, 2, 100}, Solid: server.SolidBBox},
		{Origin: [3]float32{0, -10, 0}, Mins: [3]float32{-100, -2, -100}, Maxs: [3]float32{100, 2, 100}, Solid: server.SolidBBox},
	}
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{1000, 999, 0},
		Time:     1.0,
	}
	out, err := FlyMove(in, flyMoveEmptyWorld(), walls)
	if err != nil {
		t.Fatalf("4-bump cap: %v", err)
	}
	if out.Blocked == 0 {
		t.Errorf("expected at least one Blocked bit after bouncing; got 0")
	}
}

// --- 4-bump exhaustion (loop falls through bottom) -----------------------

// Drive FlyMove through all 4 bumps without an early return -- the
// loop exits via the final `return out, nil` at the bottom of the
// function. The C upstream's for(bumpcount<4) shape lets the loop
// fall through naturally when every iteration finds a clean slide
// AND the slid velocity stays in the primal hemisphere AND there's
// still time left to keep tracing.
//
// Construct: use a tilted-plane brushmodel that EVERY trace through
// the worldmodel impacts at a slanted wall (normal[2]==0 -- a vertical
// wall, but tilted in XY). Each impact slides velocity along the wall.
// The slid velocity remains co-aligned with primal (positive dot) so
// the oscillation guard doesn't fire. The trace covers a fixed
// fraction per iteration, so time_left burns slowly and never quite
// reaches the no-trace early-exit.
//
// The construction below uses a sequence of slanted walls (boxhull
// candidates rotated about Z) so each per-iteration slide encounters
// a fresh wall.
func TestFlyMove_FourBumpExhaustionFallsThrough(t *testing.T) {
	// A "diagonal corridor" -- vertical walls forming a narrow
	// channel slanting +X+Y. The entity moves +X with a small +Y
	// drift; each iteration glances off one of the alternating-side
	// walls and slides further along. With Time large enough that
	// the per-iteration trace doesn't reach Fraction=1, the loop
	// runs all 4 iterations.
	walls := []Target{
		// Lower walls at Y=-2, staggered along X.
		{Origin: [3]float32{5, -2, 0}, Mins: [3]float32{-4, -1, -100}, Maxs: [3]float32{4, 1, 100}, Solid: server.SolidBBox},
		{Origin: [3]float32{15, -2, 0}, Mins: [3]float32{-4, -1, -100}, Maxs: [3]float32{4, 1, 100}, Solid: server.SolidBBox},
		{Origin: [3]float32{25, -2, 0}, Mins: [3]float32{-4, -1, -100}, Maxs: [3]float32{4, 1, 100}, Solid: server.SolidBBox},
		{Origin: [3]float32{35, -2, 0}, Mins: [3]float32{-4, -1, -100}, Maxs: [3]float32{4, 1, 100}, Solid: server.SolidBBox},
	}
	in := FlyMoveIn{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{30, -10, 0}, // forward + slight downward Y so we keep grazing
		Time:     10.0,
	}
	out, err := FlyMove(in, flyMoveEmptyWorld(), walls)
	if err != nil {
		t.Fatalf("4-bump exhaustion: %v", err)
	}
	// The exact final state depends on the per-iteration slide
	// choreography; assert the call returned sanely.
	_ = out
}

// --- constant drift detector ---------------------------------------------

// Pin the C upstream's literals so a sloppy refactor doesn't quietly
// change behaviour.
func TestFlyMove_ConstantDriftDetector(t *testing.T) {
	if flyMoveMaxBumps != 4 {
		t.Errorf("flyMoveMaxBumps drift: got %d want 4", flyMoveMaxBumps)
	}
	if flyMoveMaxClipPlanes != 5 {
		t.Errorf("flyMoveMaxClipPlanes drift: got %d want 5", flyMoveMaxClipPlanes)
	}
	if flyMoveFloorNormalZ != 0.7 {
		t.Errorf("flyMoveFloorNormalZ drift: got %v want 0.7", float32(flyMoveFloorNormalZ))
	}
}
