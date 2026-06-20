// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"testing"
)

// StopEpsilon is the literal STOP_EPSILON 0.1 from common/sv_phys.c.
// This drift detector pins the constant -- any tweak fails here
// pointing at the upstream macro.
func TestStopEpsilon_DriftDetector(t *testing.T) {
	if StopEpsilon != 0.1 {
		t.Errorf("StopEpsilon drift: got %v want 0.1", float32(StopEpsilon))
	}
}

// vec3ApproxEq compares two [3]float32 component-wise within tol --
// float32 reflection math accumulates ULP error past exact compare.
func vec3ApproxEq(a, b [3]float32, tol float32) bool {
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

func TestClipVelocity_VerticalWallStops(t *testing.T) {
	// Step bit: normal[2] == 0 ; overbounce=1 cancels the
	// into-wall component fully -> output zero (and the residual
	// gets snapped by StopEpsilon).
	out, blocked := ClipVelocity([3]float32{10, 0, 0}, [3]float32{1, 0, 0}, 1.0)
	if blocked != BlockedStep {
		t.Errorf("vertical wall: got blocked=%d want %d", blocked, BlockedStep)
	}
	if out != ([3]float32{0, 0, 0}) {
		t.Errorf("vertical wall: got out=%v want zeros", out)
	}
}

func TestClipVelocity_FloorStops(t *testing.T) {
	// Floor bit: normal[2] > 0 ; falling straight down against
	// an up-facing floor cancels.
	out, blocked := ClipVelocity([3]float32{0, 0, -10}, [3]float32{0, 0, 1}, 1.0)
	if blocked != BlockedFloor {
		t.Errorf("floor: got blocked=%d want %d", blocked, BlockedFloor)
	}
	if out != ([3]float32{0, 0, 0}) {
		t.Errorf("floor: got out=%v want zeros", out)
	}
}

func TestClipVelocity_SlopeSetsFloorBitOnly(t *testing.T) {
	// Slope with normal[2] > 0 (but not == 0): floor only,
	// no step. Use a 45-degree-ish slope.
	_, blocked := ClipVelocity([3]float32{0, 0, -10}, [3]float32{0, 0.7, 0.7}, 1.0)
	if blocked != BlockedFloor {
		t.Errorf("slope: got blocked=%d want %d (floor only)", blocked, BlockedFloor)
	}
}

func TestClipVelocity_OverbounceReflects(t *testing.T) {
	// overbounce > 1 -> reflected component is larger than the
	// cancellation amount, so the output points back the way it
	// came. backoff = 10 * 1.5 = 15; out[0] = 10 - 15 = -5.
	out, _ := ClipVelocity([3]float32{10, 0, 0}, [3]float32{1, 0, 0}, 1.5)
	if out[0] >= 0 {
		t.Errorf("overbounce reflect: got out[0]=%v, expected negative", out[0])
	}
	want := [3]float32{-5, 0, 0}
	if !vec3ApproxEq(out, want, 1e-5) {
		t.Errorf("overbounce reflect: got %v want %v", out, want)
	}
}

func TestClipVelocity_AbsorbsBelowOne(t *testing.T) {
	// overbounce < 1 -> output retains some forward component
	// (low magnitude). backoff = 10 * 0.5 = 5; out[0] = 10 - 5 = 5.
	out, _ := ClipVelocity([3]float32{10, 0, 0}, [3]float32{1, 0, 0}, 0.5)
	want := [3]float32{5, 0, 0}
	if !vec3ApproxEq(out, want, 1e-5) {
		t.Errorf("absorb: got %v want %v", out, want)
	}
}

func TestClipVelocity_SnapsToZeroBelowStopEpsilon(t *testing.T) {
	// Construct a clip where residual component falls within
	// ±StopEpsilon and gets snapped to 0. velocity[1]=0.05 starts
	// already under epsilon and survives (clip leaves it alone
	// because normal[1]==0), so we get out[1]=0 via the snap.
	out, _ := ClipVelocity([3]float32{10, 0.05, 0}, [3]float32{1, 0, 0}, 1.0)
	if out[1] != 0 {
		t.Errorf("StopEpsilon snap: got out[1]=%v want 0", out[1])
	}
	// And out[0] = 10 - 10 = 0 (also snapped, full cancel).
	if out[0] != 0 {
		t.Errorf("StopEpsilon snap: got out[0]=%v want 0", out[0])
	}
}

func TestClipVelocity_AlreadyMovingAwayStillApplies(t *testing.T) {
	// The C formula is unconditional: even when dot(velocity,
	// normal) < 0 (already moving away), it still runs. backoff
	// is negative, so out = in - normal*backoff actually
	// AMPLIFIES the away-component.
	in := [3]float32{-10, 0, 0}
	out, _ := ClipVelocity(in, [3]float32{1, 0, 0}, 1.0)
	// backoff = -10 ; change = -10 ; out[0] = -10 - (-10) = 0.
	// Wait -- this also cancels for overbounce=1. Use overbounce=2.
	out2, _ := ClipVelocity(in, [3]float32{1, 0, 0}, 2.0)
	// backoff = -20 ; change = -20 ; out[0] = -10 - (-20) = 10.
	want := [3]float32{10, 0, 0}
	if !vec3ApproxEq(out2, want, 1e-5) {
		t.Errorf("dot<0 + overbounce=2: got %v want %v", out2, want)
	}
	// Sanity: the dot<0+overbounce=1 case still cancels.
	if out != ([3]float32{0, 0, 0}) {
		t.Errorf("dot<0 + overbounce=1: got %v want zeros", out)
	}
}

func TestClipVelocity_NeitherFloorNorStep(t *testing.T) {
	// normal[2] is small negative -- downward-facing slope.
	// The C upstream uses literal `!normal[2]` for the step bit,
	// so any nonzero z (incl. negative) misses step. And
	// `normal[2] > 0` is false. So neither bit is set.
	_, blocked := ClipVelocity([3]float32{0, 1, 0}, [3]float32{0, 0.99, -0.14}, 1.0)
	if blocked != 0 {
		t.Errorf("downward-slope: got blocked=%d want 0", blocked)
	}
}

// TestWallFriction_FacingIntoWall: player at yaw=0 (forward=+X)
// hits a wall to the east whose outward normal points back at
// them (-X). d = dot(normal, forward) = -1 ; d+=0.5 -> -0.5.
// Formula runs with scale = 1+d = 0.5.
//
// Velocity (10, 5, 7) -> i = dot(normal, vel) = -10,
//
//	side  = vel - normal*i = (10-(-1)*(-10), 5-0, 7-0)
//	      = (10 - 10, 5, 7) = (0, 5, 7).
//	out[0] = 0 * 0.5 = 0
//	out[1] = 5 * 0.5 = 2.5
//	out[2] = velocity[2] PRESERVED (C never writes v[2]) -> 7.
func TestWallFriction_FacingIntoWall(t *testing.T) {
	in := [3]float32{10, 5, 7}
	normal := [3]float32{-1, 0, 0}
	angles := [3]float32{0, 0, 0}
	out := WallFriction(in, normal, angles)
	want := [3]float32{0, 2.5, 7}
	if !vec3ApproxEq(out, want, 1e-5) {
		t.Errorf("facing into wall: got %v want %v", out, want)
	}
}

// TestWallFriction_FacingAwayFromWall: yaw=180 -> forward=(-1,0,0).
// Wall normal=(-1,0,0) (same direction as facing). d = 1 + 0.5
// = 1.5 >= 0 -> early return.
func TestWallFriction_FacingAwayFromWall(t *testing.T) {
	in := [3]float32{10, 5, 7}
	normal := [3]float32{-1, 0, 0}
	angles := [3]float32{0, 180, 0}
	out := WallFriction(in, normal, angles)
	if !vec3ApproxEq(out, in, 1e-5) {
		t.Errorf("facing away: got %v want %v (unchanged)", out, in)
	}
}

// TestWallFriction_ParallelTripsEarlyReturn: yaw=90 -> forward=
// (0,1,0). Wall normal=(1,0,0). dot=0; d+=0.5 -> 0.5 >= 0
// -> early return. The 0.5 offset is what makes a parallel-facing
// player NOT get any wall drag.
func TestWallFriction_ParallelTripsEarlyReturn(t *testing.T) {
	in := [3]float32{10, 5, 7}
	normal := [3]float32{1, 0, 0}
	angles := [3]float32{0, 90, 0}
	out := WallFriction(in, normal, angles)
	if !vec3ApproxEq(out, in, 1e-5) {
		t.Errorf("parallel facing: got %v want %v (unchanged)", out, in)
	}
}

// TestWallFriction_ZeroVelocity: zero velocity but player facing
// the wall hard enough to trip the formula. i = 0, side = (0,0,0),
// out = (0*scale, 0*scale, velocity[2]=0) = zeros.
func TestWallFriction_ZeroVelocity(t *testing.T) {
	in := [3]float32{0, 0, 0}
	normal := [3]float32{-1, 0, 0}
	angles := [3]float32{0, 0, 0}
	out := WallFriction(in, normal, angles)
	if out != in {
		t.Errorf("zero velocity: got %v want zeros", out)
	}
}

// TestWallFriction_PreservesZ: critical Quake behavior -- the C
// only writes v[0] and v[1], so gravity (v[2]) survives every
// wall-friction tick. Without this the player would freeze in
// the air on contact.
func TestWallFriction_PreservesZ(t *testing.T) {
	in := [3]float32{10, 0, -50} // falling while pushing east
	normal := [3]float32{-1, 0, 0}
	angles := [3]float32{0, 0, 0}
	out := WallFriction(in, normal, angles)
	if out[2] != -50 {
		t.Errorf("z preservation: got out[2]=%v want -50", out[2])
	}
}

// TestWallFriction_PartialFacing: pitch=30deg means forward has
// a -sp z component. Wall normal=(-1,0,0). forward.normal =
// -cos(30) ~= -0.866. d = -0.866 + 0.5 = -0.366. scale = 0.634.
// Verify the path is taken (output differs from input).
func TestWallFriction_PartialFacing(t *testing.T) {
	in := [3]float32{10, 0, 0}
	normal := [3]float32{-1, 0, 0}
	angles := [3]float32{30, 0, 0} // pitched down 30deg
	out := WallFriction(in, normal, angles)
	// i = -10, side = (10 - (-1)*(-10), 0, 0) = (0, 0, 0)
	// out[0] = 0 * scale = 0 ; out[1] = 0 ; out[2] = velocity[2] = 0.
	want := [3]float32{0, 0, 0}
	if !vec3ApproxEq(out, want, 1e-5) {
		t.Errorf("partial facing: got %v want %v", out, want)
	}
}

// TestWallFriction_GrazingExactlyHalf: d = dot+0.5 == 0 exactly
// trips the `d >= 0` early return (the >= vs > matters in C).
// Construct: yaw so forward.normal = -0.5 exactly. forward at
// yaw=60 is (cos60, sin60, 0) = (0.5, sqrt3/2, 0). normal=(-1,0,0).
// forward.normal = -0.5. d = 0 -> early return.
func TestWallFriction_GrazingExactlyHalf(t *testing.T) {
	in := [3]float32{10, 5, 0}
	normal := [3]float32{-1, 0, 0}
	angles := [3]float32{0, 60, 0}
	out := WallFriction(in, normal, angles)
	// At exactly d==0 the >= check fires; output should be unchanged.
	// Allow tiny ULP tolerance: cos(60deg) computed via sinCos may
	// not be exactly 0.5, so d may be a hair below 0 and apply a
	// scale of ~1. Check it's at least very close to the input.
	if !vec3ApproxEq(out, in, 1e-3) {
		t.Errorf("grazing: got %v want ~%v", out, in)
	}
}

func TestMoveBlocked_Bits(t *testing.T) {
	// Drift detector on the bit literals -- tyrquake hard-codes
	// 1 (floor) and 2 (step) and SV_FlyMove reads them.
	if BlockedFloor != 1 {
		t.Errorf("BlockedFloor drift: got %d want 1", BlockedFloor)
	}
	if BlockedStep != 2 {
		t.Errorf("BlockedStep drift: got %d want 2", BlockedStep)
	}
}
