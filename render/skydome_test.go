// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

// makeDualLayerSky builds a 256x128 sky mip with distinguishable
// halves so the composite tests can verify which layer was sampled.
//
// FRONT (cols 0..127): alternating SkyTransparentIndex (0) and a
// front-marker byte 0xAA. The 0-cells let the BACK show through.
//
// BACK  (cols 128..255): solid back-marker byte 0xBB.
func makeDualLayerSky(_ *testing.T) *Pic {
	const w, h = 256, 128
	const halfW = 128
	const frontMarker byte = 0xAA
	const backMarker byte = 0xBB
	pixels := make([]byte, w*h)
	for v := 0; v < h; v++ {
		for u := 0; u < halfW; u++ {
			// Even cols -> transparent, odd cols -> marker.
			if u%2 == 0 {
				pixels[v*w+u] = SkyTransparentIndex
			} else {
				pixels[v*w+u] = frontMarker
			}
		}
		for u := halfW; u < w; u++ {
			pixels[v*w+u] = backMarker
		}
	}
	return &Pic{Width: w, Height: h, Pixels: pixels}
}

// --- error paths -----------------------------------------------------

func TestFillSkyPolygon_NilFB(t *testing.T) {
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	if err := FillSkyPolygon(nil, tex, verts, 0); !errors.Is(err, ErrSkyFillNilFB) {
		t.Fatalf("err = %v want ErrSkyFillNilFB", err)
	}
}

func TestFillSkyPolygon_NilTex(t *testing.T) {
	fb, _ := NewFrameBuffer(8, 8)
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	if err := FillSkyPolygon(fb, nil, verts, 0); !errors.Is(err, ErrSkyFillNilTex) {
		t.Fatalf("err = %v want ErrSkyFillNilTex", err)
	}
}

func TestFillSkyPolygon_FewVerts(t *testing.T) {
	fb, _ := NewFrameBuffer(8, 8)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}}
	if err := FillSkyPolygon(fb, tex, verts, 0); !errors.Is(err, ErrSkyFillFewVerts) {
		t.Fatalf("err = %v want ErrSkyFillFewVerts", err)
	}
}

func TestFillSkyPolygon_ManyVerts(t *testing.T) {
	fb, _ := NewFrameBuffer(8, 8)
	tex := makeDualLayerSky(t)
	verts := make([]TexturedVertex, MaxPolyVerts+1)
	if err := FillSkyPolygon(fb, tex, verts, 0); !errors.Is(err, ErrSkyFillManyVerts) {
		t.Fatalf("err = %v want ErrSkyFillManyVerts", err)
	}
}

func TestFillSkyPolygon_NotDoubleWide(t *testing.T) {
	fb, _ := NewFrameBuffer(8, 8)
	tex := &Pic{Width: 1, Height: 8, Pixels: make([]byte, 8)}
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	if err := FillSkyPolygon(fb, tex, verts, 0); !errors.Is(err, ErrSkyFillNotDoubleWide) {
		t.Fatalf("err = %v want ErrSkyFillNotDoubleWide", err)
	}
}

func TestFillSkyPolygon_BadShape(t *testing.T) {
	fb, _ := NewFrameBuffer(8, 8)
	tex := &Pic{Width: 4, Height: 4, Pixels: make([]byte, 7)}
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	if err := FillSkyPolygon(fb, tex, verts, 0); !errors.Is(err, ErrPicShape) {
		t.Fatalf("err = %v want ErrPicShape", err)
	}
}

// --- composite happy paths ------------------------------------------

// At time=0, FRONT col 0 is transparent so the BACK marker must win
// at the (0,0) pixel; FRONT col 1 is opaque so the FRONT marker wins
// at pixel (1,0).
func TestFillSkyPolygon_FrontTransparentRevealsBack(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, 0, 0, 0},
		{4, 0, 4, 0},
		{4, 4, 4, 4},
		{0, 4, 0, 4},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
	// Pixel (0,0): u=0.5 -> FRONT col 0 (transparent) -> BACK = 0xBB.
	if fb.Pixels[0] != 0xBB {
		t.Fatalf("(0,0) = %#x want 0xBB (back)", fb.Pixels[0])
	}
	// Pixel (1,0): u=1.5 -> FRONT col 1 (opaque) -> FRONT = 0xAA.
	if fb.Pixels[1] != 0xAA {
		t.Fatalf("(1,0) = %#x want 0xAA (front)", fb.Pixels[1])
	}
}

// At a later time the front layer scrolls 2x the back, so a pixel that
// was BACK at t=0 becomes FRONT one scroll-period later (and vice
// versa). This is the core anti-static-image guarantee.
func TestFillSkyPolygon_TimeAnimates(t *testing.T) {
	fbA, _ := NewFrameBuffer(8, 4)
	fbB, _ := NewFrameBuffer(8, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, 0, 0, 0},
		{8, 0, 8, 0},
		{8, 4, 8, 4},
		{0, 4, 0, 4},
	}
	if err := FillSkyPolygon(fbA, tex, verts, 0); err != nil {
		t.Fatalf("t=0 fill: %v", err)
	}
	// At t = 1/(SkyTimeScale*2) the front layer has moved ONE texel,
	// which flips even/odd cells along the row.
	dt := float32(1.0 / (SkyTimeScale * 2))
	if err := FillSkyPolygon(fbB, tex, verts, dt); err != nil {
		t.Fatalf("dt fill: %v", err)
	}
	differs := false
	for i := range fbA.Pixels {
		if fbA.Pixels[i] != fbB.Pixels[i] {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatalf("sky did not animate between t=0 and t=%g", dt)
	}
}

// Negative-U scroll wrap: at t=0 the formula reduces to plain UV, but
// we want to make sure a UV that goes past one tile end wraps not
// crashes. Set a UV span that exceeds the half-width.
func TestFillSkyPolygon_WrapsLargeU(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, 0, 1000, 0},
		{4, 0, 1004, 0},
		{4, 4, 1004, 4},
		{0, 4, 1000, 4},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
	// All output should still be valid: either FRONT (0xAA) or BACK
	// (0xBB) — never garbage.
	for _, b := range fb.Pixels {
		if b != 0xAA && b != 0xBB {
			t.Fatalf("wrap fill emitted invalid pixel %#x", b)
		}
	}
}

// Poly off the top of the framebuffer is a clean no-op.
func TestFillSkyPolygon_AboveFB(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, -10, 0, 0},
		{4, -10, 4, 0},
		{4, -5, 4, 4},
		{0, -5, 0, 4},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("above-FB poly wrote pixels")
		}
	}
}

// Poly off the bottom (yStart >= fb.Height clamps yEnd; then
// yStart >= yEnd).
func TestFillSkyPolygon_BelowFB(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, 100, 0, 0},
		{4, 100, 4, 0},
		{4, 110, 4, 4},
		{0, 110, 0, 4},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("below-FB poly wrote pixels")
		}
	}
}

// Cover the x0 < 0 clip path (polygon crosses left edge).
func TestFillSkyPolygon_ClipsLeft(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{-2, 0, 0, 0},
		{4, 0, 4, 0},
		{4, 4, 4, 4},
		{-2, 4, 0, 4},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
}

// Cover the x1 >= fb.Width clip path (polygon crosses right edge).
func TestFillSkyPolygon_ClipsRight(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, 0, 0, 0},
		{10, 0, 10, 0},
		{10, 4, 10, 4},
		{0, 4, 0, 4},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
}

// First-vertex-is-bottom: covers the `v.Y < yMin` branch.
func TestFillSkyPolygon_FirstVertIsBottom(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{2, 4, 2, 4},
		{0, 0, 0, 0},
		{4, 0, 4, 0},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
}

// Sliver where the per-row x clip produces x0 > x1 (poly off-right
// after clamp).
func TestFillSkyPolygon_XClipEmpty(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{4.6, 0, 0, 0},
		{4.9, 0, 1, 0},
		{4.7, 4, 0, 1},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
}

// Degenerate horizontal sliver: yStart >= yEnd after rounding.
func TestFillSkyPolygon_DegenerateBand(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeDualLayerSky(t)
	verts := []TexturedVertex{
		{0, 1.4, 0, 0},
		{4, 1.4, 4, 0},
		{2, 1.45, 2, 1},
	}
	if err := FillSkyPolygon(fb, tex, verts, 0); err != nil {
		t.Fatalf("sky fill: %v", err)
	}
}
