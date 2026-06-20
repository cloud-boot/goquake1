// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package svuser

import (
	"math"
	"testing"
)

// approxEq reports whether a and b differ by at most eps. Used in
// place of strict equality so the float32 trig path through
// mathlib.AngleVectors -- which goes via math.Sin/Cos in float64
// and rounds back -- doesn't trip the comparisons.
func approxEq(a, b, eps float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

func approxEqVec(a, b [3]float32, eps float32) bool {
	return approxEq(a[0], b[0], eps) && approxEq(a[1], b[1], eps) && approxEq(a[2], b[2], eps)
}

// Pure forward move with the identity viewangles (looking down +X)
// should project the entire forward scalar onto the +X axis.
func TestCalcWishVel_ForwardIdentity(t *testing.T) {
	got := CalcWishVel(100, 0, 0, [3]float32{0, 0, 0})
	want := [3]float32{100, 0, 0}
	if !approxEqVec(got, want, 1e-4) {
		t.Errorf("forward-only: got %v want %v", got, want)
	}
}

// Pure side move with identity viewangles. AngleVectors's "right"
// for yaw=0 is (0, -1, 0) (Quake's right-hand rule with the
// negative-yaw rotation), so a positive sideMove projects onto -Y.
func TestCalcWishVel_SideIdentity(t *testing.T) {
	got := CalcWishVel(0, 100, 0, [3]float32{0, 0, 0})
	want := [3]float32{0, -100, 0}
	if !approxEqVec(got, want, 1e-4) {
		t.Errorf("side-only: got %v want %v", got, want)
	}
}

// Pure up move falls through unmodified into wishvel[2] -- the
// projection step writes nothing to Z.
func TestCalcWishVel_UpIdentity(t *testing.T) {
	got := CalcWishVel(0, 0, 50, [3]float32{0, 0, 0})
	want := [3]float32{0, 0, 50}
	if !approxEqVec(got, want, 1e-4) {
		t.Errorf("up-only: got %v want %v", got, want)
	}
}

// Yawing 90 degrees rotates "forward" from +X to +Y. So forward
// move with yaw=90 should produce wishvel along +Y.
func TestCalcWishVel_YawRotatesForward(t *testing.T) {
	got := CalcWishVel(100, 0, 0, [3]float32{0, 90, 0})
	want := [3]float32{0, 100, 0}
	if !approxEqVec(got, want, 1e-3) {
		t.Errorf("forward at yaw=90: got %v want %v", got, want)
	}
}

// All-zero input must produce the zero wishvel regardless of view
// angles -- the projection of zero is zero.
func TestCalcWishVel_AllZero(t *testing.T) {
	got := CalcWishVel(0, 0, 0, [3]float32{30, 45, 10})
	want := [3]float32{0, 0, 0}
	if !approxEqVec(got, want, 1e-4) {
		t.Errorf("all-zero input: got %v want %v", got, want)
	}
}

// Zero wishvel -> zero dir + zero speed (the C VectorNormalize
// length==0 short-circuit). Critically the caller must not divide
// by speed, so we explicitly assert speed==0 (not "approximately").
func TestSplitWishVel_Zero(t *testing.T) {
	dir, speed := SplitWishVel([3]float32{0, 0, 0})
	if dir != ([3]float32{0, 0, 0}) {
		t.Errorf("zero input dir: got %v want [0 0 0]", dir)
	}
	if speed != 0 {
		t.Errorf("zero input speed: got %v want 0", speed)
	}
}

// Nonzero wishvel -> unit dir + magnitude == input length. We pick
// a 3-4-5 triple so the expected magnitude is exact.
func TestSplitWishVel_Nonzero(t *testing.T) {
	dir, speed := SplitWishVel([3]float32{3, 4, 0})
	if !approxEq(speed, 5, 1e-5) {
		t.Errorf("speed: got %v want 5", speed)
	}
	wantDir := [3]float32{0.6, 0.8, 0}
	if !approxEqVec(dir, wantDir, 1e-5) {
		t.Errorf("dir: got %v want %v", dir, wantDir)
	}
	// dir must have unit length.
	l := math.Sqrt(float64(dir[0]*dir[0] + dir[1]*dir[1] + dir[2]*dir[2]))
	if math.Abs(l-1) > 1e-5 {
		t.Errorf("dir magnitude: got %v want 1", l)
	}
}

// Under-cap input passes through unchanged.
func TestClampWishSpeed_Under(t *testing.T) {
	in := [3]float32{3, 4, 0} // |in| = 5
	got := ClampWishSpeed(in, 320)
	if got != in {
		t.Errorf("under-cap should be a no-op: got %v want %v", got, in)
	}
}

// Exactly at the cap also passes through unchanged (the branch
// uses <=, so the equality case takes the early-return).
func TestClampWishSpeed_Exact(t *testing.T) {
	in := [3]float32{3, 4, 0} // |in| = 5
	got := ClampWishSpeed(in, 5)
	if got != in {
		t.Errorf("exact-cap should be a no-op: got %v want %v", got, in)
	}
}

// Over-cap input gets scaled so the magnitude meets the cap.
func TestClampWishSpeed_Over(t *testing.T) {
	in := [3]float32{300, 400, 0} // |in| = 500
	got := ClampWishSpeed(in, 100)
	mag := float32(math.Sqrt(float64(got[0]*got[0] + got[1]*got[1] + got[2]*got[2])))
	if !approxEq(mag, 100, 1e-3) {
		t.Errorf("clamped magnitude: got %v want 100", mag)
	}
	// Direction must be preserved: clamped vector parallel to input.
	// Check by ratio of components.
	if !approxEq(got[0]/got[1], in[0]/in[1], 1e-5) {
		t.Errorf("clamp must preserve direction: got %v from %v", got, in)
	}
}

// Zero input + any cap -> still zero (no division by zero, no NaN).
func TestClampWishSpeed_Zero(t *testing.T) {
	got := ClampWishSpeed([3]float32{0, 0, 0}, 320)
	if got != ([3]float32{0, 0, 0}) {
		t.Errorf("zero in should stay zero: got %v", got)
	}
}

// Stationary player + +X wishdir + non-trivial dt/accel should
// gain velocity along +X. With accelerate=10, dt=1, wishspeed=100
// the per-tick accelspeed is 10*1*100 = 1000, which is capped by
// addspeed=100 (= wishspeed-0), so the final +X velocity is 100.
func TestAccelerate_StationaryGainsAlongWishdir(t *testing.T) {
	got := Accelerate(
		[3]float32{0, 0, 0},
		[3]float32{1, 0, 0},
		100, 10, 1,
	)
	want := [3]float32{100, 0, 0}
	if !approxEqVec(got, want, 1e-4) {
		t.Errorf("stationary -> +X: got %v want %v", got, want)
	}
}

// Sub-cap accelspeed path: accel*dt*wishspeed < addspeed. With
// accelerate=1, dt=0.1, wishspeed=100, the per-tick accelspeed is
// 1*0.1*100 = 10, which is below addspeed=100. So the result must
// be exactly +10 along +X.
func TestAccelerate_AccelspeedBelowAddspeed(t *testing.T) {
	got := Accelerate(
		[3]float32{0, 0, 0},
		[3]float32{1, 0, 0},
		100, 1, 0.1,
	)
	want := [3]float32{10, 0, 0}
	if !approxEqVec(got, want, 1e-4) {
		t.Errorf("small-step accel: got %v want %v", got, want)
	}
}

// Once the player is already moving along wishdir at wishspeed,
// the addspeed branch fires (addspeed <= 0) and the velocity is
// returned untouched -- single-tick cannot overshoot the cap.
func TestAccelerate_AtCapIsNoOp(t *testing.T) {
	v := [3]float32{100, 0, 0}
	got := Accelerate(v, [3]float32{1, 0, 0}, 100, 10, 1)
	if got != v {
		t.Errorf("at-wishspeed is a no-op: got %v want %v", got, v)
	}
}

// Player moving opposite to wishdir: currentspeed along wishdir is
// negative, so addspeed = wishspeed - (negative) > wishspeed > 0.
// That path runs the accel branch -- but the resulting velocity
// only gains a +wishdir component, it never has a "reverse"
// (anti-wishdir) push subtracted out. We assert the X component
// strictly increased (started at -50, ended above -50).
func TestAccelerate_OpposingNeverReverses(t *testing.T) {
	v := [3]float32{-50, 0, 0}
	got := Accelerate(v, [3]float32{1, 0, 0}, 100, 10, 1)
	if !(got[0] > v[0]) {
		t.Errorf("opposing wishdir: X component should increase, got %v from %v", got[0], v[0])
	}
}

// The "already faster than wishspeed along wishdir" early-return:
// addspeed = wishspeed - currentspeed where currentspeed >
// wishspeed gives addspeed < 0 -- the strict-less branch
// complements the addspeed==0 branch above.
func TestAccelerate_AlreadyOverSpeedIsNoOp(t *testing.T) {
	v := [3]float32{200, 0, 0}
	got := Accelerate(v, [3]float32{1, 0, 0}, 100, 10, 1)
	if got != v {
		t.Errorf("over-cap is a no-op: got %v want %v", got, v)
	}
}
