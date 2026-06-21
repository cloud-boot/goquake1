// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestDrawParticleQuads_NilFB(t *testing.T) {
	_, rd, pool := newDrawCtx(t)
	err := DrawParticleQuads(nil, rd, pool, 0)
	if !errors.Is(err, ErrParticleQuadNilFB) {
		t.Fatalf("err = %v want ErrParticleQuadNilFB", err)
	}
}

func TestDrawParticleQuads_NilPoolNoop(t *testing.T) {
	fb, rd, _ := newDrawCtx(t)
	if err := DrawParticleQuads(fb, rd, nil, 0); err != nil {
		t.Fatalf("nil-pool err = %v", err)
	}
}

func TestDrawParticleQuads_NilRefDefNoop(t *testing.T) {
	fb, _, pool := newDrawCtx(t)
	if err := DrawParticleQuads(fb, nil, pool, 0); err != nil {
		t.Fatalf("nil-rd err = %v", err)
	}
}

func TestDrawParticleQuads_StraightAheadFillsCenter(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	pool.Particles[0] = Particle{
		Origin: [3]float32{100, 0, 0},
		Color:  0x55,
		Die:    10,
	}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	// Center pixel + several surrounding pixels should be 0x55
	// (the quad is several pixels per side).
	cx, cy := fb.Width/2, fb.Height/2
	if got := fb.Pixels[cy*fb.Pitch+cx]; got != 0x55 {
		t.Fatalf("center pixel = %#x want 0x55", got)
	}
}

func TestDrawParticleQuads_QuadCoversArea(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	pool.Particles[0] = Particle{
		Origin: [3]float32{50, 0, 0}, // closer -> bigger quad
		Color:  0x77,
		Die:    10,
	}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	// Count how many pixels have the quad color.
	count := 0
	for _, b := range fb.Pixels {
		if b == 0x77 {
			count++
		}
	}
	if count < 4 {
		t.Fatalf("quad covered only %d pixels; expected >= 4 for a close particle", count)
	}
}

func TestDrawParticleQuads_BehindCamera(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	pool.Particles[0] = Particle{
		Origin: [3]float32{-100, 0, 0},
		Color:  0x99,
		Die:    10,
	}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	for _, b := range fb.Pixels {
		if b == 0x99 {
			t.Fatalf("behind-camera particle drew pixels")
		}
	}
}

func TestDrawParticleQuads_ExpiredSkipped(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	pool.Particles[0] = Particle{
		Origin: [3]float32{100, 0, 0},
		Color:  0xAA,
		Die:    -1,
	}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	for _, b := range fb.Pixels {
		if b == 0xAA {
			t.Fatalf("expired particle drew pixels")
		}
	}
}

func TestDrawParticleQuads_FarSizeFloor(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	// Very far -> size formula yields 0; floor clamps to MinQuadSize.
	pool.Particles[0] = Particle{
		Origin: [3]float32{1e6, 0, 0},
		Color:  0xBB,
		Die:    10,
	}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	count := 0
	for _, b := range fb.Pixels {
		if b == 0xBB {
			count++
		}
	}
	// At MinQuadSize=1, the far particle should fill at least 1px.
	if count == 0 {
		t.Fatalf("far particle drew 0 pixels; floor not honored")
	}
}

func TestDrawParticleQuads_CloseSizeCap(t *testing.T) {
	fb, rd, pool := newDrawCtx(t)
	// Very close -> size formula would yield a huge quad; cap to MaxQuadSize.
	pool.Particles[0] = Particle{
		Origin: [3]float32{ParticleNearClip + 0.1, 0, 0},
		Color:  0xCC,
		Die:    10,
	}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	count := 0
	for _, b := range fb.Pixels {
		if b == 0xCC {
			count++
		}
	}
	// Capped at MaxQuadSize*MaxQuadSize pixels.
	if count > MaxQuadSize*MaxQuadSize {
		t.Fatalf("close particle drew %d pixels; cap (%d) not enforced", count, MaxQuadSize*MaxQuadSize)
	}
}

func TestDrawParticleQuads_BadFov(t *testing.T) {
	fb, _, pool := newDrawCtx(t)
	rd := &RefDef{FovX: 0, FovY: 0}
	pool.Particles[0] = Particle{Origin: [3]float32{100, 0, 0}, Color: 0xDD, Die: 10}
	if err := DrawParticleQuads(fb, rd, pool, 0); err != nil {
		t.Fatalf("DrawParticleQuads: %v", err)
	}
	for _, b := range fb.Pixels {
		if b == 0xDD {
			t.Fatalf("bad-fov path drew pixels")
		}
	}
}
