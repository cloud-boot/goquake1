// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"math"
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

func TestWallFriction_TowardWallReduces(t *testing.T) {
	// velocity points partway into the wall (normal=(1,0,0)).
	// d = dot(unit(v), normal) > 0 -> formula applies.
	in := [3]float32{10, 0, 0}
	out := WallFriction(in, [3]float32{1, 0, 0}, 4.0, 0.1)
	// speed=10, unit=(1,0,0), d=1, scale=1*0.1*4=0.4.
	// out = (10 - 1*0.4, 0, 0) = (9.6, 0, 0).
	want := [3]float32{9.6, 0, 0}
	if !vec3ApproxEq(out, want, 1e-5) {
		t.Errorf("toward wall: got %v want %v", out, want)
	}
}

func TestWallFriction_AwayFromWallUnchanged(t *testing.T) {
	// dot(unit(v), normal) < 0 -> early return, velocity passes
	// through verbatim.
	in := [3]float32{-10, 0, 0}
	out := WallFriction(in, [3]float32{1, 0, 0}, 4.0, 0.1)
	if out != in {
		t.Errorf("away wall: got %v want %v (unchanged)", out, in)
	}
}

func TestWallFriction_ZeroVelocity(t *testing.T) {
	// Zero velocity -> speed==0 guard -> return verbatim.
	// Avoids the normalize-by-zero NaN that would otherwise
	// poison the dot.
	in := [3]float32{0, 0, 0}
	out := WallFriction(in, [3]float32{1, 0, 0}, 4.0, 0.1)
	if out != in {
		t.Errorf("zero velocity: got %v want zeros", out)
	}
}

func TestWallFriction_ZeroDt(t *testing.T) {
	// dt=0 -> scale=0 -> no change (formula still runs past
	// the dot guard).
	in := [3]float32{10, 0, 0}
	out := WallFriction(in, [3]float32{1, 0, 0}, 4.0, 0)
	if out != in {
		t.Errorf("dt=0: got %v want %v (unchanged)", out, in)
	}
}

func TestWallFriction_ZeroFriction(t *testing.T) {
	// friction=0 -> scale=0 -> no change.
	in := [3]float32{10, 0, 0}
	out := WallFriction(in, [3]float32{1, 0, 0}, 0, 0.1)
	if out != in {
		t.Errorf("friction=0: got %v want %v (unchanged)", out, in)
	}
}

func TestWallFriction_DiagonalInto(t *testing.T) {
	// velocity (10, 10, 0), normal (1, 0, 0). speed = sqrt(200),
	// unit = (1/sqrt2, 1/sqrt2, 0), d = 1/sqrt2. scale = d*dt*f.
	// out[0] = 10 - 1*scale ; out[1] unchanged (normal[1]=0).
	in := [3]float32{10, 10, 0}
	out := WallFriction(in, [3]float32{1, 0, 0}, 4.0, 0.5)
	sqrt2 := float32(math.Sqrt2)
	scale := (1 / sqrt2) * 0.5 * 4.0
	want := [3]float32{10 - scale, 10, 0}
	if !vec3ApproxEq(out, want, 1e-4) {
		t.Errorf("diagonal: got %v want %v", out, want)
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
