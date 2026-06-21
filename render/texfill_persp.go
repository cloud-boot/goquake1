// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// PerspTexturedVertex extends TexturedVertex with the Z component
// for perspective-correct UV interpolation. Z is the view-space
// depth (typically vp[2] from the BSP/alias vertex transform); X/Y
// are screen-space pixels; U/V are texture pixels in [0, tex.Width)
// / [0, tex.Height).
type PerspTexturedVertex struct {
	X, Y, Z float32
	U, V    float32
}

// PerspSubdivStep is the per-scanline sub-span length in pixels.
// Within each sub-span U/V are interpolated linearly (cheap); at
// every sub-span boundary we divide 1/Z to get real Z then real
// U/V. 8 is the upstream default; smaller = more accurate (more
// divides) per scanline.
const PerspSubdivStep = 8

// Sentinel errors returned by FillPerspectiveTexturedPolygon.
var (
	ErrPerspTexFillNilFB     = errors.New("render: nil framebuffer in perspective textured fill")
	ErrPerspTexFillNilTex    = errors.New("render: nil texture in perspective textured fill")
	ErrPerspTexFillFewVerts  = errors.New("render: perspective textured polygon needs >= 3 vertices")
	ErrPerspTexFillManyVerts = errors.New("render: perspective textured polygon vertex count exceeds MaxPolyVerts")
	ErrPerspTexFillZeroZ     = errors.New("render: perspective textured polygon vertex has Z <= 0")
)

// FillPerspectiveTexturedPolygon paints the convex 2D polygon
// (defined by its screen-space X/Y) with perspective-correct UV
// sampling. tyrquake: D_DrawSpans8 / D_DrawSpans16 (the choice of
// subdivision interval is hard-coded here; switching to 16 px is
// trivial via PerspSubdivStep).
//
// Algorithm:
//
//  1. Edge-walk same as FillPolygon. At each edge crossing, carry
//     the homogeneous (1/Z, U/Z, V/Z) values, NOT the raw (Z, U, V).
//  2. Per scanline pair (left, right): linearly interpolate the
//     homogeneous values across the span.
//  3. Every PerspSubdivStep pixels: divide (U/Z) / (1/Z) = U and
//     (V/Z) / (1/Z) = V; use those as the start of the next
//     sub-span; LINEARLY interpolate U + V between sub-spans.
//     The final partial sub-span (< 8 px) uses one extra divide
//     at the right edge.
//  4. Clamp U / V to texture bounds before sampling.
//  5. Light through colormap if cm != nil.
//
// Returns:
//
//	ErrPerspTexFillNilFB / NilTex / FewVerts / ManyVerts
//	ErrPerspTexFillZeroZ  any vertex has Z <= 0 (caller should
//	                      frustum-clip first)
//	ErrPicShape           tex.Width*tex.Height != len(tex.Pixels)
func FillPerspectiveTexturedPolygon(fb *FrameBuffer, tex *Pic, cm *ColorMap, lightLevel int, verts []PerspTexturedVertex) error {
	if fb == nil {
		return ErrPerspTexFillNilFB
	}
	if tex == nil {
		return ErrPerspTexFillNilTex
	}
	if len(verts) < 3 {
		return ErrPerspTexFillFewVerts
	}
	if len(verts) > MaxPolyVerts {
		return ErrPerspTexFillManyVerts
	}
	if tex.Width*tex.Height != len(tex.Pixels) {
		return ErrPicShape
	}
	for _, v := range verts {
		if v.Z <= 0 {
			return ErrPerspTexFillZeroZ
		}
	}

	uMax := tex.Width - 1
	vMax := tex.Height - 1

	// Pre-compute homogeneous coords per vertex.
	var hOoz, hUoz, hVoz [MaxPolyVerts]float32
	for i, v := range verts {
		inv := 1.0 / v.Z
		hOoz[i] = inv
		hUoz[i] = v.U * inv
		hVoz[i] = v.V * inv
	}

	yMin, yMax := verts[0].Y, verts[0].Y
	for _, v := range verts[1:] {
		if v.Y < yMin {
			yMin = v.Y
		}
		if v.Y > yMax {
			yMax = v.Y
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
		yf := float32(y) + 0.5 // scanline center
		// Per-edge crossing buffers: x position + homogeneous
		// (1/z, u/z, v/z) at that crossing.
		var xs, oozs, uozs, vozs [MaxPolyVerts]float32
		nXs := 0
		for i := 0; i < len(verts); i++ {
			j := (i + 1) % len(verts)
			y0, y1 := verts[i].Y, verts[j].Y
			if (y0 <= yf && y1 > yf) || (y1 <= yf && y0 > yf) {
				t := (yf - y0) / (y1 - y0)
				xs[nXs] = verts[i].X + t*(verts[j].X-verts[i].X)
				oozs[nXs] = hOoz[i] + t*(hOoz[j]-hOoz[i])
				uozs[nXs] = hUoz[i] + t*(hUoz[j]-hUoz[i])
				vozs[nXs] = hVoz[i] + t*(hVoz[j]-hVoz[i])
				nXs++
			}
		}
		// Insertion-sort crossings by x; carry homogeneous values.
		for i := 1; i < nXs; i++ {
			for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
				xs[j-1], xs[j] = xs[j], xs[j-1]
				oozs[j-1], oozs[j] = oozs[j], oozs[j-1]
				uozs[j-1], uozs[j] = uozs[j], uozs[j-1]
				vozs[j-1], vozs[j] = vozs[j], vozs[j-1]
			}
		}
		for pair := 0; pair+1 < nXs; pair += 2 {
			xLeft, xRight := xs[pair], xs[pair+1]
			oozL, oozR := oozs[pair], oozs[pair+1]
			uozL, uozR := uozs[pair], uozs[pair+1]
			vozL, vozR := vozs[pair], vozs[pair+1]

			x0 := int(math.Ceil(float64(xLeft)))
			x1 := int(math.Floor(float64(xRight)))
			if x0 < 0 {
				x0 = 0
			}
			if x1 >= fb.Width {
				x1 = fb.Width - 1
			}
			if x0 > x1 {
				continue
			}

			// Per-pixel step of the homogeneous values across the span.
			span := xRight - xLeft
			dOoz := (oozR - oozL) / span
			dUoz := (uozR - uozL) / span
			dVoz := (vozR - vozL) / span

			// Initial homogeneous values at the FIRST pixel center
			// (x0 + 0.5), offset from xLeft.
			xf := float32(x0) + 0.5
			ooz := oozL + (xf-xLeft)*dOoz
			uoz := uozL + (xf-xLeft)*dUoz
			voz := vozL + (xf-xLeft)*dVoz

			// First divide: real (u, v) at the first pixel.
			z := 1.0 / ooz
			u := uoz * z
			v := voz * z

			row := y * fb.Pitch
			count := x1 - x0 + 1
			pix := x0

			// Sub-span loop, D_DrawSpans8 pattern: full 8-pixel
			// sub-spans first (one divide at the far end + linear
			// step over 8 pixels), then a final partial sub-span.
			for count > 0 {
				var spanLen int
				var uNext, vNext float32
				if count > PerspSubdivStep {
					spanLen = PerspSubdivStep
					// Advance the homogeneous accumulators to the
					// start of the NEXT sub-span (8 pixels along).
					oozEnd := ooz + dOoz*float32(PerspSubdivStep)
					uozEnd := uoz + dUoz*float32(PerspSubdivStep)
					vozEnd := voz + dVoz*float32(PerspSubdivStep)
					zEnd := 1.0 / oozEnd
					uNext = uozEnd * zEnd
					vNext = vozEnd * zEnd
				} else {
					// Final sub-span: <= PerspSubdivStep pixels.
					// One divide at the LAST pixel (count-1 along)
					// to pin the endpoint without overshooting.
					spanLen = count
					steps := float32(spanLen - 1)
					oozEnd := ooz + dOoz*steps
					uozEnd := uoz + dUoz*steps
					vozEnd := voz + dVoz*steps
					zEnd := 1.0 / oozEnd
					uNext = uozEnd * zEnd
					vNext = vozEnd * zEnd
				}

				// Per-pixel linear (u, v) step within the sub-span.
				var du, dv float32
				if spanLen > 1 {
					inv := 1.0 / float32(spanLen)
					if count <= PerspSubdivStep {
						// Final partial sub-span: divide by
						// (spanLen-1) so we land exactly on the
						// pinned endpoint without overshoot.
						inv = 1.0 / float32(spanLen-1)
					}
					du = (uNext - u) * inv
					dv = (vNext - v) * inv
				}

				cu, cv := u, v
				for k := 0; k < spanLen; k++ {
					ui := int(math.Floor(float64(cu)))
					vi := int(math.Floor(float64(cv)))
					if ui < 0 {
						ui = 0
					} else if ui > uMax {
						ui = uMax
					}
					if vi < 0 {
						vi = 0
					} else if vi > vMax {
						vi = vMax
					}
					texel := tex.Pixels[vi*tex.Width+ui]
					if cm != nil {
						texel = cm.LightIndex(lightLevel, texel)
					}
					fb.Pixels[row+pix+k] = texel
					cu += du
					cv += dv
				}

				// Advance to the next sub-span: homogeneous
				// accumulators step by spanLen pixels; (u, v)
				// resume from the divided-exact endpoint.
				ooz += dOoz * float32(spanLen)
				uoz += dUoz * float32(spanLen)
				voz += dVoz * float32(spanLen)
				u = uNext
				v = vNext
				pix += spanLen
				count -= spanLen
			}
		}
	}
	return nil
}
