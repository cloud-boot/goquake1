// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"math"
	"testing"
)

const float32Epsilon = 1e-5

func absF32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func feq(a, b float32) bool { return absF32(a-b) <= float32Epsilon }

func veq(a, b [3]float32) bool {
	return feq(a[0], b[0]) && feq(a[1], b[1]) && feq(a[2], b[2])
}

func meq(a, b Mat3) bool {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if !feq(a[i][j], b[i][j]) {
				return false
			}
		}
	}
	return true
}

// rotZ90 rotates +X -> +Y about Z (column-vector convention, applied
// as out = M * v).
var rotZ90 = Mat3{
	{0, -1, 0},
	{1, 0, 0},
	{0, 0, 1},
}

// rotX90 rotates +Y -> +Z about X.
var rotX90 = Mat3{
	{1, 0, 0},
	{0, 0, -1},
	{0, 1, 0},
}

// --- Identity ---------------------------------------------------------

func TestIdentity(t *testing.T) {
	want := Mat3{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	if got := Identity(); !meq(got, want) {
		t.Errorf("Identity: got %v want %v", got, want)
	}
}

// --- ConcatRotations --------------------------------------------------

func TestConcatRotations_IdentityLeft(t *testing.T) {
	got := ConcatRotations(Identity(), rotZ90)
	if !meq(got, rotZ90) {
		t.Errorf("I * M: got %v want %v", got, rotZ90)
	}
}

func TestConcatRotations_IdentityRight(t *testing.T) {
	got := ConcatRotations(rotZ90, Identity())
	if !meq(got, rotZ90) {
		t.Errorf("M * I: got %v want %v", got, rotZ90)
	}
}

func TestConcatRotations_NonCommutative(t *testing.T) {
	ab := ConcatRotations(rotZ90, rotX90)
	ba := ConcatRotations(rotX90, rotZ90)
	if meq(ab, ba) {
		t.Errorf("ConcatRotations should be non-commutative; got equal: %v", ab)
	}
	// Apply (rotZ90 * rotX90) to +Y: rotX90 sends +Y to +Z, then rotZ90
	// keeps +Z fixed -> +Z. Apply (rotX90 * rotZ90) to +Y: rotZ90 sends
	// +Y to -X, then rotX90 keeps -X fixed -> -X.
	if got := TransformVector(ab, [3]float32{0, 1, 0}); !veq(got, [3]float32{0, 0, 1}) {
		t.Errorf("rotZ90*rotX90 * +Y: got %v want (0,0,1)", got)
	}
	if got := TransformVector(ba, [3]float32{0, 1, 0}); !veq(got, [3]float32{-1, 0, 0}) {
		t.Errorf("rotX90*rotZ90 * +Y: got %v want (-1,0,0)", got)
	}
}

// --- TransformVector --------------------------------------------------

func TestTransformVector_RotZ90MapsXToY(t *testing.T) {
	got := TransformVector(rotZ90, [3]float32{1, 0, 0})
	if !veq(got, [3]float32{0, 1, 0}) {
		t.Errorf("rotZ90 * +X: got %v want (0,1,0)", got)
	}
}

// --- TransformAffine + ConcatAffine -----------------------------------

func TestTransformAffine_IdentityRotationPlusTranslation(t *testing.T) {
	a := Affine{R: Identity(), T: [3]float32{4, 5, 6}}
	got := TransformAffine(a, [3]float32{1, 2, 3})
	if !veq(got, [3]float32{5, 7, 9}) {
		t.Errorf("identity+T: got %v want (5,7,9)", got)
	}
}

func TestConcatAffine_RotateThenTranslateVsTranslateThenRotate(t *testing.T) {
	rot := Affine{R: rotZ90, T: [3]float32{0, 0, 0}}
	trans := Affine{R: Identity(), T: [3]float32{10, 0, 0}}
	p := [3]float32{1, 0, 0}

	// First trans, then rot: apply trans, then apply rot.
	// In a.R*b.R*p + a.R*b.T + a.T, with a=rot, b=trans:
	//   = rotZ90 * (I*p + (10,0,0)) + 0
	//   = rotZ90 * (11, 0, 0) = (0, 11, 0)
	rotAfterTrans := ConcatAffine(rot, trans)
	if got := TransformAffine(rotAfterTrans, p); !veq(got, [3]float32{0, 11, 0}) {
		t.Errorf("rot*trans * (1,0,0): got %v want (0,11,0)", got)
	}

	// First rot, then trans: a=trans, b=rot.
	//   = I * (rotZ90 * (1,0,0)) + (10,0,0)
	//   = (0,1,0) + (10,0,0) = (10,1,0)
	transAfterRot := ConcatAffine(trans, rot)
	if got := TransformAffine(transAfterRot, p); !veq(got, [3]float32{10, 1, 0}) {
		t.Errorf("trans*rot * (1,0,0): got %v want (10,1,0)", got)
	}

	// And the composed transforms themselves are not equal.
	if meq(rotAfterTrans.R, transAfterRot.R) && veq(rotAfterTrans.T, transAfterRot.T) {
		t.Errorf("ConcatAffine should not be commutative here")
	}
}

// --- RotatePointAroundVector -----------------------------------------

func TestRotatePointAroundVector_Quarter(t *testing.T) {
	got := RotatePointAroundVector([3]float32{0, 0, 1}, [3]float32{1, 0, 0}, 90)
	if !veq(got, [3]float32{0, 1, 0}) {
		t.Errorf("rot 90 about Z of +X: got %v want (0,1,0)", got)
	}
}

func TestRotatePointAroundVector_Zero(t *testing.T) {
	p := [3]float32{3, -2, 7}
	got := RotatePointAroundVector([3]float32{0, 0, 1}, p, 0)
	if !veq(got, p) {
		t.Errorf("rot 0 should be identity: got %v want %v", got, p)
	}
}

func TestRotatePointAroundVector_FullTurn(t *testing.T) {
	p := [3]float32{1, 2, 3}
	axis := [3]float32{0, 1, 0}
	got := RotatePointAroundVector(axis, p, 360)
	// Accept a slightly looser tolerance for the 360-degree round trip --
	// sin/cos of 2*pi accumulate the usual ULP wobble.
	const tol = float32(1e-4)
	if absF32(got[0]-p[0]) > tol || absF32(got[1]-p[1]) > tol || absF32(got[2]-p[2]) > tol {
		t.Errorf("rot 360 should be ~identity: got %v want %v", got, p)
	}
}

// --- ViewMatrix -------------------------------------------------------

func TestViewMatrix_ZeroAngles(t *testing.T) {
	m := ViewMatrix([3]float32{0, 0, 0})
	// Row 2 (forward / vpn) should be +X for Quake's convention.
	if !veq([3]float32{m[2][0], m[2][1], m[2][2]}, [3]float32{1, 0, 0}) {
		t.Errorf("ViewMatrix(0,0,0) row 2 = forward: got %v want (1,0,0)", [3]float32{m[2][0], m[2][1], m[2][2]})
	}
	// Row 0 (right / vright) should be along the -Y axis for Q1 with all
	// zero angles -- AngleVectors at p=y=r=0 gives right = (0, -1, 0).
	if !veq([3]float32{m[0][0], m[0][1], m[0][2]}, [3]float32{0, -1, 0}) {
		t.Errorf("ViewMatrix(0,0,0) row 0 = right: got %v want (0,-1,0)", [3]float32{m[0][0], m[0][1], m[0][2]})
	}
	// Row 1 (up / vup) at zero angles is +Z.
	if !veq([3]float32{m[1][0], m[1][1], m[1][2]}, [3]float32{0, 0, 1}) {
		t.Errorf("ViewMatrix(0,0,0) row 1 = up: got %v want (0,0,1)", [3]float32{m[1][0], m[1][1], m[1][2]})
	}
}

func TestViewMatrix_Yaw90(t *testing.T) {
	// Yaw +90 rotates the forward vector from +X to +Y.
	m := ViewMatrix([3]float32{0, 90, 0})
	if !veq([3]float32{m[2][0], m[2][1], m[2][2]}, [3]float32{0, 1, 0}) {
		t.Errorf("ViewMatrix yaw=90 forward: got %v want (0,1,0)",
			[3]float32{m[2][0], m[2][1], m[2][2]})
	}
}

// Sanity-check that ViewMatrix actually projects world coords into the
// viewer frame: at zero angles, a world point straight in front of the
// viewer should land on +X of the local frame after TransformVector.
func TestViewMatrix_TransformsWorldToView(t *testing.T) {
	m := ViewMatrix([3]float32{0, 0, 0})
	out := TransformVector(m, [3]float32{1, 0, 0})
	// row0*v = -y = 0, row1*v = z = 0, row2*v = x = 1 -> (0, 0, 1) in
	// local frame.
	if !veq(out, [3]float32{0, 0, 1}) {
		t.Errorf("ViewMatrix(0,0,0) * (1,0,0): got %v want (0,0,1)", out)
	}
}

// Final sanity: the renderer's epsilon is consistent with mathlib's.
func TestEpsilonConvention(t *testing.T) {
	if math.Abs(float64(float32Epsilon)-1e-5) > 1e-12 {
		t.Errorf("renderer epsilon drifted from mathlib's 1e-5")
	}
}
