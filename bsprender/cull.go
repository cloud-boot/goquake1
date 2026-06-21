// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender

import "github.com/go-quake1/engine/render"

// FrustumStatus is the tri-state result of [CullSubmodel]:
//
//   - [StatusFullyInside]  the box is entirely within all 4 planes
//   - [StatusOutside]      the box is entirely on the negative side of at
//     least one plane
//   - [StatusStraddling]   the box intersects at least one plane (partial
//     visibility)
//
// Renderers use [StatusFullyInside] to skip per-surface frustum tests
// (everything inside the box is automatically visible);
// [StatusOutside] to cull the whole submodel; [StatusStraddling] to
// fall through to per-surface checks.
type FrustumStatus int

// FrustumStatus constants -- see the type doc for the per-value
// renderer semantics.
const (
	StatusFullyInside FrustumStatus = 0
	StatusOutside     FrustumStatus = 1
	StatusStraddling  FrustumStatus = 2
)

// CullSphere reports whether the sphere centered at center with the
// given radius intersects the frustum. The test is exact for the four
// side planes: each plane's signed distance to the center must be
// >= -radius for the sphere to be visible.
//
// Sign convention matches [render.Frustum.PointInFrustum]: a point p
// is on the inside of plane pl iff pl.Normal . p - pl.Dist >= 0. The
// sphere extends radius units along the normal in either direction,
// so the sphere is fully outside plane pl iff
// pl.Normal . center - pl.Dist < -radius (i.e. even the deepest
// inside-pointing edge of the sphere doesn't reach the plane). If no
// plane reports the sphere fully outside, the sphere intersects (or
// is fully inside) the frustum.
//
// tyrquake: the alias-model equivalent of R_CullBox; CullSphere is
// cheaper than CullBox and matches the alias bbox's natural shape (a
// bounding-sphere derived from the .mdl header's radius field;
// r_main.c near R_DrawBrushModel uses the same `dist <= -model->radius`
// test for rotated submodels).
//
// Returns true iff the sphere intersects the frustum.
func CullSphere(f render.Frustum, center [3]float32, radius float32) bool {
	for _, pl := range f {
		d := pl.Normal[0]*center[0] + pl.Normal[1]*center[1] + pl.Normal[2]*center[2] - pl.Dist
		if d < -radius {
			return false
		}
	}
	return true
}

// CullSubmodel classifies a submodel's AABB against the frustum. For
// each plane:
//
//   - if the p-vertex (the box corner farthest along the plane's
//     inward normal -- the most-inside candidate) is outside, the
//     whole box is outside that plane -> return [StatusOutside].
//
//   - if the n-vertex (the box corner farthest against the inward
//     normal -- the most-outside candidate) is also on the inside,
//     the box is fully inside that plane; it counts toward
//     [StatusFullyInside].
//
//   - otherwise the box straddles that plane.
//
// If every plane reports the box fully inside -> [StatusFullyInside].
// Otherwise (some plane straddled, none rejected) ->
// [StatusStraddling].
//
// A degenerate zero-size box (mins == maxs) is just a point: the
// p-vertex and n-vertex coincide, so the box is either FullyInside or
// Outside on every plane; Straddling cannot occur for a point.
//
// tyrquake: the BSP equivalent of an n-vertex/p-vertex AABB test;
// matches the BoxOnPlaneSide trichotomy r_main.c uses for submodel
// clipflags (PSIDE_BACK -> Outside, PSIDE_FRONT -> FullyInside,
// PSIDE_BOTH -> Straddling).
func CullSubmodel(f render.Frustum, mins, maxs [3]float32) FrustumStatus {
	allInside := true
	for _, pl := range f {
		// p-vertex: most-inside corner along +Normal.
		// n-vertex: most-outside corner (opposite axis selection).
		var pv, nv [3]float32
		for i := 0; i < 3; i++ {
			if pl.Normal[i] >= 0 {
				pv[i] = maxs[i]
				nv[i] = mins[i]
			} else {
				pv[i] = mins[i]
				nv[i] = maxs[i]
			}
		}
		if pl.Normal[0]*pv[0]+pl.Normal[1]*pv[1]+pl.Normal[2]*pv[2]-pl.Dist < 0 {
			// Even the most-inside corner is outside -> whole box
			// is on the negative side of this plane.
			return StatusOutside
		}
		if pl.Normal[0]*nv[0]+pl.Normal[1]*nv[1]+pl.Normal[2]*nv[2]-pl.Dist < 0 {
			// p-vertex inside but n-vertex outside -> box straddles
			// this plane.
			allInside = false
		}
	}
	if allInside {
		return StatusFullyInside
	}
	return StatusStraddling
}
