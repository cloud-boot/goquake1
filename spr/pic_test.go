// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package spr

import (
	"bytes"
	"errors"
	"testing"
)

func makeSingleSprite(t *testing.T, frames []singleSpec) *Sprite {
	t.Helper()
	specs := make([]frameSpec, len(frames))
	for i, sf := range frames {
		specs[i] = frameSpec{kind: FrameSingle, single: sf}
	}
	raw, sz := build(buildSpec{width: 4, height: 4, frames: specs})
	sp, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return sp
}

func TestSelectFrame_NilSprite(t *testing.T) {
	if _, err := SelectFrame(nil, 0, 0); !errors.Is(err, ErrNoFrames) {
		t.Fatalf("err = %v want ErrNoFrames", err)
	}
}

func TestSelectFrame_EmptySprite(t *testing.T) {
	if _, err := SelectFrame(&Sprite{}, 0, 0); !errors.Is(err, ErrNoFrames) {
		t.Fatalf("err = %v want ErrNoFrames", err)
	}
}

func TestSelectFrame_OutOfRange(t *testing.T) {
	sp := makeSingleSprite(t, []singleSpec{
		{w: 2, h: 2, bitmap: []byte{1, 2, 3, 4}},
	})
	if _, err := SelectFrame(sp, -1, 0); !errors.Is(err, ErrFrameOutOfRange) {
		t.Fatalf("neg idx err = %v want ErrFrameOutOfRange", err)
	}
	if _, err := SelectFrame(sp, 1, 0); !errors.Is(err, ErrFrameOutOfRange) {
		t.Fatalf("past-end idx err = %v want ErrFrameOutOfRange", err)
	}
}

func TestSelectFrame_SingleIgnoresTime(t *testing.T) {
	sp := makeSingleSprite(t, []singleSpec{
		{w: 2, h: 2, bitmap: []byte{7, 7, 7, 7}},
	})
	for _, ts := range []float32{-1, 0, 0.5, 1000} {
		sf, err := SelectFrame(sp, 0, ts)
		if err != nil {
			t.Fatalf("time=%v err = %v", ts, err)
		}
		if sf.Bitmap[0] != 7 {
			t.Fatalf("time=%v bitmap[0] = %d want 7", ts, sf.Bitmap[0])
		}
	}
}

func TestSelectFrame_GroupCyclesByInterval(t *testing.T) {
	// Group: 3 sub-frames with intervals 0.1 / 0.2 / 0.3 (cycle = 0.3).
	raw, sz := build(buildSpec{
		width: 4, height: 4,
		frames: []frameSpec{{kind: FrameGroup, group: groupSpec{
			intervals: []float32{0.1, 0.2, 0.3},
			frames: []singleSpec{
				{w: 1, h: 1, bitmap: []byte{0xA0}},
				{w: 1, h: 1, bitmap: []byte{0xA1}},
				{w: 1, h: 1, bitmap: []byte{0xA2}},
			},
		}}},
	})
	sp, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		time float32
		want byte
	}{
		{0.00, 0xA0}, // t < 0.1
		{0.05, 0xA0}, // t < 0.1
		{0.15, 0xA1}, // 0.1 <= t < 0.2
		{0.25, 0xA2}, // 0.2 <= t < 0.3
		{0.35, 0xA0}, // wraps around: t-cycle = 0.05
		{0.65, 0xA0}, // wraps twice: t-2*cycle = 0.05
	}
	for _, c := range cases {
		sf, err := SelectFrame(sp, 0, c.time)
		if err != nil {
			t.Fatalf("time=%v err = %v", c.time, err)
		}
		if sf.Bitmap[0] != c.want {
			t.Fatalf("time=%v bitmap[0] = %#x want %#x", c.time, sf.Bitmap[0], c.want)
		}
	}
}

func TestSelectFrame_GroupNegativeTimeClampsToZero(t *testing.T) {
	raw, sz := build(buildSpec{
		width: 4, height: 4,
		frames: []frameSpec{{kind: FrameGroup, group: groupSpec{
			intervals: []float32{0.1, 0.2},
			frames: []singleSpec{
				{w: 1, h: 1, bitmap: []byte{0xB0}},
				{w: 1, h: 1, bitmap: []byte{0xB1}},
			},
		}}},
	})
	sp, _ := Load(bytes.NewReader(raw), sz)
	sf, err := SelectFrame(sp, 0, -100)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if sf.Bitmap[0] != 0xB0 {
		t.Fatalf("neg-time bitmap[0] = %#x want 0xB0", sf.Bitmap[0])
	}
}

func TestSelectFrame_GroupExactCycleBoundary(t *testing.T) {
	// t == cycle hits the "fell off the end" fall-through (last sub-frame).
	raw, sz := build(buildSpec{
		width: 4, height: 4,
		frames: []frameSpec{{kind: FrameGroup, group: groupSpec{
			intervals: []float32{0.5},
			frames: []singleSpec{
				{w: 1, h: 1, bitmap: []byte{0xC0}},
			},
		}}},
	})
	sp, _ := Load(bytes.NewReader(raw), sz)
	// Exact-cycle boundary: t/cycle = 1.0, n = 1, t -= 1*0.5 -> 0.0.
	// Loop sees t < 0.5 (interval upper bound) -> picks slot 0.
	sf, err := SelectFrame(sp, 0, 0.5)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if sf.Bitmap[0] != 0xC0 {
		t.Fatalf("bitmap[0] = %#x want 0xC0", sf.Bitmap[0])
	}
}

// TestSelectFrame_GroupZeroCycle covers the cycle<=0 degenerate
// guard (intervals[last] == 0), which collapses to "always pick
// sub-frame 0" -- matching the upstream's R_GetSpriteFrame guard
// against a divide-by-zero on a misauthored sprite.
func TestSelectFrame_GroupZeroCycle(t *testing.T) {
	raw, sz := build(buildSpec{
		width: 4, height: 4,
		frames: []frameSpec{{kind: FrameGroup, group: groupSpec{
			intervals: []float32{0},
			frames: []singleSpec{
				{w: 1, h: 1, bitmap: []byte{0xD0}},
			},
		}}},
	})
	sp, _ := Load(bytes.NewReader(raw), sz)
	sf, err := SelectFrame(sp, 0, 999)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if sf.Bitmap[0] != 0xD0 {
		t.Fatalf("zero-cycle bitmap[0] = %#x want 0xD0", sf.Bitmap[0])
	}
}

// TestSelectFrame_GroupNilOrEmpty hand-builds a Sprite (bypassing
// Load) with a malformed group-frame to exercise the structural
// nil-guard. A loader-produced sprite cannot reach this branch
// because Load always allocates the group + its Frames slice; the
// guard is defensive against hand-constructed (test-fixture / future-
// caller) Sprites.
func TestSelectFrame_GroupNilOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		sp   *Sprite
	}{
		{
			"nil-group",
			&Sprite{Frames: []Frame{{Type: FrameGroup, Group: nil}}},
		},
		{
			"empty-group-frames",
			&Sprite{Frames: []Frame{{Type: FrameGroup, Group: &GroupFrame{}}}},
		},
	}
	for _, c := range cases {
		if _, err := SelectFrame(c.sp, 0, 0); !errors.Is(err, ErrNoFrames) {
			t.Fatalf("%s err = %v want ErrNoFrames", c.name, err)
		}
	}
}

// TestSelectFrame_GroupLastSubframePicked exercises the post-loop
// fall-back arm: when the after-modulo t is >= every per-slot
// interval the function returns the LAST sub-frame (mirroring the
// upstream R_GetSpriteFrame's "if (i == numframes)" tail). The
// loop in our implementation iterates over `Intervals[0..last-1]`
// only -- the last slot is the implicit catch-all -- so the
// fall-through is reached whenever NO earlier slot matches.
//
// Construction: 3 sub-frames, intervals = [0.1, 0.2, 0.3]. cycle =
// 0.3; with t = 0.25 (no wrap), the loop checks 0.25 < 0.1 (false)
// and 0.25 < 0.2 (false), then falls through to the LAST slot.
func TestSelectFrame_GroupLastSubframePicked(t *testing.T) {
	raw, sz := build(buildSpec{
		width: 4, height: 4,
		frames: []frameSpec{{kind: FrameGroup, group: groupSpec{
			intervals: []float32{0.1, 0.2, 0.3},
			frames: []singleSpec{
				{w: 1, h: 1, bitmap: []byte{0xA0}},
				{w: 1, h: 1, bitmap: []byte{0xA1}},
				{w: 1, h: 1, bitmap: []byte{0xA2}},
			},
		}}},
	})
	sp, _ := Load(bytes.NewReader(raw), sz)
	sf, err := SelectFrame(sp, 0, 0.25)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if sf.Bitmap[0] != 0xA2 {
		t.Fatalf("got %#x want 0xA2 (last sub-frame)", sf.Bitmap[0])
	}
}

func TestFlatten_HappyPath(t *testing.T) {
	sp := makeSingleSprite(t, []singleSpec{
		{originX: -2, originY: -2, w: 4, h: 4, bitmap: bytes.Repeat([]byte{0x42}, 16)},
	})
	ff, err := Flatten(sp, 0, 0)
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}
	if ff.Width != 4 || ff.Height != 4 {
		t.Fatalf("dims = %dx%d want 4x4", ff.Width, ff.Height)
	}
	if ff.OriginX != -2 || ff.OriginY != -2 {
		t.Fatalf("origin = (%d,%d) want (-2,-2)", ff.OriginX, ff.OriginY)
	}
	if len(ff.Pixels) != 16 || ff.Pixels[0] != 0x42 {
		t.Fatalf("pixels: len=%d [0]=%#x", len(ff.Pixels), ff.Pixels[0])
	}
}

func TestFlatten_PropagatesSelectError(t *testing.T) {
	if _, err := Flatten(nil, 0, 0); !errors.Is(err, ErrNoFrames) {
		t.Fatalf("nil err = %v want ErrNoFrames", err)
	}
}

func TestFrameForTime_ZeroFrames(t *testing.T) {
	if got := FrameForTime(0, 0.1, 10); got != 0 {
		t.Fatalf("zero-frames returned %d want 0", got)
	}
}

func TestFrameForTime_ZeroPeriod(t *testing.T) {
	if got := FrameForTime(5, 0, 100); got != 0 {
		t.Fatalf("zero-period returned %d want 0", got)
	}
}

func TestFrameForTime_NegativePeriod(t *testing.T) {
	if got := FrameForTime(5, -0.1, 100); got != 0 {
		t.Fatalf("neg-period returned %d want 0", got)
	}
}

func TestFrameForTime_NegativeTimeClamps(t *testing.T) {
	if got := FrameForTime(5, 0.1, -5); got != 0 {
		t.Fatalf("neg-time returned %d want 0", got)
	}
}

func TestFrameForTime_Cycles(t *testing.T) {
	cases := []struct {
		nframes int
		period  float32
		time    float32
		want    int
	}{
		{6, 0.1, 0.00, 0},
		{6, 0.1, 0.05, 0},
		{6, 0.1, 0.15, 1},
		{6, 0.1, 0.55, 5},
		{6, 0.1, 0.65, 0}, // wraps
		{6, 0.1, 1.25, 0}, // 12 cycles + remainder 0
		{6, 0.1, 1.35, 1},
	}
	for _, c := range cases {
		got := FrameForTime(c.nframes, c.period, c.time)
		if got != c.want {
			t.Fatalf("FrameForTime(%d, %v, %v) = %d want %d",
				c.nframes, c.period, c.time, got, c.want)
		}
	}
}
