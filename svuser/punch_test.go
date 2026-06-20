// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package svuser

import "testing"

// At-rest input (all zero) stays at zero -- the decay step has
// nothing to bite.
func TestDropPunchAngle_Zero(t *testing.T) {
	got := DropPunchAngle([3]float32{0, 0, 0}, 0.1)
	want := [3]float32{0, 0, 0}
	if got != want {
		t.Errorf("zero input: got %v want %v", got, want)
	}
}

// Large punch + small dt: each axis shrinks by 10*dt exactly, sign
// preserved (none of the axes are close enough to zero to snap).
func TestDropPunchAngle_LargeShrinksProportionally(t *testing.T) {
	// dt=0.01 -> step=0.1; all axes well above 0.1 in magnitude.
	got := DropPunchAngle([3]float32{5, 7, -3}, 0.01)
	want := [3]float32{4.9, 6.9, -2.9}
	if !approxEqVec(got, want, 1e-5) {
		t.Errorf("large punch: got %v want %v", got, want)
	}
}

// Small punch (|axis| < 10*dt) snaps to exactly 0, no sign flip.
func TestDropPunchAngle_SnapsToZero(t *testing.T) {
	// dt=1.0 -> step=10; every axis under 10 in magnitude.
	got := DropPunchAngle([3]float32{0.5, -0.5, 9.999}, 1.0)
	want := [3]float32{0, 0, 0}
	if got != want {
		t.Errorf("snap: got %v want %v", got, want)
	}
}

// Negative punch decays toward 0 from below: sign preserved while
// |axis| > step, then snaps to 0.
func TestDropPunchAngle_NegativeSignPreserved(t *testing.T) {
	got := DropPunchAngle([3]float32{-5, 0, 0}, 0.01) // step=0.1
	want := [3]float32{-4.9, 0, 0}
	if !approxEqVec(got, want, 1e-5) {
		t.Errorf("negative decay: got %v want %v", got, want)
	}
}

// Boundary: |axis| exactly equal to the step lands at 0 (the
// `<=` arm of the snap branch).
func TestDropPunchAngle_ExactStepLandsAtZero(t *testing.T) {
	got := DropPunchAngle([3]float32{1.0, -1.0, 0}, 0.1) // step=1.0
	want := [3]float32{0, 0, 0}
	if got != want {
		t.Errorf("exact-step: got %v want %v", got, want)
	}
}

// Axes decay independently: a non-zero X coexists with zero Y/Z and
// each axis is processed on its own.
func TestDropPunchAngle_PerAxisIndependence(t *testing.T) {
	got := DropPunchAngle([3]float32{5, 0, 0}, 0.01) // step=0.1
	want := [3]float32{4.9, 0, 0}
	if !approxEqVec(got, want, 1e-5) {
		t.Errorf("X-only: got %v want %v", got, want)
	}
}

// flatSampler returns the same floor height at every offset.
func flatSampler(z float32) PitchSampler {
	return func(_ float32) (float32, bool) { return z, true }
}

// stairsSampler returns a floor that climbs (or descends) linearly
// at `slopePerUnit` units of Z per world unit of forward offset.
func stairsSampler(slopePerUnit float32) PitchSampler {
	return func(off float32) (float32, bool) { return off * slopePerUnit, true }
}

// Flat floor: every sample returns the same Z, no step is above
// onEpsilon, dir stays zero -> 0.
func TestIdealPitch_Flat(t *testing.T) {
	got := IdealPitch(flatSampler(42), 0.8)
	if got != 0 {
		t.Errorf("flat: got %v want 0", got)
	}
}

// Rising floor (positive slope) -> negative pitch (look down at
// the rising ground). The C convention is `-dir * scale` and dir
// is positive for an ascending floor, so the return is negative.
func TestIdealPitch_RisingNegative(t *testing.T) {
	// slope=1.0 unit Z per unit forward: consecutive samples 12
	// units apart give a per-step delta of 12; dir=12.
	got := IdealPitch(stairsSampler(1.0), 0.8)
	want := float32(-12 * 0.8)
	if !approxEq(got, want, 1e-4) {
		t.Errorf("rising: got %v want %v", got, want)
	}
}

// Descending floor: positive pitch (look up away from the
// dropping ground).
func TestIdealPitch_DescendingPositive(t *testing.T) {
	got := IdealPitch(stairsSampler(-1.0), 0.8)
	want := float32(12 * 0.8) // -dir, dir=-12
	if !approxEq(got, want, 1e-4) {
		t.Errorf("descending: got %v want %v", got, want)
	}
}

// Any sampler miss aborts the whole computation, regardless of
// position in the 6-sample sequence.
func TestIdealPitch_MissAtFirst(t *testing.T) {
	miss := func(_ float32) (float32, bool) { return 0, false }
	if got := IdealPitch(miss, 0.8); got != 0 {
		t.Errorf("miss at first: got %v want 0", got)
	}
}

func TestIdealPitch_MissInMiddle(t *testing.T) {
	calls := 0
	s := func(off float32) (float32, bool) {
		calls++
		if calls == 4 {
			return 0, false
		}
		return off, true
	}
	if got := IdealPitch(s, 0.8); got != 0 {
		t.Errorf("miss at middle: got %v want 0", got)
	}
}

func TestIdealPitch_MissAtLast(t *testing.T) {
	calls := 0
	s := func(off float32) (float32, bool) {
		calls++
		if calls == maxForward {
			return 0, false
		}
		return off, true
	}
	if got := IdealPitch(s, 0.8); got != 0 {
		t.Errorf("miss at last: got %v want 0", got)
	}
}

// Scale=0 zeroes any pitch unconditionally.
func TestIdealPitch_ScaleZero(t *testing.T) {
	if got := IdealPitch(stairsSampler(1.0), 0); got != 0 {
		t.Errorf("scale=0: got %v want 0", got)
	}
}

// Scale=2 doubles the result vs scale=1 (linear in the cvar).
func TestIdealPitch_ScaleLinear(t *testing.T) {
	s := stairsSampler(1.0)
	one := IdealPitch(s, 1)
	two := IdealPitch(s, 2)
	if !approxEq(two, 2*one, 1e-4) {
		t.Errorf("scale-linear: 2x=%v want %v", two, 2*one)
	}
}

// Mixed step directions (some up, some down beyond ON_EPSILON in
// opposite directions) -> 0, exercising the "mixed changes" early
// return.
func TestIdealPitch_MixedDirections(t *testing.T) {
	// Heights at offsets 36, 48, 60, 72, 84, 96: alternating
	// ascending / descending so step deltas flip sign by far more
	// than ON_EPSILON.
	heights := map[float32]float32{
		36: 0,
		48: 10, // step +10
		60: 0,  // step -10  (opposite sign, far beyond epsilon)
		72: 0,
		84: 0,
		96: 0,
	}
	s := func(off float32) (float32, bool) { return heights[off], true }
	if got := IdealPitch(s, 0.8); got != 0 {
		t.Errorf("mixed: got %v want 0", got)
	}
}

// Only ONE non-flat step -> steps < 2 -> 0. The rest of the floor
// is flat so dir is set exactly once.
func TestIdealPitch_SingleStepInsufficient(t *testing.T) {
	heights := map[float32]float32{
		36: 0,
		48: 10, // step +10
		60: 10, // step  0 (flat, skipped)
		72: 10,
		84: 10,
		96: 10,
	}
	s := func(off float32) (float32, bool) { return heights[off], true }
	if got := IdealPitch(s, 0.8); got != 0 {
		t.Errorf("single-step: got %v want 0", got)
	}
}

// All steps small but non-zero in the SAME direction across the
// six samples: every consecutive delta is below ON_EPSILON so it's
// skipped, dir stays zero, return 0. This covers the "all-flat"
// terminal branch where dir==0 even though heights vary slightly.
func TestIdealPitch_SubEpsilonStepsAllFlat(t *testing.T) {
	// Step delta of 0.05 (< onEpsilon=0.1) per offset.
	s := func(off float32) (float32, bool) { return off * 0.001, true }
	// Each consecutive offset diff is 12; step = 12 * 0.001 =
	// 0.012, well under 0.1.
	if got := IdealPitch(s, 0.8); got != 0 {
		t.Errorf("sub-epsilon: got %v want 0", got)
	}
}

// Two consistent steps (rest flat) is the minimum that qualifies:
// dir set, steps==2, scale applied.
func TestIdealPitch_TwoConsistentSteps(t *testing.T) {
	heights := map[float32]float32{
		36: 0,
		48: 5, // step +5
		60: 5,
		72: 10, // step +5
		84: 10,
		96: 10,
	}
	s := func(off float32) (float32, bool) { return heights[off], true }
	got := IdealPitch(s, 0.8)
	want := float32(-5 * 0.8)
	if !approxEq(got, want, 1e-4) {
		t.Errorf("two-steps: got %v want %v", got, want)
	}
}
