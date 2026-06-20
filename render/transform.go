// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "github.com/go-quake1/engine/mathlib"

// Mat3 is a 3x3 transform matrix in row-major order. Each row is a
// rotation axis (right / up / forward) when used as a view matrix;
// each row is a basis vector when used as an arbitrary rotation.
// tyrquake: `float[3][3]` declarations in mathlib.h.
type Mat3 [3][3]float32

// Affine is the renderer's split of tyrquake's float[3][4] affine
// transform: a 3x3 rotation plus a translation 3-vec. The C upstream
// inlines translation as the 4th column of each row; the Go port keeps
// them apart for clearer typing.
//
// Layout: an Affine transforms a point p as a.R * p + a.T.
type Affine struct {
	R Mat3       // rotation
	T [3]float32 // translation
}

// ConcatRotations multiplies two 3x3 rotation matrices: out = in1 * in2.
// tyrquake: R_ConcatRotations in mathlib.c.
//
// The result rotates a vector first by in2, then by in1 (matrix
// multiplication is right-applied). Pure function; in1 and in2 may
// alias each other but NOT the return value.
//
// Delegates to [mathlib.ConcatRotations] for the underlying 3x3 multiply
// so the renderer and the shared math library share one implementation.
func ConcatRotations(in1, in2 Mat3) Mat3 {
	return Mat3(mathlib.ConcatRotations(mathlib.Mat3(in1), mathlib.Mat3(in2)))
}

// ConcatAffine returns out = in1 * in2 (apply in2 first).
//
// Rotation part: out.R = in1.R * in2.R (via [ConcatRotations]).
// Translation:   out.T = in1.R * in2.T + in1.T.
//
// tyrquake: R_ConcatTransforms in mathlib.c, with the float[3][4] layout
// unpacked into the [Affine] struct.
func ConcatAffine(in1, in2 Affine) Affine {
	return Affine{
		R: ConcatRotations(in1.R, in2.R),
		T: TransformAffine(in1, in2.T),
	}
}

// TransformVector rotates v by m (no translation). Equivalent to a
// matrix-vector multiply for a pure rotation. tyrquake: the
// per-vertex TransformVector in r_misc.c, which dots the input against
// vright/vup/vpn -- the rows of the view matrix.
//
// Returns out = m * v.
func TransformVector(m Mat3, v [3]float32) [3]float32 {
	return [3]float32{
		m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2],
		m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2],
		m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2],
	}
}

// TransformAffine applies an Affine transform to v: out = a.R * v + a.T.
// tyrquake: the per-vertex transform applied to surface verts after
// the alias model's bonematrix concat.
func TransformAffine(a Affine, v [3]float32) [3]float32 {
	r := TransformVector(a.R, v)
	return [3]float32{r[0] + a.T[0], r[1] + a.T[1], r[2] + a.T[2]}
}

// RotatePointAroundVector rotates point `p` by `degrees` around the
// axis vector `axis` (which must be unit-length -- caller's job). The
// result is the new point. tyrquake: R_RotatePointAroundVector in
// mathlib.c (Rodrigues' rotation formula via a 3x3 builder).
//
// degrees uses tyrquake's convention (positive = counter-clockwise
// looking along the axis); pass radians instead via math.Pi/180
// conversion if your call site prefers.
//
// Delegates to [mathlib.RotatePointAroundVector] so the engine has a
// single implementation of the Rodrigues' formula.
func RotatePointAroundVector(axis, p [3]float32, degrees float32) [3]float32 {
	return [3]float32(mathlib.RotatePointAroundVector(mathlib.Vec3(axis), mathlib.Vec3(p), degrees))
}

// Identity returns the 3x3 identity matrix.
func Identity() Mat3 {
	return Mat3{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
}

// ViewMatrix builds a 3x3 view-rotation matrix from a viewangle
// triple (pitch, yaw, roll) in degrees. Rows are
//
//	row 0 = right vector
//	row 1 = up vector
//	row 2 = forward vector (the view-direction axis)
//
// This pairs with [TransformAffine] to project worldspace verts into
// the viewer's local frame. tyrquake: the AngleVectors output composed
// into vpn/vright/vup at R_SetupFrame, then dot-producted against
// vertex coordinates by TransformVector -- so the rows here are
// (vright, vup, vpn) in that order, matching r_misc.c:392.
func ViewMatrix(viewangles [3]float32) Mat3 {
	forward, right, up := mathlib.AngleVectors(mathlib.Vec3(viewangles))
	return Mat3{
		{right[0], right[1], right[2]},
		{up[0], up[1], up[2]},
		{forward[0], forward[1], forward[2]},
	}
}
