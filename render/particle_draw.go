// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// ParticleNearClip is the minimum view-space forward distance for a
// particle to be drawn. Particles closer than this (or behind the
// camera) are clipped. tyrquake: the close-particle guard inside
// R_DrawParticles plus the basic z > 0 test.
const ParticleNearClip = 16.0

var ErrPartNilFB = errors.New("render: nil framebuffer in DrawParticles")

// DrawParticles projects every alive particle in `pool` into screen
// space via `rd`'s view transform + the pixel scale derived from
// FovX, and writes a single palette-indexed pixel for each visible
// one. No z-buffer test (occlusion arrives with the world rasterizer
// in a later batch); particles always draw on top.
//
// `now` is wall-clock-like time used to skip expired particles
// (Die <= now -> free slot).
//
// tyrquake: R_DrawParticles in r_part.c. The Go port collapses the
// upstream's per-particle 2D quad to a single pixel; the multi-pixel
// version is a follow-up pass once the rasterizer is in.
//
// Returns ErrPartNilFB if fb == nil; nil otherwise (nil pool/rd is a
// silent no-op so frontends can call DrawParticles unconditionally
// each frame).
func DrawParticles(fb *FrameBuffer, rd *RefDef, pool *Pool, now float32) error {
	if fb == nil {
		return ErrPartNilFB
	}
	if pool == nil || rd == nil {
		return nil
	}

	view := rd.SetupView()
	// ViewMatrix lays out rows (right, up, forward), so after the
	// TransformAffine multiply the result components are:
	//   vp[0] = right  . (p - origin)  (positive = on viewer's right)
	//   vp[1] = up     . (p - origin)  (positive = above viewer)
	//   vp[2] = forward. (p - origin)  (positive = in front -- depth)
	//
	// Horizontal scale ties FovX to the framebuffer's half-width:
	//   tan(fovX/2) = halfWidth / scale  =>  scale = halfWidth / tan(fovX/2)
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
		// vp[2] is forward depth; reject near/behind.
		if vp[2] < ParticleNearClip {
			continue
		}
		invZ := 1 / vp[2]
		// right (vp[0]) maps to screen +X; up (vp[1]) maps to
		// screen -Y (framebuffer top has lower Y).
		sx := halfW + vp[0]*scale*invZ
		sy := halfH - vp[1]*scale*invZ
		px := int(sx)
		py := int(sy)
		if px < 0 || py < 0 || px >= fb.Width || py >= fb.Height {
			continue
		}
		fb.Pixels[py*fb.Pitch+px] = p.Color
	}
	return nil
}
