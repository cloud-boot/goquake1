// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// ParticleQuadBaseSize is the at-unit-depth pixel edge of the quad
// drawn per particle by DrawParticleQuads. At larger depths the
// quad shrinks; at closer depths it grows (capped at MaxQuadSize).
const ParticleQuadBaseSize = 8.0

// MaxQuadSize is the per-particle pixel cap (avoids drawing huge
// fullscreen quads for particles very close to the camera).
const MaxQuadSize = 16

// MinQuadSize is the per-particle pixel floor (so far-away particles
// stay visible as a 1px dot rather than vanishing into sub-pixel
// nothingness).
const MinQuadSize = 1

var ErrParticleQuadNilFB = errors.New("render: nil framebuffer in DrawParticleQuads")

// DrawParticleQuads is DrawParticles with depth-scaled multi-pixel
// quads instead of single-pixel writes. Each alive particle:
//
//  1. Transform world position into view space.
//  2. Near-clip on depth.
//  3. Perspective-divide to screen space.
//  4. Compute on-screen quad size = ParticleQuadBaseSize / depth *
//     fb.Width (clamped to [MinQuadSize, MaxQuadSize]).
//  5. DrawFill a (size x size) square centered on the projected position
//     with the particle's Color.
//
// vs. DrawParticles' single-pixel writes: this is the "close-up
// particles look like blobs not pixels" upgrade. Falls back to the
// 1px floor for far-away particles so distant explosions still
// register visually.
//
// tyrquake: a Q1-classic compromise sits between this and the full
// d_polyse software 2D quad rasterizer; for the Go port a flat-color
// DrawFill rectangle is sufficient quality + cheaper than the
// rotated quad upstream uses.
//
// Returns ErrParticleQuadNilFB if fb == nil. nil pool / nil rd are
// silent no-ops (mirrors DrawParticles).
func DrawParticleQuads(fb *FrameBuffer, rd *RefDef, pool *Pool, now float32) error {
	if fb == nil {
		return ErrParticleQuadNilFB
	}
	if pool == nil || rd == nil {
		return nil
	}

	view := rd.SetupView()
	const deg2rad = math.Pi / 180
	tanHalfX := float32(math.Tan(float64(rd.FovX/2) * deg2rad))
	if tanHalfX <= 0 {
		return nil
	}
	halfW := float32(fb.Width) / 2
	halfH := float32(fb.Height) / 2
	scale := halfW / tanHalfX

	for i := range pool.Particles {
		p := &pool.Particles[i]
		if p.Die <= now {
			continue
		}
		vp := TransformAffine(view, p.Origin)
		if vp[2] < ParticleNearClip {
			continue
		}
		invZ := 1 / vp[2]
		sx := halfW + vp[0]*scale*invZ
		sy := halfH - vp[1]*scale*invZ

		// Quad size scales inversely with depth; clamp to [Min, Max].
		size := int(ParticleQuadBaseSize * scale * invZ)
		if size < MinQuadSize {
			size = MinQuadSize
		}
		if size > MaxQuadSize {
			size = MaxQuadSize
		}

		// Center the quad on the projected point.
		x := int(sx) - size/2
		y := int(sy) - size/2
		// DrawFill handles clipping + off-screen no-op.
		_ = DrawFill(fb, x, y, size, size, p.Color)
	}
	return nil
}
