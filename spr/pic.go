// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package spr

import "errors"

// ErrNoFrames is returned by [SelectFrame] / [FlatFrame] when the
// sprite has zero frames. A valid .spr always carries at least one
// frame; this is the structural-invariant guard for callers that
// hand-built a [Sprite] (the synth path in tests).
var ErrNoFrames = errors.New("spr: sprite has no frames")

// ErrFrameOutOfRange is returned by [FlatFrame] when the requested
// top-level frame index is < 0 or >= len(Frames).
var ErrFrameOutOfRange = errors.New("spr: frame index out of range")

// FlatFrame is one sprite frame collapsed to a flat palette-indexed
// bitmap + the per-frame origin offset the renderer uses to anchor
// the billboard. It's the format DrawSpriteBillboard wants: width,
// height, packed pixels (length == Width*Height).
//
// OriginX / OriginY mirror the .spr per-frame origin. The C upstream
// uses them to shift the billboard centre off the geometric centre
// (some explosion sprites are intentionally off-centre so the brightest
// flash lands at the projectile impact point); the Go renderer reads
// them via [SpriteFrameAt] when building the billboard.
type FlatFrame struct {
	OriginX int
	OriginY int
	Width   int
	Height  int
	Pixels  []byte // length == Width * Height
}

// SelectFrame resolves a (top-level-index, time) pair to a concrete
// single-frame sub-record. For SPR_SINGLE frames the time argument is
// ignored and the single bitmap is returned. For SPR_GROUP frames the
// arm walks the per-frame intervals (monotonic float32 tic boundaries,
// the upstream convention) and picks the slot whose interval the
// time-modulo-cycle falls into; this is the canonical id1 group-frame
// animation loop. tyrquake: R_GetSpriteFrame in r_sprite.c.
//
// Returns the chosen [SingleFrame] + nil on success. Errors:
//
//	ErrNoFrames           -- sp == nil OR len(sp.Frames) == 0
//	ErrFrameOutOfRange    -- frameIdx is out of [0, len(Frames))
//
// A group frame with zero sub-frames degrades to ErrNoFrames (the on-
// disk format treats this as a malformed sprite; the upstream
// Sys_Errors here -- the Go port surfaces the structural error).
//
// The returned SingleFrame's Bitmap aliases the sprite's storage; the
// caller MUST NOT mutate it.
func SelectFrame(sp *Sprite, frameIdx int, time float32) (SingleFrame, error) {
	if sp == nil || len(sp.Frames) == 0 {
		return SingleFrame{}, ErrNoFrames
	}
	if frameIdx < 0 || frameIdx >= len(sp.Frames) {
		return SingleFrame{}, ErrFrameOutOfRange
	}
	fr := sp.Frames[frameIdx]
	if fr.Type == FrameGroup {
		g := fr.Group
		if g == nil || len(g.Frames) == 0 {
			return SingleFrame{}, ErrNoFrames
		}
		// The upstream's R_GetSpriteFrame: cycle = intervals[N-1];
		// targettime = time - ((int)(time / cycle)) * cycle; walk
		// intervals[] for the first slot whose boundary >= targettime.
		// Negative cycle (degenerate sprite -- last interval <= 0)
		// collapses to the first sub-frame.
		cycle := g.Intervals[len(g.Intervals)-1]
		if cycle <= 0 {
			return g.Frames[0], nil
		}
		t := time
		if t < 0 {
			t = 0
		}
		// Modulo-cycle without math.Mod (keep the dependency surface
		// narrow + the arm hot-path-friendly). The cast-to-int
		// trick matches the upstream verbatim.
		n := int(t / cycle)
		t -= float32(n) * cycle
		// Walk to the first interval that hasn't elapsed. The last
		// interval == cycle by construction (the upstream's monotonic-
		// intervals contract), so the LAST slot's `t < ub` would be
		// `t < cycle` -- ALMOST always true after the modulo, but FP
		// rounding can leave t == cycle and trip the inequality. Treat
		// the last slot as a fall-back match (the upstream's
		// `if (i == numframes)` arm collapses to the same outcome:
		// pick the last sub-frame). The collapsed form keeps every
		// branch exercisable.
		last := len(g.Frames) - 1
		for i := 0; i < last; i++ {
			if t < g.Intervals[i] {
				return g.Frames[i], nil
			}
		}
		return g.Frames[last], nil
	}
	return fr.Single, nil
}

// Flatten returns the requested top-level frame collapsed to a
// flat ([FlatFrame]) bitmap, advancing through any group-frame
// sub-records via [SelectFrame]. Errors mirror SelectFrame.
//
// The returned FlatFrame's Pixels aliases the sprite's storage; the
// caller MUST NOT mutate it.
func Flatten(sp *Sprite, frameIdx int, time float32) (FlatFrame, error) {
	sf, err := SelectFrame(sp, frameIdx, time)
	if err != nil {
		return FlatFrame{}, err
	}
	return FlatFrame{
		OriginX: int(sf.OriginX),
		OriginY: int(sf.OriginY),
		Width:   int(sf.Width),
		Height:  int(sf.Height),
		Pixels:  sf.Bitmap,
	}, nil
}

// FrameForTime returns the cycling top-level frame index for a sprite
// driven by a single advancing time + per-frame period (the engine's
// canonical "looping animation" -- explosions, item glints,
// projectile flickers). Result is in [0, NumFrames).
//
// period <= 0 collapses to "frame 0" (no animation). A zero NumFrames
// returns 0 as well (caller is expected to nil-check the sprite
// before drawing).
//
// tyrquake: the engine's pattern of advancing a sprite's sub-frame
// every (1.0 / fps) seconds; explosions in id1 use ~0.1 s per frame
// (10 fps). The helper here lets the caller decouple the cadence
// from the .spr's group-interval data, which for s_explod.spr is a
// single SPR_SINGLE per frame (no per-frame intervals on disk).
func FrameForTime(numFrames int, period, time float32) int {
	if numFrames <= 0 {
		return 0
	}
	if period <= 0 {
		return 0
	}
	if time < 0 {
		time = 0
	}
	n := int(time / period)
	return n % numFrames
}
