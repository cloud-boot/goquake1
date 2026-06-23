// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// SkyTransparentIndex is the palette index id Software reserves inside
// the front (foreground) cloud layer for "see-through". The vanilla
// engine special-cases palette 0 here -- 255 is the *general* engine
// transparent index (DrawTransPic) but the sky compositor uses 0.
// tyrquake: R_InitSky in r_sky.c maps mip column 0 to "blue key".
const SkyTransparentIndex byte = 0

// SkyHalfWidth is the texel width of one half of the 256-wide sky
// miptex. Quake ships every sky texture pre-baked at 256x128: cols
// 0..127 are the foreground (cloud overlay with transparency), cols
// 128..255 are the background (solid sky dome). tyrquake hard-codes
// the same 128 (`SKYSHIFT` / SKYSIZE constants in r_sky.c).
const SkyHalfWidth = 128

// SkyTimeScale converts wall-clock seconds into texel pan offsets for
// the BACKGROUND layer. The FOREGROUND scrolls at 2x this rate (the
// classic parallax effect tyrquake calls "the cloud layer rolls
// faster"). 8.0 was chosen to match DrawSkyHorizon's existing scroll
// constant so the legacy band path + the dome path animate at the
// same tempo.
const SkyTimeScale = 8.0

// Sentinel errors returned by FillSkyPolygon.
var (
	ErrSkyFillNilFB         = errors.New("render: nil framebuffer in sky fill")
	ErrSkyFillNilTex        = errors.New("render: nil sky texture in sky fill")
	ErrSkyFillFewVerts      = errors.New("render: sky polygon needs >= 3 vertices")
	ErrSkyFillManyVerts     = errors.New("render: sky polygon vertex count exceeds MaxPolyVerts")
	ErrSkyFillNotDoubleWide = errors.New("render: sky texture width must be 2*height-aligned (>=2)")
)

// FillSkyPolygon paints the convex 2D polygon (defined by verts) with
// the engine's two-layer sky composite. tyrquake: D_DrawSkyScans in
// d_scan.c + R_DrawSkyChain in r_sky.c.
//
// Layer split: the sky miptex is 2*H wide. The LEFT half (cols
// 0..H-1) is the FRONT (cloud) layer, palette-keyed by
// SkyTransparentIndex. The RIGHT half (cols H..2H-1) is the BACK
// (solid dome) layer.
//
// Per-pixel composite:
//
//  1. Sample BACK at (u/2 + back_scroll, v/2) -- the /2 stretches the
//     dome over screen space, the /2 on V folds the upper half into a
//     wraparound dome (mirrors tyrquake's `r_skytex.y >> 1`).
//  2. Sample FRONT at (u + front_scroll, v).
//  3. If FRONT == SkyTransparentIndex, emit BACK; else emit FRONT.
//
// FRONT scrolls at 2x BACK so the cloud layer parallaxes over the
// dome. Both scrolls wrap modulo H so the texture tiles seamlessly.
//
// Errors:
//
//	ErrSkyFillNilFB          fb == nil
//	ErrSkyFillNilTex         tex == nil
//	ErrSkyFillFewVerts       len(verts) < 3
//	ErrSkyFillManyVerts      len(verts) > MaxPolyVerts
//	ErrSkyFillNotDoubleWide  tex.Width < 2 (need >= 2 to split halves)
//	ErrPicShape              tex.Width*tex.Height != len(tex.Pixels)
func FillSkyPolygon(fb *FrameBuffer, tex *Pic, verts []TexturedVertex, timeSec float32) error {
	if fb == nil {
		return ErrSkyFillNilFB
	}
	if tex == nil {
		return ErrSkyFillNilTex
	}
	if len(verts) < 3 {
		return ErrSkyFillFewVerts
	}
	if len(verts) > MaxPolyVerts {
		return ErrSkyFillManyVerts
	}
	if tex.Width < 2 {
		return ErrSkyFillNotDoubleWide
	}
	if tex.Width*tex.Height != len(tex.Pixels) {
		return ErrPicShape
	}

	halfW := tex.Width / 2 // matches the upstream 128 on stock skies
	texH := tex.Height
	// Per-frame scroll offsets (texels). FRONT scrolls 2x BACK.
	backScroll := timeSec * SkyTimeScale
	frontScroll := timeSec * SkyTimeScale * 2

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
		yf := float32(y) + 0.5
		var xs, us, vs [MaxPolyVerts]float32
		nXs := 0
		for i := 0; i < len(verts); i++ {
			a := verts[i]
			b := verts[(i+1)%len(verts)]
			y0, y1 := a.Y, b.Y
			if (y0 <= yf && y1 > yf) || (y1 <= yf && y0 > yf) {
				t := (yf - y0) / (y1 - y0)
				xs[nXs] = a.X + t*(b.X-a.X)
				us[nXs] = a.U + t*(b.U-a.U)
				vs[nXs] = a.V + t*(b.V-a.V)
				nXs++
			}
		}
		for i := 1; i < nXs; i++ {
			for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
				xs[j-1], xs[j] = xs[j], xs[j-1]
				us[j-1], us[j] = us[j], us[j-1]
				vs[j-1], vs[j] = vs[j], vs[j-1]
			}
		}
		for pair := 0; pair+1 < nXs; pair += 2 {
			xLeft := xs[pair]
			xRight := xs[pair+1]
			uLeft, uRight := us[pair], us[pair+1]
			vLeft, vRight := vs[pair], vs[pair+1]

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

			span := xRight - xLeft
			duDx := (uRight - uLeft) / span
			dvDx := (vRight - vLeft) / span

			row := y * fb.Pitch
			for x := x0; x <= x1; x++ {
				xf := float32(x) + 0.5
				u := uLeft + (xf-xLeft)*duDx
				v := vLeft + (xf-xLeft)*dvDx
				// FRONT sample: cols 0..halfW-1.
				fu := positiveMod(int(math.Floor(float64(u+frontScroll))), halfW)
				fv := positiveMod(int(math.Floor(float64(v))), texH)
				front := tex.Pixels[fv*tex.Width+fu]
				if front != SkyTransparentIndex {
					fb.Pixels[row+x] = front
					continue
				}
				// BACK sample: cols halfW..2*halfW-1, with U/V halved
				// so the dome stretches over screen space.
				bu := positiveMod(int(math.Floor(float64(u*0.5+backScroll))), halfW)
				bv := positiveMod(int(math.Floor(float64(v*0.5))), texH)
				back := tex.Pixels[bv*tex.Width+halfW+bu]
				fb.Pixels[row+x] = back
			}
		}
	}
	return nil
}

// _ = SkyHalfWidth // doc-only: 128 is the canonical Quake sky half
// width; FillSkyPolygon uses tex.Width/2 so non-stock skies that ship
// wider variants still split cleanly.
