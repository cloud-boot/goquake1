// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"math"
)

// SpriteNearClip is the minimum view-space forward depth at which a
// sprite is drawn. tyrquake: the close-sprite cull inside
// R_DrawSpriteModel.
const SpriteNearClip = 16.0

var ErrSpriteNilFB = errors.New("render: nil framebuffer in DrawSpriteBillboard")

// DrawSpriteBillboard renders `pic` as a billboarded sprite at
// world-space `origin`: the pic is projected through `rd`'s view
// transform to a screen position, then DrawTransPic'd centered on
// that position. The sprite is NOT scaled with depth in this
// commit (the simplified MVP path); a perspective-scaled variant
// is a follow-up.
//
// Returns:
//
//	ErrSpriteNilFB    fb == nil
//	(silent skip)     pic == nil OR rd == nil OR origin behind camera OR off-screen
//	(propagated)      any DrawTransPic / shape error
//
// tyrquake: R_DrawSpriteModel in r_sprite.c. The vanilla path
// rotates the sprite's bounds to face the viewer + clips them
// against the view frustum; this MVP plants the sprite center at
// the screen position and lets DrawTransPic's clipping handle the
// edge cases.
func DrawSpriteBillboard(fb *FrameBuffer, rd *RefDef, pic *Pic, origin [3]float32) error {
	if fb == nil {
		return ErrSpriteNilFB
	}
	if rd == nil || pic == nil {
		return nil
	}

	view := rd.SetupView()
	vp := TransformAffine(view, origin)
	if vp[2] < SpriteNearClip {
		return nil
	}

	const deg2rad = math.Pi / 180
	tanHalfX := float32(math.Tan(float64(rd.FovX/2) * deg2rad))
	if tanHalfX <= 0 {
		return nil
	}
	halfW := float32(fb.Width) / 2
	halfH := float32(fb.Height) / 2
	scale := halfW / tanHalfX

	invZ := 1 / vp[2]
	cx := halfW + vp[0]*scale*invZ
	cy := halfH - vp[1]*scale*invZ
	// Draw centered on (cx, cy).
	x := int(cx) - pic.Width/2
	y := int(cy) - pic.Height/2

	return DrawTransPic(fb, x, y, pic)
}
