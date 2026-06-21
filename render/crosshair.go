// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// CrosshairStyle selects the on-screen crosshair shape.
// tyrquake: scr_crosshair cvar (0 = none, 1 = '+', 2+ = pic).
type CrosshairStyle int

const (
	CrosshairNone  CrosshairStyle = 0
	CrosshairPlus  CrosshairStyle = 1 // simple 3px '+' centred on the viewport
	CrosshairBox   CrosshairStyle = 2 // hollow 5px outline box
)

var ErrCrosshairNilFB = errors.New("render: nil framebuffer in DrawCrosshair")

// DrawCrosshair paints a one-color crosshair at (centerX, centerY)
// in the framebuffer. CrosshairNone is a no-op. Out-of-bounds pixels
// are silently clipped by the underlying SetPixel path.
//
// tyrquake: SCR_DrawCrosshair (called from SCR_UpdateScreen after
// the world view).
func DrawCrosshair(fb *FrameBuffer, centerX, centerY int, color byte, style CrosshairStyle) error {
	if fb == nil {
		return ErrCrosshairNilFB
	}
	switch style {
	case CrosshairNone:
		return nil
	case CrosshairPlus:
		// 3-pixel plus: center + 4 neighbours.
		setIfInBounds(fb, centerX, centerY, color)
		setIfInBounds(fb, centerX-1, centerY, color)
		setIfInBounds(fb, centerX+1, centerY, color)
		setIfInBounds(fb, centerX, centerY-1, color)
		setIfInBounds(fb, centerX, centerY+1, color)
	case CrosshairBox:
		// 5x5 hollow box, corners only.
		setIfInBounds(fb, centerX-2, centerY-2, color)
		setIfInBounds(fb, centerX+2, centerY-2, color)
		setIfInBounds(fb, centerX-2, centerY+2, color)
		setIfInBounds(fb, centerX+2, centerY+2, color)
		// Side midpoints.
		setIfInBounds(fb, centerX-2, centerY, color)
		setIfInBounds(fb, centerX+2, centerY, color)
		setIfInBounds(fb, centerX, centerY-2, color)
		setIfInBounds(fb, centerX, centerY+2, color)
	}
	return nil
}

// setIfInBounds is the silent-clip pixel write. SetPixel returns an
// error on out-of-bounds; the crosshair routine doesn't care -- it
// just skips off-screen pixels.
func setIfInBounds(fb *FrameBuffer, x, y int, c byte) {
	if x < 0 || x >= fb.Width || y < 0 || y >= fb.Height {
		return
	}
	fb.Pixels[y*fb.Pitch+x] = c
}
