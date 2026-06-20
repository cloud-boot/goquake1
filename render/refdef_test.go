// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
	"testing"
)

const eps = 1e-4

func approx(a, b float32) bool {
	d := float64(a - b)
	if d < 0 {
		d = -d
	}
	return d < eps
}

// ----- CalcFovY ----------------------------------------------------

func TestCalcFovY_Square90(t *testing.T) {
	got := CalcFovY(90, 400, 400)
	if !approx(got, 90) {
		t.Fatalf("CalcFovY(90, 400, 400) = %v want 90", got)
	}
}

func TestCalcFovY_WideAspect(t *testing.T) {
	// 16:9-ish, fovX = 90 -> fovY should be less than 90
	got := CalcFovY(90, 1600, 900)
	if got >= 90 || got <= 0 {
		t.Fatalf("CalcFovY widescreen = %v want < 90 and > 0", got)
	}
}

func TestCalcFovY_TallAspect(t *testing.T) {
	// fovX 60 with tall display -> fovY should be > 60
	got := CalcFovY(60, 400, 800)
	if got <= 60 {
		t.Fatalf("CalcFovY tall = %v want > 60", got)
	}
}

func TestCalcFovY_BadInput(t *testing.T) {
	cases := []struct {
		fovX  float32
		w, h  int
		label string
	}{
		{0, 400, 400, "fovX zero"},
		{-1, 400, 400, "fovX negative"},
		{180, 400, 400, "fovX 180"},
		{200, 400, 400, "fovX above 180"},
		{90, 0, 400, "width zero"},
		{90, -1, 400, "width negative"},
		{90, 400, 0, "height zero"},
		{90, 400, -1, "height negative"},
	}
	for _, c := range cases {
		if got := CalcFovY(c.fovX, c.w, c.h); got != 0 {
			t.Fatalf("CalcFovY(%v, %d, %d) = %v want 0 (%s)", c.fovX, c.w, c.h, got, c.label)
		}
	}
}

// ----- NewRefDef ---------------------------------------------------

func TestNewRefDef_Happy(t *testing.T) {
	rd, err := NewRefDef(
		VRect{X: 0, Y: 0, Width: 640, Height: 480},
		[3]float32{0, 90, 0},
		[3]float32{100, 200, 30},
		90,
	)
	if err != nil {
		t.Fatalf("NewRefDef: %v", err)
	}
	if rd.FovX != 90 {
		t.Fatalf("FovX = %v want 90", rd.FovX)
	}
	if rd.FovY <= 0 || rd.FovY >= 90 {
		t.Fatalf("FovY = %v want (0, 90) for 4:3 + 90fovX", rd.FovY)
	}
	if rd.ViewOrigin != [3]float32{100, 200, 30} {
		t.Fatalf("ViewOrigin not propagated")
	}
}

func TestNewRefDef_DimZero(t *testing.T) {
	cases := []VRect{
		{Width: 0, Height: 480},
		{Width: 640, Height: 0},
		{Width: -1, Height: 480},
		{Width: 640, Height: -1},
	}
	for _, c := range cases {
		_, err := NewRefDef(c, [3]float32{}, [3]float32{}, 90)
		if !errors.Is(err, ErrRefDimZero) {
			t.Fatalf("NewRefDef(%+v) err = %v want ErrRefDimZero", c, err)
		}
	}
}

func TestNewRefDef_FovRange(t *testing.T) {
	for _, fov := range []float32{0, -1, 180, 200} {
		_, err := NewRefDef(VRect{Width: 640, Height: 480}, [3]float32{}, [3]float32{}, fov)
		if !errors.Is(err, ErrRefFovRange) {
			t.Fatalf("NewRefDef fov=%v err = %v want ErrRefFovRange", fov, err)
		}
	}
}

// ----- SetupView ---------------------------------------------------

func TestSetupView_AtOriginZeroAngles(t *testing.T) {
	rd := &RefDef{
		ViewAngles: [3]float32{0, 0, 0},
		ViewOrigin: [3]float32{0, 0, 0},
	}
	a := rd.SetupView()
	// Translation must be zero.
	if a.T != [3]float32{0, 0, 0} {
		t.Fatalf("translation = %v want {0,0,0}", a.T)
	}
}

func TestSetupView_NonzeroOrigin(t *testing.T) {
	// At zero angles + origin (10, 20, 30), worldToView places the
	// origin at (-R * origin). R for zero angles = the identity in
	// view-frame ordering (right=-Y, up=+Z, forward=+X), so
	// the translation is just -origin under that basis.
	rd := &RefDef{
		ViewAngles: [3]float32{0, 0, 0},
		ViewOrigin: [3]float32{10, 20, 30},
	}
	a := rd.SetupView()
	// Verify: TransformAffine(a, origin) should be (0, 0, 0).
	out := TransformAffine(a, rd.ViewOrigin)
	for i, v := range out {
		if !approx(v, 0) {
			t.Fatalf("TransformAffine(SetupView, origin)[%d] = %v want 0", i, v)
		}
	}
}

// ----- BuildFrustum ------------------------------------------------

func TestBuildFrustum_PlanesPassThroughOrigin(t *testing.T) {
	rd := &RefDef{
		ViewAngles: [3]float32{0, 0, 0},
		ViewOrigin: [3]float32{100, 50, 10},
		FovX:       90,
		FovY:       90,
	}
	f := rd.BuildFrustum()
	for i, pl := range f {
		// Origin must lie ON each plane: Normal . origin - Dist == 0.
		d := pl.Normal[0]*rd.ViewOrigin[0] + pl.Normal[1]*rd.ViewOrigin[1] + pl.Normal[2]*rd.ViewOrigin[2] - pl.Dist
		if !approx(d, 0) {
			t.Fatalf("plane %d: origin not on plane (residual %v)", i, d)
		}
	}
}

func TestBuildFrustum_NormalsUnitLength(t *testing.T) {
	rd := &RefDef{
		ViewAngles: [3]float32{0, 45, 0},
		ViewOrigin: [3]float32{0, 0, 0},
		FovX:       90,
		FovY:       60,
	}
	f := rd.BuildFrustum()
	for i, pl := range f {
		l := math.Sqrt(float64(
			pl.Normal[0]*pl.Normal[0] + pl.Normal[1]*pl.Normal[1] + pl.Normal[2]*pl.Normal[2]))
		if math.Abs(l-1) > 1e-4 {
			t.Fatalf("plane %d normal length = %v want 1", i, l)
		}
	}
}

// ----- PointInFrustum ----------------------------------------------

func TestPointInFrustum_AheadInside(t *testing.T) {
	rd := &RefDef{
		ViewAngles: [3]float32{0, 0, 0},
		ViewOrigin: [3]float32{0, 0, 0},
		FovX:       90,
		FovY:       90,
	}
	f := rd.BuildFrustum()
	// Zero angles -> forward axis is +X. A point straight ahead.
	if !f.PointInFrustum([3]float32{100, 0, 0}) {
		t.Fatalf("point ahead not in frustum")
	}
}

func TestPointInFrustum_BehindOutside(t *testing.T) {
	rd := &RefDef{
		ViewAngles: [3]float32{0, 0, 0},
		ViewOrigin: [3]float32{0, 0, 0},
		FovX:       90,
		FovY:       90,
	}
	f := rd.BuildFrustum()
	// A point behind the camera.
	if f.PointInFrustum([3]float32{-100, 0, 0}) {
		t.Fatalf("point behind in frustum (want outside)")
	}
}

func TestPointInFrustum_AtCamera(t *testing.T) {
	rd := &RefDef{FovX: 90, FovY: 90}
	f := rd.BuildFrustum()
	// Camera origin sits on every plane -> Normal.P - Dist == 0
	// for every plane, which is the >= 0 boundary -> inside.
	if !f.PointInFrustum([3]float32{0, 0, 0}) {
		t.Fatalf("origin not in frustum (boundary case)")
	}
}

// ----- BoxInFrustum ------------------------------------------------

func TestBoxInFrustum_AheadInside(t *testing.T) {
	rd := &RefDef{FovX: 90, FovY: 90}
	f := rd.BuildFrustum()
	mins := [3]float32{50, -10, -10}
	maxs := [3]float32{100, 10, 10}
	if !f.BoxInFrustum(mins, maxs) {
		t.Fatalf("box ahead not in frustum")
	}
}

func TestBoxInFrustum_BehindOutside(t *testing.T) {
	rd := &RefDef{FovX: 90, FovY: 90}
	f := rd.BuildFrustum()
	mins := [3]float32{-100, -10, -10}
	maxs := [3]float32{-50, 10, 10}
	if f.BoxInFrustum(mins, maxs) {
		t.Fatalf("box behind in frustum (want outside)")
	}
}

func TestBoxInFrustum_Straddling(t *testing.T) {
	rd := &RefDef{FovX: 90, FovY: 90}
	f := rd.BuildFrustum()
	// Box straddles the camera origin in X -> p-vertex of every plane
	// should be on the inside side, so the box passes.
	mins := [3]float32{-10, -10, -10}
	maxs := [3]float32{10, 10, 10}
	if !f.BoxInFrustum(mins, maxs) {
		t.Fatalf("straddling box not in frustum")
	}
}

func TestBoxInFrustum_NegativeNormalComponent(t *testing.T) {
	// Force the p-vertex picker to hit BOTH branches of its if/else
	// (Normal[i] >= 0 vs < 0). A non-axis-aligned frustum direction
	// guarantees both signs across the 4 planes.
	rd := &RefDef{
		ViewAngles: [3]float32{20, 45, 10},
		ViewOrigin: [3]float32{0, 0, 0},
		FovX:       90,
		FovY:       60,
	}
	f := rd.BuildFrustum()
	// A reasonably-sized box centred in front of the camera.
	mins := [3]float32{-20, -20, -20}
	maxs := [3]float32{200, 200, 200}
	// We don't assert in-or-out; we just exercise the p-vertex
	// picker to cover both branches in every plane's coordinate.
	_ = f.BoxInFrustum(mins, maxs)
}
