// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"math"
	"testing"
)

// ----- ClampViewAngles ---------------------------------------------

func TestClampViewAngles_PitchUp(t *testing.T) {
	got := ClampViewAngles([3]float32{-100, 0, 0})
	if got[0] != -MaxPitchUp {
		t.Fatalf("pitch up clamp = %v want %v", got[0], -MaxPitchUp)
	}
}

func TestClampViewAngles_PitchDown(t *testing.T) {
	got := ClampViewAngles([3]float32{120, 0, 0})
	if got[0] != MaxPitchDown {
		t.Fatalf("pitch down clamp = %v want %v", got[0], MaxPitchDown)
	}
}

func TestClampViewAngles_PitchInRange(t *testing.T) {
	got := ClampViewAngles([3]float32{30, 0, 0})
	if got[0] != 30 {
		t.Fatalf("pitch in-range = %v want 30", got[0])
	}
}

func TestClampViewAngles_YawWrapHigh(t *testing.T) {
	got := ClampViewAngles([3]float32{0, 400, 0})
	if got[1] != 40 {
		t.Fatalf("yaw 400 = %v want 40", got[1])
	}
}

func TestClampViewAngles_YawWrapMultiple(t *testing.T) {
	got := ClampViewAngles([3]float32{0, 730, 0})
	if got[1] != 10 {
		t.Fatalf("yaw 730 = %v want 10", got[1])
	}
}

func TestClampViewAngles_YawWrapNegative(t *testing.T) {
	got := ClampViewAngles([3]float32{0, -10, 0})
	if got[1] != 350 {
		t.Fatalf("yaw -10 = %v want 350", got[1])
	}
}

func TestClampViewAngles_YawAlreadyValid(t *testing.T) {
	got := ClampViewAngles([3]float32{0, 180, 0})
	if got[1] != 180 {
		t.Fatalf("yaw 180 = %v want 180", got[1])
	}
}

func TestClampViewAngles_RollPos(t *testing.T) {
	got := ClampViewAngles([3]float32{0, 0, 100})
	if got[2] != MaxRollAbs {
		t.Fatalf("roll +100 = %v want %v", got[2], MaxRollAbs)
	}
}

func TestClampViewAngles_RollNeg(t *testing.T) {
	got := ClampViewAngles([3]float32{0, 0, -100})
	if got[2] != -MaxRollAbs {
		t.Fatalf("roll -100 = %v want %v", got[2], -MaxRollAbs)
	}
}

func TestClampViewAngles_RollInRange(t *testing.T) {
	got := ClampViewAngles([3]float32{0, 0, 20})
	if got[2] != 20 {
		t.Fatalf("roll 20 = %v want 20", got[2])
	}
}

// ----- ClampFovX ---------------------------------------------------

func TestClampFovX_InRange(t *testing.T) {
	if got := ClampFovX(90); got != 90 {
		t.Fatalf("ClampFovX(90) = %v want 90", got)
	}
}

func TestClampFovX_TooLow(t *testing.T) {
	if got := ClampFovX(5); got != MinFovX {
		t.Fatalf("ClampFovX(5) = %v want %v", got, MinFovX)
	}
}

func TestClampFovX_TooHigh(t *testing.T) {
	if got := ClampFovX(200); got != MaxFovX {
		t.Fatalf("ClampFovX(200) = %v want %v", got, MaxFovX)
	}
}

func TestClampFovX_NaN(t *testing.T) {
	if got := ClampFovX(float32(math.NaN())); got != DefaultFovX {
		t.Fatalf("ClampFovX(NaN) = %v want default %v", got, DefaultFovX)
	}
}

// ----- ApplyViewOffset ---------------------------------------------

func TestApplyViewOffset(t *testing.T) {
	got := ApplyViewOffset([3]float32{10, 20, 30}, 5)
	want := [3]float32{10, 20, 35}
	if got != want {
		t.Fatalf("ApplyViewOffset = %v want %v", got, want)
	}
}

func TestApplyViewOffset_NegativeBob(t *testing.T) {
	got := ApplyViewOffset([3]float32{10, 20, 30}, -3)
	if got[2] != 27 {
		t.Fatalf("z = %v want 27", got[2])
	}
}

// ----- PlaneSide ---------------------------------------------------

func TestPlaneSide_Front(t *testing.T) {
	pl := Plane{Normal: [3]float32{1, 0, 0}, Dist: 0}
	if got := PlaneSide([3]float32{5, 0, 0}, pl); got != 1 {
		t.Fatalf("PlaneSide front = %d want 1", got)
	}
}

func TestPlaneSide_Back(t *testing.T) {
	pl := Plane{Normal: [3]float32{1, 0, 0}, Dist: 0}
	if got := PlaneSide([3]float32{-5, 0, 0}, pl); got != -1 {
		t.Fatalf("PlaneSide back = %d want -1", got)
	}
}

func TestPlaneSide_On(t *testing.T) {
	pl := Plane{Normal: [3]float32{1, 0, 0}, Dist: 0}
	if got := PlaneSide([3]float32{0, 5, 5}, pl); got != 0 {
		t.Fatalf("PlaneSide on-plane = %d want 0", got)
	}
}

func TestPlaneSide_NonAxisPlane(t *testing.T) {
	// Plane: x + y = 10. Point (5, 5, 0) is on it.
	pl := Plane{Normal: [3]float32{0.7071, 0.7071, 0}, Dist: 7.071}
	if got := PlaneSide([3]float32{10, 10, 0}, pl); got != 1 {
		t.Fatalf("(10,10) front of x+y=10 = %d want 1", got)
	}
	if got := PlaneSide([3]float32{0, 0, 0}, pl); got != -1 {
		t.Fatalf("(0,0) back of x+y=10 = %d want -1", got)
	}
}
