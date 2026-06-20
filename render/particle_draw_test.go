// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func newDrawCtx(t *testing.T) (*FrameBuffer, *RefDef, *Pool) {
	t.Helper()
	fb, err := NewFrameBuffer(320, 200)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	rd, err := NewRefDef(
		VRect{Width: 320, Height: 200},
		[3]float32{0, 0, 0},     // zero angles -> forward = +X
		[3]float32{0, 0, 0},     // at world origin
		90,                      // 90deg fov
	)
	if err != nil {
		t.Fatalf("NewRefDef: %v", err)
	}
	pool := NewPool()
	return fb, rd, pool
}

func TestDrawParticles_NilFB(t *testing.T) {
	_, rd, pool := newDrawCtx(t)
	err := DrawParticles(nil, rd, pool, 0)
	if !errors.Is(err, ErrPartNilFB) {
		t.Fatalf("err = %v want ErrPartNilFB", err)
	}
}

func TestDrawParticles_NilPoolNoop(t *testing.T) {
	fb, rd, _ := newDrawCtx(t)
	if err := DrawParticles(fb, rd, nil, 0); err != nil {
		t.Fatalf("nil-pool DrawParticles err = %v want nil", err)
	}
}

func TestDrawParticles_NilRefDefNoop(t *testing.T) {
	fb, _, pool := newDrawCtx(t)
	if err := DrawParticles(fb, nil, pool, 0); err != nil {
		t.Fatalf("nil-rd DrawParticles err = %v want nil", err)
	}
}

func TestDrawParticles_StraightAheadHitsCenter(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	// One particle straight ahead at +X = 100, exactly on view axis.
	pool.Particles[0] = Particle{
		Origin: [3]float32{100, 0, 0},
		Color:  0x55,
		Die:    10, // alive
	}
	if err := DrawParticles(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticles: %v", err)
	}
	// Center pixel should now hold 0x55.
	cx, cy := fb.Width/2, fb.Height/2
	if got := fb.Pixels[cy*fb.Pitch+cx]; got != 0x55 {
		t.Fatalf("center pixel = %#x want 0x55", got)
	}
}

func TestDrawParticles_BehindCameraClipped(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	// Particle directly behind the camera (negative X is behind
	// when forward = +X).
	pool.Particles[0] = Particle{
		Origin: [3]float32{-100, 0, 0},
		Color:  0x77,
		Die:    10,
	}
	if err := DrawParticles(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticles: %v", err)
	}
	// Nothing should have been written.
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("behind-camera particle drew a pixel")
		}
	}
}

func TestDrawParticles_NearClipped(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	// Particle just under the near-clip threshold.
	pool.Particles[0] = Particle{
		Origin: [3]float32{ParticleNearClip - 1, 0, 0},
		Color:  0x33,
		Die:    10,
	}
	if err := DrawParticles(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticles: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("too-close particle drew a pixel")
		}
	}
}

func TestDrawParticles_ExpiredSkipped(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	pool.Particles[0] = Particle{
		Origin: [3]float32{100, 0, 0},
		Color:  0x44,
		Die:    -1, // free slot
	}
	if err := DrawParticles(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticles: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("expired particle drew a pixel")
		}
	}
}

func TestDrawParticles_OffScreenClipped(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	// Far to the side: at depth=100, a sideways offset of 10000
	// puts it well outside the view frustum + framebuffer.
	pool.Particles[0] = Particle{
		Origin: [3]float32{100, 10000, 0},
		Color:  0x66,
		Die:    10,
	}
	if err := DrawParticles(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticles: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("offscreen particle drew a pixel")
		}
	}
}

func TestDrawParticles_BadFovNoop(t *testing.T) {
	fb, _, pool := newDrawCtx(t)
	// A RefDef with FovX=0 -> tanHalfX=0 -> early-out (no panic on
	// the divide). This is a defensive guard; NewRefDef rejects fov
	// 0, but a hand-built rd might slip through.
	rd := &RefDef{FovX: 0, FovY: 0}
	pool.Particles[0] = Particle{Origin: [3]float32{100, 0, 0}, Color: 0x11, Die: 10}
	if err := DrawParticles(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticles: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("bad-fov path drew a pixel")
		}
	}
}
