// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
	"testing"
)

// makeWaterTex builds a 64x64 mip where pixel (u,v) encodes both
// coordinates into one byte. The tests probe sample positions by
// inverting this encoding.
func makeWaterTex(t *testing.T, w, h int) *Pic {
	t.Helper()
	pixels := make([]byte, w*h)
	for v := 0; v < h; v++ {
		for u := 0; u < w; u++ {
			pixels[v*w+u] = byte((u + v*16) & 0xFF)
		}
	}
	return &Pic{Width: w, Height: h, Pixels: pixels}
}

// --- error-path coverage ---------------------------------------------

func TestFillTurbulentPolygon_NilFB(t *testing.T) {
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	err := FillTurbulentPolygon(nil, tex, nil, 0, verts, 0)
	if !errors.Is(err, ErrTurbFillNilFB) {
		t.Fatalf("nil fb err = %v want ErrTurbFillNilFB", err)
	}
}

func TestFillTurbulentPolygon_NilTex(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	err := FillTurbulentPolygon(fb, nil, nil, 0, verts, 0)
	if !errors.Is(err, ErrTurbFillNilTex) {
		t.Fatalf("nil tex err = %v want ErrTurbFillNilTex", err)
	}
}

func TestFillTurbulentPolygon_FewVerts(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}}
	err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0)
	if !errors.Is(err, ErrTurbFillFewVerts) {
		t.Fatalf("few verts err = %v want ErrTurbFillFewVerts", err)
	}
}

func TestFillTurbulentPolygon_ManyVerts(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	tex := makeWaterTex(t, 64, 64)
	verts := make([]TexturedVertex, MaxPolyVerts+1)
	err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0)
	if !errors.Is(err, ErrTurbFillManyVerts) {
		t.Fatalf("many verts err = %v want ErrTurbFillManyVerts", err)
	}
}

func TestFillTurbulentPolygon_BadShape(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	tex := &Pic{Width: 8, Height: 8, Pixels: make([]byte, 7)} // wrong length
	verts := []TexturedVertex{{0, 0, 0, 0}, {1, 0, 1, 0}, {0, 1, 0, 1}}
	err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("bad shape err = %v want ErrPicShape", err)
	}
}

// --- happy paths -----------------------------------------------------

// At time=0, the warp at (u=0, v=0) is sin_lut(0)=0 + sin_lut(0)=0, so
// the sampled texel is exactly tex(0,0) = 0.
func TestFillTurbulentPolygon_TimeZeroAtOrigin(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	// A quad covering the framebuffer with UV exactly matching x,y.
	verts := []TexturedVertex{
		{0, 0, 0, 0},
		{4, 0, 4, 0},
		{4, 4, 4, 4},
		{0, 4, 0, 4},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
	// At pixel (0, 0), pixel center (0.5, 0.5) -> u'=0.5+sin_lut(0.5/16)
	// ~ 0.5+0 = 0.5; floor = 0. Same for v. So texel = tex(0, 0) = 0.
	if fb.Pixels[0] != 0 {
		t.Fatalf("(0,0) = %d want 0", fb.Pixels[0])
	}
}

// Verify the warp visibly shifts the sample between two adjacent
// times. Pick a (u, v) point where the LUT phase change is large
// enough to step the integer texel coordinate by at least 1.
func TestFillTurbulentPolygon_TimeShiftsSample(t *testing.T) {
	tex := makeWaterTex(t, 64, 64)
	// Hand-rolled single-pixel evaluation -- just call WarpUV.
	u0, v0 := WarpUV(10, 10, 0)
	u1, v1 := WarpUV(10, 10, 1.0)
	if u0 == u1 && v0 == v1 {
		t.Fatalf("warp did not shift in time: (%g,%g) == (%g,%g)", u0, v0, u1, v1)
	}
	_ = tex
}

// Verify WarpUV matches the LUT exactly for a known phase. At t=0 and
// u=0, sin_lut at (v/16 mod 256) -- for v=64 that's index 4 -> phase
// 2*pi*4/256 ~= 0.098 rad -> ~ TurbScale * 0.098 ~ 0.784.
func TestWarpUV_MatchesLUT(t *testing.T) {
	got, _ := WarpUV(0, 64, 0)
	want := turbSinTable[4] // since u=0, u' = 0 + sin_lut(64/16) = sin_lut(4)
	if math.Abs(float64(got-want)) > 1e-5 {
		t.Fatalf("WarpUV(0,64,0).u = %g want %g", got, want)
	}
}

// LUT has peak amplitude == TurbScale.
func TestTurbSinTable_Peak(t *testing.T) {
	tbl := TurbSinTable()
	if len(tbl) != TurbSinTableSize {
		t.Fatalf("len = %d want %d", len(tbl), TurbSinTableSize)
	}
	max := float32(0)
	for _, v := range tbl {
		if v > max {
			max = v
		}
	}
	if math.Abs(float64(max-TurbScale)) > 0.05 {
		t.Fatalf("LUT peak = %g want %g", max, float32(TurbScale))
	}
}

// Defensive-copy contract: mutating the returned table does not affect
// internal state.
func TestTurbSinTable_DefensiveCopy(t *testing.T) {
	tbl := TurbSinTable()
	tbl[0] = 999
	if turbSinTable[0] == 999 {
		t.Fatalf("internal LUT was mutated through returned slice")
	}
}

// Polygon entirely above the framebuffer is a clean no-op.
func TestFillTurbulentPolygon_AboveFB(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{
		{0, -10, 0, 0},
		{4, -10, 4, 0},
		{4, -5, 4, 4},
		{0, -5, 0, 4},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("no-op poly wrote pixels")
		}
	}
}

// Polygon entirely below the framebuffer is also a clean no-op (covers
// the yStart >= yEnd branch from the other side).
func TestFillTurbulentPolygon_BelowFB(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{
		{0, 100, 0, 0},
		{4, 100, 4, 0},
		{4, 110, 4, 4},
		{0, 110, 0, 4},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("no-op poly wrote pixels")
		}
	}
}

// Polygon that crosses the left edge should clip cleanly (covers x0<0
// path).
func TestFillTurbulentPolygon_ClipsLeft(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{
		{-2, 0, 0, 0},
		{4, 0, 4, 0},
		{4, 4, 4, 4},
		{-2, 4, 0, 4},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
	// At least one pixel in the framebuffer should be written.
	nonZero := false
	for _, b := range fb.Pixels {
		if b != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatalf("left-clipped poly produced no pixels")
	}
}

// Polygon that crosses the right edge should clip cleanly.
func TestFillTurbulentPolygon_ClipsRight(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{
		{0, 0, 0, 0},
		{10, 0, 10, 0},
		{10, 4, 10, 4},
		{0, 4, 0, 4},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
}

// With a non-nil colormap the texel passes through cm.LightIndex.
func TestFillTurbulentPolygon_WithColormap(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	// Build a colormap that maps EVERY input to a single constant
	// value -- makes the test independent of palette layout.
	const sentinel byte = 0x42
	cmBytes := make([]byte, ColorMapLumpSize)
	for i := range cmBytes {
		cmBytes[i] = sentinel
	}
	cm, err := LoadColorMap(cmBytes)
	if err != nil {
		t.Fatalf("LoadColorMap: %v", err)
	}
	verts := []TexturedVertex{
		{0, 0, 0, 0},
		{4, 0, 4, 0},
		{4, 4, 4, 4},
		{0, 4, 0, 4},
	}
	if err := FillTurbulentPolygon(fb, tex, cm, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 && b != sentinel {
			t.Fatalf("colormap not applied: pixel = %#x", b)
		}
	}
}

// positiveMod degenerate cases.
func TestPositiveMod_NonPositiveN(t *testing.T) {
	if got := positiveMod(5, 0); got != 0 {
		t.Fatalf("n=0 -> %d want 0", got)
	}
	if got := positiveMod(5, -3); got != 0 {
		t.Fatalf("n=-3 -> %d want 0", got)
	}
}

func TestPositiveMod_NegativeA(t *testing.T) {
	if got := positiveMod(-1, 64); got != 63 {
		t.Fatalf("-1 mod 64 = %d want 63", got)
	}
	if got := positiveMod(-65, 64); got != 63 {
		t.Fatalf("-65 mod 64 = %d want 63", got)
	}
}

// First-vertex-is-bottom: covers the `v.Y < yMin` branch (when the
// first vertex starts as the max-Y and a later vertex undercuts it).
func TestFillTurbulentPolygon_FirstVertIsBottom(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	// verts[0] is the largest Y; verts[1].Y < verts[0].Y must update yMin.
	verts := []TexturedVertex{
		{2, 4, 2, 4},
		{0, 0, 0, 0},
		{4, 0, 4, 0},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
}

// Sliver where the per-row x clip produces x0 > x1 (e.g. polygon that
// only touches a single sub-pixel column, off the FB width).
func TestFillTurbulentPolygon_XClipEmpty(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	// Triangle whose two left vertices are at x=4.6 and x=4.9 -- their
	// edge crossings on row y=0 will produce xLeft~4.6/xRight~4.9; ceil
	// -> 5, floor -> 4 ; both > fb.Width-1=3 ; after clamp x0=5
	// becomes... well, the clamp only fires for x1, so x0 stays 5 and
	// x0 > x1 fires.
	verts := []TexturedVertex{
		{4.6, 0, 0, 0},
		{4.9, 0, 1, 0},
		{4.7, 4, 0, 1},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
}

// Sub-pixel scanline coverage (yMin == yMax + tiny epsilon would
// otherwise be skipped). Cover the slim wedge where yStart == yEnd.
func TestFillTurbulentPolygon_DegenerateBand(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	tex := makeWaterTex(t, 64, 64)
	verts := []TexturedVertex{
		{0, 1.4, 0, 0},
		{4, 1.4, 4, 0},
		{2, 1.45, 2, 1},
	}
	if err := FillTurbulentPolygon(fb, tex, nil, 0, verts, 0); err != nil {
		t.Fatalf("turb fill: %v", err)
	}
}
