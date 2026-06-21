// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

// ----- TestPatternBars --------------------------------------------

func TestPatternBars_NilFB(t *testing.T) {
	err := TestPatternBars(nil, 8, 0)
	if !errors.Is(err, ErrTestPatNilFB) {
		t.Fatalf("err = %v want ErrTestPatNilFB", err)
	}
}

func TestPatternBars_Happy(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 4)
	if err := TestPatternBars(fb, 4, 0x10); err != nil {
		t.Fatalf("TestPatternBars: %v", err)
	}
	// 4 bars in 32 px -> bar width 8. Bar 0 = 0x10, bar 1 = 0x11, ...
	if fb.Pixels[0] != 0x10 {
		t.Fatalf("col 0 = %#x want 0x10", fb.Pixels[0])
	}
	if fb.Pixels[8] != 0x11 {
		t.Fatalf("col 8 = %#x want 0x11", fb.Pixels[8])
	}
	if fb.Pixels[16] != 0x12 {
		t.Fatalf("col 16 = %#x want 0x12", fb.Pixels[16])
	}
	if fb.Pixels[24] != 0x13 {
		t.Fatalf("col 24 = %#x want 0x13", fb.Pixels[24])
	}
}

func TestPatternBars_ClampsLow(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 4)
	if err := TestPatternBars(fb, 0, 0x20); err != nil {
		t.Fatalf("TestPatternBars: %v", err)
	}
	// 0 -> clamps to 1 -> single bar = 0x20 everywhere.
	for _, b := range fb.Pixels {
		if b != 0x20 {
			t.Fatalf("clamp-low pixel = %#x want 0x20", b)
		}
	}
}

func TestPatternBars_ClampsHigh(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 4)
	// 300 bars in 32 px -> clamps to 256, but bar width = 32/256 = 0
	// (clamped up to 1) -> bar index = col / 1 = col (capped at 255).
	if err := TestPatternBars(fb, 300, 0); err != nil {
		t.Fatalf("TestPatternBars: %v", err)
	}
	if fb.Pixels[0] != 0 {
		t.Fatalf("col 0 = %#x", fb.Pixels[0])
	}
}

func TestPatternBars_PitchHonored(t *testing.T) {
	fb, _ := NewFrameBufferAligned(32, 4, 64)
	if err := TestPatternBars(fb, 4, 0x10); err != nil {
		t.Fatalf("TestPatternBars: %v", err)
	}
	// Pitch is 64; the last 32 bytes of each row are NOT touched.
	for y := 0; y < 4; y++ {
		for x := 32; x < 64; x++ {
			if fb.Pixels[y*64+x] != 0 {
				t.Fatalf("pitch-byte (%d,%d) = %#x want 0", x, y, fb.Pixels[y*64+x])
			}
		}
	}
}

func TestPatternBars_BarOverflow(t *testing.T) {
	// Width 10, numBars 3 -> barW = 3, last bar (index 2) covers x=6..9
	// but if x=9 maps to bar=3 it would overflow; the clamp keeps it at 2.
	fb, _ := NewFrameBuffer(10, 1)
	if err := TestPatternBars(fb, 3, 0); err != nil {
		t.Fatalf("TestPatternBars: %v", err)
	}
	if fb.Pixels[9] != 2 {
		t.Fatalf("col 9 = %#x want 2 (clamped to last bar)", fb.Pixels[9])
	}
}

// ----- TestPatternGradient ----------------------------------------

func TestPatternGradient_NilFB(t *testing.T) {
	err := TestPatternGradient(nil, 0, 64)
	if !errors.Is(err, ErrTestPatNilFB) {
		t.Fatalf("err = %v want ErrTestPatNilFB", err)
	}
}

func TestPatternGradient_Happy(t *testing.T) {
	fb, _ := NewFrameBuffer(64, 1)
	if err := TestPatternGradient(fb, 0, 64); err != nil {
		t.Fatalf("TestPatternGradient: %v", err)
	}
	// At x=0, off = 0; at x=32, off = 32; at x=63, off = 63.
	if fb.Pixels[0] != 0 {
		t.Fatalf("col 0 = %#x", fb.Pixels[0])
	}
	if fb.Pixels[32] != 32 {
		t.Fatalf("col 32 = %#x", fb.Pixels[32])
	}
}

func TestPatternGradient_RangeClamp(t *testing.T) {
	fb, _ := NewFrameBuffer(10, 1)
	if err := TestPatternGradient(fb, 5, 0); err != nil {
		t.Fatalf("TestPatternGradient: %v", err)
	}
	// indexRange 0 clamps to 1; off always 0; every pixel = startIdx
	for _, b := range fb.Pixels {
		if b != 5 {
			t.Fatalf("zero-range pixel = %#x want 5", b)
		}
	}
}

// ----- TestPatternCheckerboard ------------------------------------

func TestPatternCheckerboard_NilFB(t *testing.T) {
	err := TestPatternCheckerboard(nil, 4, 0, 15)
	if !errors.Is(err, ErrTestPatNilFB) {
		t.Fatalf("err = %v want ErrTestPatNilFB", err)
	}
}

func TestPatternCheckerboard_Happy(t *testing.T) {
	fb, _ := NewFrameBuffer(16, 16)
	if err := TestPatternCheckerboard(fb, 8, 0, 15); err != nil {
		t.Fatalf("TestPatternCheckerboard: %v", err)
	}
	// Top-left tile (0,0): tileX+tileY even -> darkIdx 0
	if fb.Pixels[0] != 0 {
		t.Fatalf("(0,0) = %#x want 0", fb.Pixels[0])
	}
	// Top-right tile (8,0): tileX+tileY = 1 odd -> lightIdx 15
	if fb.Pixels[8] != 15 {
		t.Fatalf("(8,0) = %#x want 15", fb.Pixels[8])
	}
}

func TestPatternCheckerboard_TileSizeClamp(t *testing.T) {
	fb, _ := NewFrameBuffer(4, 4)
	if err := TestPatternCheckerboard(fb, 0, 1, 2); err != nil {
		t.Fatalf("TestPatternCheckerboard: %v", err)
	}
	// tileSize clamps to 1; every cell is its own tile; (0,0) is dark,
	// (1,0) light, alternating.
	if fb.Pixels[0] != 1 {
		t.Fatalf("(0,0) = %#x want 1", fb.Pixels[0])
	}
	if fb.Pixels[1] != 2 {
		t.Fatalf("(1,0) = %#x want 2", fb.Pixels[1])
	}
}
