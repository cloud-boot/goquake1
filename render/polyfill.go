// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// MaxPolyVerts is the per-poly vertex cap. Quake's BSP polygons are
// already clipped to convex with <= 16 verts; this leaves headroom.
const MaxPolyVerts = 64

var (
	ErrPolyTooFewVerts = errors.New("render: polygon needs >= 3 vertices")
	ErrPolyTooManyVerts = errors.New("render: polygon vertex count exceeds MaxPolyVerts")
	ErrPolyNilFB       = errors.New("render: nil framebuffer in polygon fill")
)

// FillPolygon paints the convex 2D polygon defined by verts with
// palette index c. Edge-walks every scanline in the polygon's
// y-range, finds left + right x crossings, fills the span between.
//
// Caller responsibilities:
//   - verts must be CONVEX (this function does not check; non-convex
//     input may produce visual artifacts or wrong fills)
//   - verts may be in clockwise or counter-clockwise order
//   - verts coordinates may extend outside the framebuffer; the fill
//     clips per-scanline (off-framebuffer rows are skipped; off-edge
//     columns are clamped)
//
// Algorithm: for each integer scanline y in the polygon's y-extent,
// walk all edges, find every edge that straddles y, sort the
// per-edge x intersections, fill between pairs.
//
// tyrquake: a simplified version of D_DrawFlat with no perspective
// or texture mapping -- the building block the rest of D_* extends
// with texel walking, lighting, and span lists.
//
// Returns ErrPolyNilFB / ErrPolyTooFewVerts / ErrPolyTooManyVerts on
// bad input; nil otherwise (a fully-off-screen poly is a silent
// no-op).
func FillPolygon(fb *FrameBuffer, verts [][2]float32, c byte) error {
	if fb == nil {
		return ErrPolyNilFB
	}
	if len(verts) < 3 {
		return ErrPolyTooFewVerts
	}
	if len(verts) > MaxPolyVerts {
		return ErrPolyTooManyVerts
	}

	yMin, yMax := verts[0][1], verts[0][1]
	for _, v := range verts[1:] {
		if v[1] < yMin {
			yMin = v[1]
		}
		if v[1] > yMax {
			yMax = v[1]
		}
	}

	yStart := int(math.Floor(float64(yMin)))
	yEnd := int(math.Ceil(float64(yMax)))
	if yStart < 0 {
		yStart = 0
	}
	if yEnd > fb.Height {
		yEnd = fb.Height
	}
	if yStart >= yEnd {
		return nil
	}

	for y := yStart; y < yEnd; y++ {
		yf := float32(y) + 0.5 // sample at scanline center
		// Find all edges crossing yf; record their x intersection.
		var xs [MaxPolyVerts]float32
		nXs := 0
		for i := 0; i < len(verts); i++ {
			a := verts[i]
			b := verts[(i+1)%len(verts)]
			y0, y1 := a[1], b[1]
			// Edge straddles yf iff one endpoint is on each side.
			if (y0 <= yf && y1 > yf) || (y1 <= yf && y0 > yf) {
				t := (yf - y0) / (y1 - y0)
				xs[nXs] = a[0] + t*(b[0]-a[0])
				nXs++
			}
		}
		// Convex polygon (caller-contract) gives 0 or 2+ crossings;
		// 0 means the scanline missed the polygon entirely. The
		// pair-fill below is a no-op when nXs == 0.
		//
		// Sort ascending (a convex polygon has at most 2 crossings
		// per scanline; bubble-sort is fine + handles >2 for
		// degenerate near-horizontal edges).
		for i := 1; i < nXs; i++ {
			for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
				xs[j-1], xs[j] = xs[j], xs[j-1]
			}
		}
		// Fill between consecutive pairs.
		for pair := 0; pair+1 < nXs; pair += 2 {
			x0 := int(math.Ceil(float64(xs[pair])))
			x1 := int(math.Floor(float64(xs[pair+1])))
			if x0 < 0 {
				x0 = 0
			}
			if x1 >= fb.Width {
				x1 = fb.Width - 1
			}
			if x0 > x1 {
				continue
			}
			row := y * fb.Pitch
			for x := x0; x <= x1; x++ {
				fb.Pixels[row+x] = c
			}
		}
	}
	return nil
}
