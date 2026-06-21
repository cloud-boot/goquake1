// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestRenderTestScene_NilFB(t *testing.T) {
	chars := makeCharsSheet(t)
	err := RenderTestScene(nil, chars, DefaultTestSceneOpts())
	if !errors.Is(err, ErrTestSceneNilFB) {
		t.Fatalf("err = %v want ErrTestSceneNilFB", err)
	}
}

func TestRenderTestScene_NilChars(t *testing.T) {
	fb, _ := NewFrameBuffer(320, 200)
	err := RenderTestScene(fb, nil, DefaultTestSceneOpts())
	if !errors.Is(err, ErrTestSceneNilChars) {
		t.Fatalf("err = %v want ErrTestSceneNilChars", err)
	}
}

func TestRenderTestScene_Happy(t *testing.T) {
	fb, _ := NewFrameBuffer(320, 200)
	chars := makeCharsSheet(t)
	if err := RenderTestScene(fb, chars, DefaultTestSceneOpts()); err != nil {
		t.Fatalf("RenderTestScene: %v", err)
	}
	// Top-left pixel should be the first colorbar color: 16
	if fb.Pixels[0] != 16 {
		t.Fatalf("first bar pixel = %#x want 16", fb.Pixels[0])
	}
	// Centre pixel should be the crosshair color
	cx, cy := fb.Width/2, fb.Height/2
	if fb.Pixels[cy*fb.Pitch+cx] != 0xFF {
		t.Fatalf("center crosshair pixel = %#x want 0xFF", fb.Pixels[cy*fb.Pitch+cx])
	}
}

func TestRenderTestScene_PropagatesTestPatternError(t *testing.T) {
	// TestPatternBars validates numBars internally but never errors
	// when fb is non-nil; the propagation path is via the underlying
	// ErrTestPatNilFB check which already passes. So this test
	// instead exercises DrawCenteredString's error path with bad chars.
	fb, _ := NewFrameBuffer(80, 100)
	bad := &Pic{Width: 64, Height: 64, Pixels: make([]byte, 64*64)}
	err := RenderTestScene(fb, bad, DefaultTestSceneOpts())
	if !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape", err)
	}
}

func TestDefaultTestSceneOpts(t *testing.T) {
	opts := DefaultTestSceneOpts()
	if opts.NumBars != 16 || opts.BarsStartIdx != 16 || opts.CrosshairColor != 0xFF {
		t.Fatalf("defaults drift: %+v", opts)
	}
	if opts.BannerText == "" {
		t.Fatalf("default banner text is empty")
	}
}
