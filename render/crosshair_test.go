// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestDrawCrosshair_NilFB(t *testing.T) {
	err := DrawCrosshair(nil, 10, 10, 0xFF, CrosshairPlus)
	if !errors.Is(err, ErrCrosshairNilFB) {
		t.Fatalf("err = %v want ErrCrosshairNilFB", err)
	}
}

func TestDrawCrosshair_None(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	if err := DrawCrosshair(fb, 16, 16, 0xAA, CrosshairNone); err != nil {
		t.Fatalf("DrawCrosshair: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("CrosshairNone drew pixels")
		}
	}
}

func TestDrawCrosshair_Plus(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	if err := DrawCrosshair(fb, 16, 16, 0xAA, CrosshairPlus); err != nil {
		t.Fatalf("DrawCrosshair: %v", err)
	}
	// Center + 4 neighbours = 5 pixels of 0xAA
	count := 0
	for _, b := range fb.Pixels {
		if b == 0xAA {
			count++
		}
	}
	if count != 5 {
		t.Fatalf("CrosshairPlus pixels = %d want 5", count)
	}
	// Center pixel set
	if fb.Pixels[16*fb.Pitch+16] != 0xAA {
		t.Fatalf("center not set")
	}
	// Left neighbour set
	if fb.Pixels[16*fb.Pitch+15] != 0xAA {
		t.Fatalf("left not set")
	}
}

func TestDrawCrosshair_Box(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	if err := DrawCrosshair(fb, 16, 16, 0xBB, CrosshairBox); err != nil {
		t.Fatalf("DrawCrosshair: %v", err)
	}
	// 4 corners + 4 side midpoints = 8 pixels of 0xBB
	count := 0
	for _, b := range fb.Pixels {
		if b == 0xBB {
			count++
		}
	}
	if count != 8 {
		t.Fatalf("CrosshairBox pixels = %d want 8", count)
	}
}

func TestDrawCrosshair_UnknownStyle(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Style 99 falls through; no-op
	if err := DrawCrosshair(fb, 16, 16, 0xCC, CrosshairStyle(99)); err != nil {
		t.Fatalf("DrawCrosshair: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("unknown style drew pixels")
		}
	}
}

func TestDrawCrosshair_NearEdge(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Crosshair at (0, 0) -> some arms cut by out-of-bounds clip.
	if err := DrawCrosshair(fb, 0, 0, 0xDD, CrosshairPlus); err != nil {
		t.Fatalf("DrawCrosshair: %v", err)
	}
	// At (0,0): center set + (1,0) + (0,1) set; (-1,0) and (0,-1) clipped.
	// So 3 pixels of 0xDD
	count := 0
	for _, b := range fb.Pixels {
		if b == 0xDD {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("edge-clipped CrosshairPlus pixels = %d want 3", count)
	}
}

func TestDrawCrosshair_FullyOutside(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 32)
	// Crosshair centered way past framebuffer: every pixel clipped.
	if err := DrawCrosshair(fb, 1000, 1000, 0xEE, CrosshairPlus); err != nil {
		t.Fatalf("DrawCrosshair: %v", err)
	}
	for _, b := range fb.Pixels {
		if b == 0xEE {
			t.Fatalf("fully-outside crosshair drew pixels")
		}
	}
}
