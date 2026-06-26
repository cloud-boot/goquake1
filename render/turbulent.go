// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// TurbScale is the texel-domain peak amplitude of the warp (the "amp"
// in u' = u + amp*sin((v+t)/16) -- matches tyrquake's AMP2 = 8 used by
// D_DrawTurbulent8 in d_scan.c when expanded into UV space). 8 texels
// gives the classic visible-but-readable ripple.
const TurbScale = 8.0

// TurbDivisor is the period divisor inside the sin() phase term. 16
// follows D_DrawTurbulent8's TURBSCALE = (256.0 / (2 * PI)) folded
// with the (uv >> 16) right shift -- the net effect is that a 64-texel
// stretch of UV walks one full warp cycle.
const TurbDivisor = 16.0

// TurbSinTableSize is the entry count of the precomputed sin LUT.
// tyrquake r_table.c uses 256 entries (one byte of phase resolution);
// we mirror that.
const TurbSinTableSize = 256

// turbSinTable is the warp's precomputed sin lookup, peak amplitude
// TurbScale. tyrquake: r_turbsin[] in r_table.c. The full-cycle index
// is x mod TurbSinTableSize; the input units are "phase tics", where
// one tic = (2*pi) / TurbSinTableSize radians.
var turbSinTable = func() [TurbSinTableSize]float32 {
	var t [TurbSinTableSize]float32
	for i := 0; i < TurbSinTableSize; i++ {
		phase := 2 * math.Pi * float64(i) / float64(TurbSinTableSize)
		t[i] = float32(TurbScale * math.Sin(phase))
	}
	return t
}()

// TurbSinTable returns a defensive copy of the engine's precomputed
// warp sin LUT, exposed for tests + alt rasterizers. Mutations to the
// returned slice do NOT affect the renderer's internal table.
func TurbSinTable() []float32 {
	out := make([]float32, TurbSinTableSize)
	copy(out, turbSinTable[:])
	return out
}

// turbWarp returns the LUT-sampled warp offset for the given UV-space
// coordinate (in texels) at time `t` (seconds). The phase formula
// matches tyrquake's D_DrawTurbulent: phase tic = (coord + t*40) /
// TurbDivisor, then wrap to TurbSinTableSize. The (t*40) term keeps
// the warp animating at ~CYCLE/6.4 s (tyrquake's CYCLE = 128 ticks at
// 20 Hz; here we collapse the unit into "phase tics per second").
func turbWarp(coord, t float32) float32 {
	// Phase in LUT ticks. Use float64 in the modulo to avoid catastrophic
	// cancellation at large coord+t (Quake maps can have UVs in the
	// thousands; t grows unbounded across a session).
	idxF := float64(coord+t*40.0) / float64(TurbDivisor)
	idx := int(math.Floor(idxF)) & (TurbSinTableSize - 1)
	return turbSinTable[idx]
}

// WarpUV computes the warped (u', v') given the source (u, v) and the
// elapsed time t (seconds). Public so tests + alt rasterizers can
// validate the warp math independently from the fill loop. The formula
// is the one D_DrawTurbulent applies per pixel:
//
//	u' = u + sin_lut((v + t*40) / 16)
//	v' = v + sin_lut((u + t*40) / 16)
//
// (with the LUT peak amplitude == TurbScale = 8 texels).
func WarpUV(u, v, t float32) (uOut, vOut float32) {
	return u + turbWarp(v, t), v + turbWarp(u, t)
}

// Sentinel errors returned by FillTurbulentPolygon. Distinct from
// ErrTexFill* so callers can tell the warp path failed without sniffing
// the message string.
var (
	ErrTurbFillNilFB     = errors.New("render: nil framebuffer in turbulent fill")
	ErrTurbFillNilTex    = errors.New("render: nil texture in turbulent fill")
	ErrTurbFillFewVerts  = errors.New("render: turbulent polygon needs >= 3 vertices")
	ErrTurbFillManyVerts = errors.New("render: turbulent polygon vertex count exceeds MaxPolyVerts")
)

// FillTurbulentPolygon paints the convex 2D polygon (defined by verts)
// with a sinusoidally-warped texture sample per pixel -- the engine's
// water + lava + slime renderer. tyrquake: D_DrawTurbulent8 in d_scan.c.
//
// The warp is identical to upstream: per output pixel the linearly
// interpolated (u, v) is offset by the precomputed sin-LUT amplitudes
//
//	u' = u + sin_lut((v + t*40) / 16)
//	v' = v + sin_lut((u + t*40) / 16)
//
// then wrapped into [0, tex.Width) / [0, tex.Height) before the
// per-pixel sample. The wrap matches tyrquake's `& 63` mask on the
// stock 64x64 liquid mips; for arbitrary mip sizes we do a positive
// modulo so non-power-of-two textures still tile cleanly.
//
// Errors:
//
//	ErrTurbFillNilFB      fb == nil
//	ErrTurbFillNilTex     tex == nil
//	ErrTurbFillFewVerts   len(verts) < 3
//	ErrTurbFillManyVerts  len(verts) > MaxPolyVerts
//	ErrPicShape           tex.Width*tex.Height != len(tex.Pixels)
func FillTurbulentPolygon(fb *FrameBuffer, tex *Pic, cm *ColorMap, lightLevel int, verts []TexturedVertex, timeSec float32) error {
	if fb == nil {
		return ErrTurbFillNilFB
	}
	if tex == nil {
		return ErrTurbFillNilTex
	}
	if len(verts) < 3 {
		return ErrTurbFillFewVerts
	}
	if len(verts) > MaxPolyVerts {
		return ErrTurbFillManyVerts
	}
	if tex.Width*tex.Height != len(tex.Pixels) {
		return ErrPicShape
	}

	texW := tex.Width
	texH := tex.Height

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
				uw, vw := WarpUV(u, v, timeSec)
				ui := positiveMod(int(math.Floor(float64(uw))), texW)
				vi := positiveMod(int(math.Floor(float64(vw))), texH)
				texel := tex.Pixels[vi*texW+ui]
				if cm != nil {
					texel = cm.LightIndex(lightLevel, texel)
				}
				fb.Pixels[row+x] = texel
			}
		}
	}
	return nil
}

// positiveMod returns the mathematically-positive (a mod n) for any
// signed a. Go's `%` on negative operands yields a negative remainder,
// which would index out of the texture's Pixels slice. Helper kept
// package-private so sky.go can share it without exposing modulo
// quirks on the public API.
func positiveMod(a, n int) int {
	if n <= 0 {
		return 0
	}
	r := a % n
	if r < 0 {
		r += n
	}
	return r
}
