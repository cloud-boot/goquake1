// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// ErrTestPatNilFB is returned by every test-pattern helper if fb is nil.
var ErrTestPatNilFB = errors.New("render: nil framebuffer in test pattern")

// TestPatternBars paints `numBars` equal-width vertical color bars
// across the framebuffer, walking palette indices from `startIdx` to
// `startIdx + numBars - 1` (modulo 256). Useful for visually
// verifying the palette expand path: a backend that renders the
// expected bar widths + colors is wired correctly.
//
// numBars <= 0 or numBars > 256 silently clamps to [1, 256].
//
// tyrquake: a debugging helper analogue to vid_test in
// vid_sdl.c / vid_win.c (not present in vanilla tyrquake; a Go
// port convenience).
func TestPatternBars(fb *FrameBuffer, numBars int, startIdx byte) error {
	if fb == nil {
		return ErrTestPatNilFB
	}
	if numBars < 1 {
		numBars = 1
	}
	if numBars > 256 {
		numBars = 256
	}
	barW := fb.Width / numBars
	if barW < 1 {
		barW = 1
	}
	for y := 0; y < fb.Height; y++ {
		row := y * fb.Pitch
		for x := 0; x < fb.Width; x++ {
			bar := x / barW
			if bar >= numBars {
				bar = numBars - 1
			}
			fb.Pixels[row+x] = startIdx + byte(bar)
		}
	}
	return nil
}

// TestPatternGradient paints a horizontal gradient: column x maps to
// palette index startIdx + (x * range / Width). Useful for spotting
// quantization artifacts in palette-expand backends.
func TestPatternGradient(fb *FrameBuffer, startIdx byte, indexRange int) error {
	if fb == nil {
		return ErrTestPatNilFB
	}
	if indexRange < 1 {
		indexRange = 1
	}
	for y := 0; y < fb.Height; y++ {
		row := y * fb.Pitch
		for x := 0; x < fb.Width; x++ {
			off := (x * indexRange) / fb.Width
			fb.Pixels[row+x] = startIdx + byte(off)
		}
	}
	return nil
}

// TestPatternCheckerboard paints a black-vs-white (paletteIdx 0 vs
// paletteIdx 15) NxN checkerboard. tileSize is the per-tile pixel
// edge. Useful for verifying pitch handling and pixel addressing.
func TestPatternCheckerboard(fb *FrameBuffer, tileSize int, darkIdx, lightIdx byte) error {
	if fb == nil {
		return ErrTestPatNilFB
	}
	if tileSize < 1 {
		tileSize = 1
	}
	for y := 0; y < fb.Height; y++ {
		row := y * fb.Pitch
		tileY := y / tileSize
		for x := 0; x < fb.Width; x++ {
			tileX := x / tileSize
			if (tileX+tileY)&1 == 0 {
				fb.Pixels[row+x] = darkIdx
			} else {
				fb.Pixels[row+x] = lightIdx
			}
		}
	}
	return nil
}
