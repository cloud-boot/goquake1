// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"

	"github.com/go-quake1/engine/mathlib"
)

// RefDef is the per-frame view definition the renderer reads to
// project the world. tyrquake: refdef_t in r_local.h + vrect_t.
type RefDef struct {
	VRect      VRect
	ViewAngles [3]float32
	ViewOrigin [3]float32
	FovX       float32
	FovY       float32
}

// VRect is the viewport rectangle in framebuffer pixels.
// tyrquake: vrect_t in vid.h.
type VRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Plane is Normal . P - Dist = 0; the positive half-space (inside)
// satisfies Normal . P - Dist >= 0. tyrquake: mplane_t in model.h.
type Plane struct {
	Normal [3]float32
	Dist   float32
}

// Frustum is the 4-plane view-frustum (left / right / top / bottom).
// Near and far are managed elsewhere in Q1's BSP traversal. Each
// plane's normal points INWARD (toward the visible region).
// tyrquake: view_clipplanes[] in r_local.h.
type Frustum [4]Plane

var (
	ErrRefDimZero  = errors.New("render: refdef vrect has zero width or height")
	ErrRefFovRange = errors.New("render: refdef fov out of (0, 180) range")
)

// CalcFovY returns the vertical fov derived from fovX + viewport
// aspect, the same way the C source does:
//
//	tan(fovY/2) = tan(fovX/2) * height / width
//
// Degrees in, degrees out. Returns 0 on bad input (fovX out of
// (0,180), width <= 0, height <= 0); the public path NewRefDef
// validates first.
func CalcFovY(fovX float32, width, height int) float32 {
	if fovX <= 0 || fovX >= 180 || width <= 0 || height <= 0 {
		return 0
	}
	const deg2rad = math.Pi / 180
	x := math.Tan(float64(fovX/2) * deg2rad)
	y := x * float64(height) / float64(width)
	return float32(math.Atan(y) / deg2rad * 2)
}

// NewRefDef builds a RefDef with FovY derived from FovX + the VRect
// aspect.
func NewRefDef(vrect VRect, viewAngles, viewOrigin [3]float32, fovX float32) (*RefDef, error) {
	if vrect.Width <= 0 || vrect.Height <= 0 {
		return nil, ErrRefDimZero
	}
	if fovX <= 0 || fovX >= 180 {
		return nil, ErrRefFovRange
	}
	return &RefDef{
		VRect:      vrect,
		ViewAngles: viewAngles,
		ViewOrigin: viewOrigin,
		FovX:       fovX,
		FovY:       CalcFovY(fovX, vrect.Width, vrect.Height),
	}, nil
}

// SetupView returns the world-to-view affine transform: R * (p - origin)
// = R*p - R*origin, packed as Affine{R, -R*origin}. tyrquake: the
// implicit transform formed by R_TransformVector in r_misc.c.
func (rd *RefDef) SetupView() Affine {
	r := ViewMatrix(rd.ViewAngles)
	// T = -R * origin
	t := TransformVector(r, rd.ViewOrigin)
	return Affine{
		R: r,
		T: [3]float32{-t[0], -t[1], -t[2]},
	}
}

// BuildFrustum returns the 4-plane frustum for the given RefDef.
// Each plane's Normal points INWARD; Dist = Normal . ViewOrigin so
// the plane passes through the camera.
//
// tyrquake: R_SetFrustum in r_main.c.
//
// Algorithm: rotate the forward axis around the up axis by ±(90 -
// fovX/2) for the left/right planes; rotate forward around right
// by ±(90 - fovY/2) for top/bottom. The resulting vector IS the
// plane normal (because the frustum apex is at the camera, the
// inward normal of each side plane is the forward axis tilted away
// from the visible region by 90 degrees minus half-fov).
func (rd *RefDef) BuildFrustum() Frustum {
	fwd, right, up := mathlib.AngleVectors(mathlib.Vec3(rd.ViewAngles))
	fwd3 := [3]float32{fwd[0], fwd[1], fwd[2]}
	right3 := [3]float32{right[0], right[1], right[2]}
	up3 := [3]float32{up[0], up[1], up[2]}

	halfX := 90 - rd.FovX/2
	halfY := 90 - rd.FovY/2

	var f Frustum
	f[0].Normal = RotatePointAroundVector(up3, fwd3, -halfX) // left
	f[1].Normal = RotatePointAroundVector(up3, fwd3, halfX)  // right
	f[2].Normal = RotatePointAroundVector(right3, fwd3, halfY)  // top
	f[3].Normal = RotatePointAroundVector(right3, fwd3, -halfY) // bottom

	for i := range f {
		f[i].Dist = f[i].Normal[0]*rd.ViewOrigin[0] +
			f[i].Normal[1]*rd.ViewOrigin[1] +
			f[i].Normal[2]*rd.ViewOrigin[2]
	}
	return f
}

// PointInFrustum returns true iff p lies in (or on) the positive half
// of every frustum plane.
func (f Frustum) PointInFrustum(p [3]float32) bool {
	for _, pl := range f {
		if pl.Normal[0]*p[0]+pl.Normal[1]*p[1]+pl.Normal[2]*p[2]-pl.Dist < 0 {
			return false
		}
	}
	return true
}

// BoxInFrustum returns true iff the AABB [mins, maxs] intersects the
// frustum. Uses the p-vertex test: for each plane, the box corner
// farthest along the plane's inward normal is the most-inside
// candidate; if even that corner is outside, the box is fully outside.
//
// tyrquake: R_CullBox in r_main.c.
func (f Frustum) BoxInFrustum(mins, maxs [3]float32) bool {
	for _, pl := range f {
		var p [3]float32
		for i := 0; i < 3; i++ {
			if pl.Normal[i] >= 0 {
				p[i] = maxs[i]
			} else {
				p[i] = mins[i]
			}
		}
		if pl.Normal[0]*p[0]+pl.Normal[1]*p[1]+pl.Normal[2]*p[2]-pl.Dist < 0 {
			return false
		}
	}
	return true
}
