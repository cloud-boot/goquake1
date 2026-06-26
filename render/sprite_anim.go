// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"

	"github.com/go-quake1/engine/spr"
)

// ErrSpriteAnimNilSprite is returned by [SpriteFramePic] /
// [DrawSpriteAtTime] when the *spr.Sprite is nil. A separate sentinel
// from the spr-package errors keeps the render-side surface
// self-contained for callers.
var ErrSpriteAnimNilSprite = errors.New("render: nil *spr.Sprite")

// SpriteFramePic resolves a (top-level frame index, group-local time)
// pair on `sp` to a *Pic ready for DrawTransPic / DrawSpriteBillboard.
//
// The pic's pixels are COPIED out of the sprite (the renderer owns
// independent storage); the per-frame OriginX/OriginY values are not
// applied to the pic itself -- callers fold them into their billboard
// anchor when needed (DrawSpriteBillboard centres on the screen
// projection, which already matches the upstream centred-sprite
// convention for explosions; gunglow / itemglint sprites with a
// non-(0,0) origin can post-apply the shift if they need it).
//
// Returns:
//
//	ErrSpriteAnimNilSprite     -- sp == nil
//	(spr.ErrNoFrames / spr.ErrFrameOutOfRange when those apply)
//
// Group-frame intervals: see [spr.SelectFrame] -- the time argument
// here cycles through the per-group sub-frames.
func SpriteFramePic(sp *spr.Sprite, frameIdx int, time float32) (*Pic, error) {
	if sp == nil {
		return nil, ErrSpriteAnimNilSprite
	}
	ff, err := spr.Flatten(sp, frameIdx, time)
	if err != nil {
		return nil, err
	}
	pixels := make([]byte, len(ff.Pixels))
	copy(pixels, ff.Pixels)
	return &Pic{Width: ff.Width, Height: ff.Height, Pixels: pixels}, nil
}

// SpriteFramePeriod is the canonical 10 Hz frame-advance cadence for
// sprite animations (matches the upstream r_part.c's explosion frame
// stride: 0.1 s per frame for s_explod.spr's 6-frame burst).
const SpriteFramePeriod float32 = 0.1

// SpriteFrameForElapsed picks the top-level frame index for a sprite
// driven by elapsed (since-spawn) time + the canonical [SpriteFramePeriod].
// Mirrors [spr.FrameForTime] with the period bound to the engine's
// standard 10-fps sprite cadence.
//
// numFrames <= 0 returns 0 (caller is expected to nil-check the
// sprite before drawing).
func SpriteFrameForElapsed(numFrames int, elapsed float32) int {
	return spr.FrameForTime(numFrames, SpriteFramePeriod, elapsed)
}

// DrawSpriteAtTime is the one-call convenience that picks the right
// per-elapsed-time top-level frame on `sp`, builds the matching Pic,
// and billboards it at `origin` via [DrawSpriteBillboard]. It bundles
// the frame-selection + Pic-build + billboard call the temp-entity
// pool's per-tic walk would otherwise repeat at every call site.
//
// Returns:
//
//	ErrSpriteAnimNilSprite     -- sp == nil
//	ErrSpriteNilFB             -- fb == nil
//	(propagated SelectFrame / DrawSpriteBillboard errors)
//
// nil rd is a silent no-op (matches DrawSpriteBillboard's contract).
// An out-of-range frame index after the cycle is clamped to 0 (the
// degenerate "sprite has zero frames" case yields an early return).
func DrawSpriteAtTime(fb *FrameBuffer, rd *RefDef, sp *spr.Sprite, origin [3]float32, elapsed float32) error {
	if fb == nil {
		return ErrSpriteNilFB
	}
	if sp == nil {
		return ErrSpriteAnimNilSprite
	}
	frameIdx := SpriteFrameForElapsed(len(sp.Frames), elapsed)
	// Group-frames want time MODULO the group's cycle, not the raw
	// elapsed value -- but the per-elapsed picker above already
	// resolves the right top-level slot; we hand the group-local
	// time (the residue after the top-level cycle) as `elapsed`
	// minus its top-level slot's window. For single-frame entries
	// (the s_explod.spr case the bring-up cares about) the time
	// argument is ignored by SelectFrame, so we can pass `elapsed`
	// through and let group-frame sprites self-cycle via their own
	// interval table.
	pic, err := SpriteFramePic(sp, frameIdx, elapsed)
	if err != nil {
		return err
	}
	return DrawSpriteBillboard(fb, rd, pic, origin)
}
