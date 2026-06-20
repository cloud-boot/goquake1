// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"math"
	"testing"
)

// DefaultPhysParams is a drift detector against the C upstream
// cvar literals -- if anyone bumps a default in DefaultPhysParams
// without updating the comment / NQ reference, this test fails
// pointing at the exact field.
func TestDefaultPhysParams_DriftDetector(t *testing.T) {
	got := DefaultPhysParams()
	want := PhysParams{
		MaxVelocity:  2000, // common/sv_phys.c sv_maxvelocity "2000"
		Gravity:      800,  // common/sv_phys.c sv_gravity "800"
		Friction:     4,    // common/sv_phys.c sv_friction "4"
		EdgeFriction: 2,    // NQ/sv_user.c sv_edgefriction "2"
		StopSpeed:    100,  // common/sv_phys.c sv_stopspeed "100"
		MaxSpeed:     320,  // common/sv_phys.c sv_maxspeed "320"
		Accelerate:   10,   // common/sv_phys.c sv_accelerate "10"
	}
	if got != want {
		t.Errorf("DefaultPhysParams drift:\n got %+v\nwant %+v", got, want)
	}
}

func TestClampVelocity_InRange(t *testing.T) {
	in := [3]float32{100, -200, 300}
	got := ClampVelocity(in, 2000)
	if got != in {
		t.Errorf("in-range mutated: got %v want %v", got, in)
	}
}

func TestClampVelocity_PerAxisAtPositiveMax(t *testing.T) {
	in := [3]float32{5000, 1, -1}
	got := ClampVelocity(in, 2000)
	want := [3]float32{2000, 1, -1}
	if got != want {
		t.Errorf("positive clamp: got %v want %v", got, want)
	}
}

func TestClampVelocity_PerAxisAtNegativeMax(t *testing.T) {
	in := [3]float32{-5000, 1, -1}
	got := ClampVelocity(in, 2000)
	want := [3]float32{-2000, 1, -1}
	if got != want {
		t.Errorf("negative clamp: got %v want %v", got, want)
	}
}

func TestClampVelocity_AllAxesClamp(t *testing.T) {
	in := [3]float32{5000, -5000, 3000}
	got := ClampVelocity(in, 2000)
	want := [3]float32{2000, -2000, 2000}
	if got != want {
		t.Errorf("all-axes clamp: got %v want %v", got, want)
	}
}

func TestClampVelocity_NaNReplacedWithZero(t *testing.T) {
	nan := float32(math.NaN())
	cases := []struct {
		name string
		in   [3]float32
		want [3]float32
	}{
		{"nan-x", [3]float32{nan, 50, 60}, [3]float32{0, 50, 60}},
		{"nan-y", [3]float32{40, nan, 60}, [3]float32{40, 0, 60}},
		{"nan-z", [3]float32{40, 50, nan}, [3]float32{40, 50, 0}},
		{"all-nan", [3]float32{nan, nan, nan}, [3]float32{0, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClampVelocity(c.in, 2000)
			if got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestClampVelocity_TinyAndHuge(t *testing.T) {
	tiny := ClampVelocity([3]float32{1e-30, -1e-30, 0}, 2000)
	wantTiny := [3]float32{1e-30, -1e-30, 0}
	if tiny != wantTiny {
		t.Errorf("tiny: got %v want %v", tiny, wantTiny)
	}
	huge := ClampVelocity([3]float32{1e30, -1e30, 1e30}, 2000)
	wantHuge := [3]float32{2000, -2000, 2000}
	if huge != wantHuge {
		t.Errorf("huge: got %v want %v", huge, wantHuge)
	}
}

func TestApplyGravity_DecreasesZ(t *testing.T) {
	got := ApplyGravity([3]float32{1, 2, 100}, 1, 800, 0.1)
	// dz = -1 * 800 * 0.1 = -80; new z = 20
	want := [3]float32{1, 2, 20}
	if got != want {
		t.Errorf("positive gravity: got %v want %v", got, want)
	}
}

func TestApplyGravity_FactorMultiplies(t *testing.T) {
	got := ApplyGravity([3]float32{0, 0, 100}, 2, 800, 0.1)
	// dz = -2 * 800 * 0.1 = -160; new z = -60
	want := [3]float32{0, 0, -60}
	if got != want {
		t.Errorf("factor multiplies: got %v want %v", got, want)
	}
}

func TestApplyGravity_FactorZeroPromotedToOne(t *testing.T) {
	gotZero := ApplyGravity([3]float32{0, 0, 100}, 0, 800, 0.1)
	gotOne := ApplyGravity([3]float32{0, 0, 100}, 1, 800, 0.1)
	if gotZero != gotOne {
		t.Errorf("gravityFactor=0 not promoted to 1: gotZero=%v gotOne=%v", gotZero, gotOne)
	}
}

func TestApplyGravity_DtZeroIsNoOp(t *testing.T) {
	in := [3]float32{1, 2, 3}
	got := ApplyGravity(in, 1, 800, 0)
	if got != in {
		t.Errorf("dt=0 no-op: got %v want %v", got, in)
	}
}

func TestApplyFriction_SpeedZeroNoOp(t *testing.T) {
	in := [3]float32{0, 0, 17}
	got := ApplyFriction(in, DefaultPhysParams(), false, 0.1)
	if got != in {
		t.Errorf("speed=0 no-op: got %v want %v", got, in)
	}
}

func TestApplyFriction_ProportionalAboveStopSpeed(t *testing.T) {
	// Horizontal speed = sqrt(200^2 + 0) = 200 > stopSpeed (100).
	// control = speed = 200. friction = 4. dt = 0.1.
	// newspeed = 200 - 0.1 * 200 * 4 = 200 - 80 = 120.
	// scale = 120/200 = 0.6.
	p := DefaultPhysParams()
	got := ApplyFriction([3]float32{200, 0, 50}, p, false, 0.1)
	want := [3]float32{120, 0, 30}
	if !approxEq3(got, want) {
		t.Errorf("proportional: got %v want %v", got, want)
	}
}

func TestApplyFriction_StopFloorBelowStopSpeed(t *testing.T) {
	// Horizontal speed = 50 < stopSpeed (100).
	// control = stopSpeed = 100. friction = 4. dt = 0.1.
	// newspeed = 50 - 0.1 * 100 * 4 = 50 - 40 = 10.
	// scale = 10/50 = 0.2.
	p := DefaultPhysParams()
	got := ApplyFriction([3]float32{50, 0, 50}, p, false, 0.1)
	want := [3]float32{10, 0, 10}
	if !approxEq3(got, want) {
		t.Errorf("stop floor: got %v want %v", got, want)
	}
}

func TestApplyFriction_OnEdgeSwapsFriction(t *testing.T) {
	// onEdge=true -> friction = friction * edgeFriction = 4 * 2 = 8.
	// Same as proportional case but with doubled friction:
	// newspeed = 200 - 0.1 * 200 * 8 = 200 - 160 = 40.
	// scale = 40/200 = 0.2.
	p := DefaultPhysParams()
	got := ApplyFriction([3]float32{200, 0, 50}, p, true, 0.1)
	want := [3]float32{40, 0, 10}
	if !approxEq3(got, want) {
		t.Errorf("onEdge: got %v want %v", got, want)
	}
}

func TestApplyFriction_MagnitudeReductionClampedNoSignFlip(t *testing.T) {
	// Pick dt enormous so dt*control*friction > speed -- the clamp
	// should zero the velocity rather than flip its sign.
	p := DefaultPhysParams()
	got := ApplyFriction([3]float32{50, 0, 7}, p, false, 100)
	want := [3]float32{0, 0, 0}
	if got != want {
		t.Errorf("clamp no sign flip: got %v want %v", got, want)
	}
}

func approxEq3(a, b [3]float32) bool {
	for i := 0; i < 3; i++ {
		if math.Abs(float64(a[i]-b[i])) > 1e-4 {
			return false
		}
	}
	return true
}
