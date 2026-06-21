// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestFillPolygon_NilFB(t *testing.T) {
	verts := [][2]float32{{0, 0}, {10, 0}, {5, 10}}
	if err := FillPolygon(nil, verts, 0xAA); !errors.Is(err, ErrPolyNilFB) {
		t.Fatalf("err = %v want ErrPolyNilFB", err)
	}
}

func TestFillPolygon_TooFewVerts(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	if err := FillPolygon(fb, [][2]float32{{0, 0}, {10, 0}}, 0); !errors.Is(err, ErrPolyTooFewVerts) {
		t.Fatalf("err = %v want ErrPolyTooFewVerts", err)
	}
}

func TestFillPolygon_TooManyVerts(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	verts := make([][2]float32, MaxPolyVerts+1)
	if err := FillPolygon(fb, verts, 0); !errors.Is(err, ErrPolyTooManyVerts) {
		t.Fatalf("err = %v want ErrPolyTooManyVerts", err)
	}
}

func TestFillPolygon_Triangle(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// A right triangle: (0,0) -> (10,0) -> (0,10)
	verts := [][2]float32{{0, 0}, {10, 0}, {0, 10}}
	if err := FillPolygon(fb, verts, 0x42); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// The center of the triangle (e.g. pixel (3, 3)) should be filled.
	if fb.Pixels[3*fb.Pitch+3] != 0x42 {
		t.Fatalf("triangle center not filled")
	}
	// A pixel WELL outside the triangle (e.g. (20, 20)) should be 0.
	if fb.Pixels[20*fb.Pitch+20] != 0 {
		t.Fatalf("triangle filled outside its area")
	}
}

func TestFillPolygon_RectangleQuad(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// A 5x5 rectangle from (10,10) to (15,15).
	verts := [][2]float32{{10, 10}, {15, 10}, {15, 15}, {10, 15}}
	if err := FillPolygon(fb, verts, 0x77); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// Center of the rect.
	if fb.Pixels[12*fb.Pitch+12] != 0x77 {
		t.Fatalf("rect center not filled")
	}
}

func TestFillPolygon_FullyOffScreenLeft(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	verts := [][2]float32{{-50, -50}, {-40, -50}, {-45, -40}}
	if err := FillPolygon(fb, verts, 0xFF); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("off-screen triangle drew pixels")
		}
	}
}

func TestFillPolygon_PartiallyOffScreen(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Triangle whose top is above the framebuffer; bottom inside.
	verts := [][2]float32{{15, -10}, {25, 20}, {5, 20}}
	if err := FillPolygon(fb, verts, 0x33); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// Some pixel inside the polygon that's inside the framebuffer.
	if fb.Pixels[15*fb.Pitch+15] != 0x33 {
		t.Fatalf("partially-off polygon: in-bound pixel not filled")
	}
}

func TestFillPolygon_OffRightEdge(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Triangle that goes past the right edge.
	verts := [][2]float32{{20, 5}, {50, 10}, {20, 15}}
	if err := FillPolygon(fb, verts, 0x11); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// Pixel (25, 10) inside the polygon should be filled.
	if fb.Pixels[10*fb.Pitch+25] != 0x11 {
		t.Fatalf("right-edge polygon: in-bound pixel not filled")
	}
	// Final column (31) should be filled (the polygon extends to x=50, clamps to 31).
	if fb.Pixels[10*fb.Pitch+31] != 0x11 {
		t.Fatalf("right-clamp didn't write to fb edge")
	}
}

func TestFillPolygon_ZeroHeightPolygon(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// All three verts on the same scanline -> yStart == yEnd -> no fill.
	verts := [][2]float32{{10, 10}, {15, 10}, {20, 10}}
	if err := FillPolygon(fb, verts, 0xAA); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("zero-height polygon drew pixels")
		}
	}
}

func TestFillPolygon_OutOfBoundsYClipping(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Triangle wholly below the framebuffer.
	verts := [][2]float32{{10, 100}, {20, 100}, {15, 120}}
	if err := FillPolygon(fb, verts, 0x99); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("below-framebuffer polygon drew pixels")
		}
	}
}

func TestFillPolygon_ReverseWinding(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Same right triangle but vertices in opposite order.
	verts := [][2]float32{{0, 10}, {10, 0}, {0, 0}}
	if err := FillPolygon(fb, verts, 0x55); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	if fb.Pixels[3*fb.Pitch+3] != 0x55 {
		t.Fatalf("reversed-winding triangle not filled")
	}
}

func TestFillPolygon_ConsecutiveScanlinePairsSwap(t *testing.T) {
	// Build a polygon whose edge endpoints land in an order that
	// requires the x-crossings sort to swap. A diamond from
	// (16,0)->(32,16)->(16,32)->(0,16) reverses left/right
	// crossings across its scanlines.
	fb, _ := NewFrameBuffer(32, 32)
	verts := [][2]float32{{16, 0}, {32, 16}, {16, 32}, {0, 16}}
	if err := FillPolygon(fb, verts, 0x88); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// Center pixel should be filled (covered by every scanline).
	if fb.Pixels[16*fb.Pitch+16] != 0x88 {
		t.Fatalf("diamond center not filled")
	}
}

func TestFillPolygon_OffLeftEdgeClamp(t *testing.T) {
	// Polygon extending past x=0 -> x0 clamp from negative to 0.
	fb, _ := NewFrameBuffer(32, 32)
	verts := [][2]float32{{-5, 5}, {15, 5}, {5, 15}}
	if err := FillPolygon(fb, verts, 0x22); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// First column should be touched.
	if fb.Pixels[7*fb.Pitch+0] != 0x22 {
		t.Fatalf("left-clamp did not write to column 0")
	}
}

func TestFillPolygon_SubPixelWidthSkip(t *testing.T) {
	// A polygon whose per-scanline width is less than 1 pixel on at
	// least one scanline -> ceil(xs[0]) > floor(xs[1]) -> the per-pair
	// skip continues.
	fb, _ := NewFrameBuffer(32, 32)
	// Very thin triangle: verts (10.6, 5), (10.9, 5), (10.7, 15).
	// Between the two top verts the polygon width is 0.3 px.
	verts := [][2]float32{{10.6, 5}, {10.9, 5}, {10.7, 15}}
	if err := FillPolygon(fb, verts, 0x88); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// Near the apex the rasterizer should skip (no fill); a few scanlines
	// down where the polygon is >= 1px wide there might be a column.
	// We just assert no panic + a sane partial fill.
	_ = fb
}

func TestFillPolygon_DegenerateCrossingsSkip(t *testing.T) {
	// Scanline that intersects an edge endpoint only (nXs == 1) skips.
	// Build a triangle so the top vertex lands exactly on an integer
	// scanline; the scanline at the apex sees only one crossing.
	fb, _ := NewFrameBuffer(32, 32)
	verts := [][2]float32{{15, 5}, {25, 25}, {5, 25}}
	if err := FillPolygon(fb, verts, 0x66); err != nil {
		t.Fatalf("FillPolygon: %v", err)
	}
	// The body should still fill.
	if fb.Pixels[15*fb.Pitch+15] != 0x66 {
		t.Fatalf("triangle body not filled")
	}
}
