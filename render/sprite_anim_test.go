// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/spr"
)

// buildSpriteRaw encodes a minimal single-frame .spr in memory; lets
// the render-side tests load real *spr.Sprite values without pulling
// in the spr-package test fixtures.
func buildSpriteRaw(t *testing.T, frames []frameDesc) *spr.Sprite {
	t.Helper()
	buf := &bytes.Buffer{}
	wU32 := func(v uint32) { _ = binary.Write(buf, binary.LittleEndian, v) }
	wI32 := func(v int32) { _ = binary.Write(buf, binary.LittleEndian, v) }
	wF32 := func(v float32) { _ = binary.Write(buf, binary.LittleEndian, v) }
	wU32(spr.IDSpriteHeader)
	wI32(spr.Version)
	wI32(0)    // type
	wF32(16.0) // bounding_radius
	wI32(8)    // width
	wI32(8)    // height
	wI32(int32(len(frames)))
	wF32(0) // beam length
	wI32(0) // sync type
	for _, f := range frames {
		wI32(spr.FrameSingle)
		wI32(f.ox)
		wI32(f.oy)
		wI32(f.w)
		wI32(f.h)
		buf.Write(f.pixels)
	}
	raw := buf.Bytes()
	sp, err := spr.Load(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("spr.Load: %v", err)
	}
	return sp
}

type frameDesc struct {
	ox, oy, w, h int32
	pixels       []byte
}

func makeSpriteFrame(fill byte, w, h int32) frameDesc {
	pix := make([]byte, w*h)
	for i := range pix {
		pix[i] = fill
	}
	return frameDesc{w: w, h: h, pixels: pix}
}

func TestSpriteFramePic_Nil(t *testing.T) {
	if _, err := SpriteFramePic(nil, 0, 0); !errors.Is(err, ErrSpriteAnimNilSprite) {
		t.Fatalf("err = %v want ErrSpriteAnimNilSprite", err)
	}
}

func TestSpriteFramePic_PropagatesSelectErr(t *testing.T) {
	sp := buildSpriteRaw(t, []frameDesc{makeSpriteFrame(1, 2, 2)})
	if _, err := SpriteFramePic(sp, 99, 0); !errors.Is(err, spr.ErrFrameOutOfRange) {
		t.Fatalf("err = %v want spr.ErrFrameOutOfRange", err)
	}
}

func TestSpriteFramePic_HappyPath(t *testing.T) {
	sp := buildSpriteRaw(t, []frameDesc{makeSpriteFrame(0x77, 3, 4)})
	pic, err := SpriteFramePic(sp, 0, 0)
	if err != nil {
		t.Fatalf("SpriteFramePic: %v", err)
	}
	if pic.Width != 3 || pic.Height != 4 {
		t.Fatalf("dims = %dx%d want 3x4", pic.Width, pic.Height)
	}
	if len(pic.Pixels) != 12 || pic.Pixels[0] != 0x77 {
		t.Fatalf("pixels: len=%d [0]=%#x", len(pic.Pixels), pic.Pixels[0])
	}
	// Owned-storage invariant: mutating the pic must not corrupt the sprite.
	pic.Pixels[0] = 0xFF
	if sp.Frames[0].Single.Bitmap[0] != 0x77 {
		t.Fatalf("sprite bitmap mutated by pic write")
	}
}

func TestSpriteFrameForElapsed_ZeroFrames(t *testing.T) {
	if SpriteFrameForElapsed(0, 0.5) != 0 {
		t.Fatalf("zero-frames returned non-zero")
	}
}

func TestSpriteFrameForElapsed_CyclesAtPeriod(t *testing.T) {
	// 6 frames, period 0.1; elapsed 0.55 -> floor(5.5) = 5; cycles
	// once at elapsed 0.6.
	cases := []struct {
		elapsed float32
		want    int
	}{
		{0.00, 0},
		{0.05, 0},
		{0.15, 1},
		{0.55, 5},
		{0.60, 0}, // wraps
	}
	for _, c := range cases {
		got := SpriteFrameForElapsed(6, c.elapsed)
		if got != c.want {
			t.Fatalf("elapsed=%v frame = %d want %d", c.elapsed, got, c.want)
		}
	}
}

func TestDrawSpriteAtTime_NilFB(t *testing.T) {
	sp := buildSpriteRaw(t, []frameDesc{makeSpriteFrame(1, 2, 2)})
	if err := DrawSpriteAtTime(nil, nil, sp, [3]float32{0, 0, 0}, 0); !errors.Is(err, ErrSpriteNilFB) {
		t.Fatalf("err = %v want ErrSpriteNilFB", err)
	}
}

func TestDrawSpriteAtTime_NilSprite(t *testing.T) {
	fb, _ := NewFrameBuffer(64, 64)
	err := DrawSpriteAtTime(fb, nil, nil, [3]float32{0, 0, 0}, 0)
	if !errors.Is(err, ErrSpriteAnimNilSprite) {
		t.Fatalf("err = %v want ErrSpriteAnimNilSprite", err)
	}
}

func TestDrawSpriteAtTime_HappyCenter(t *testing.T) {
	fb, _ := NewFrameBuffer(320, 200)
	rd, _ := NewRefDef(
		VRect{Width: 320, Height: 200},
		[3]float32{0, 0, 0},
		[3]float32{0, 0, 0},
		90,
	)
	sp := buildSpriteRaw(t, []frameDesc{makeSpriteFrame(0x42, 4, 4)})
	if err := DrawSpriteAtTime(fb, rd, sp, [3]float32{100, 0, 0}, 0); err != nil {
		t.Fatalf("DrawSpriteAtTime: %v", err)
	}
	cx, cy := fb.Width/2, fb.Height/2
	if got := fb.Pixels[(cy-2)*fb.Pitch+(cx-2)]; got != 0x42 {
		t.Fatalf("centre fill = %#x want 0x42", got)
	}
}

func TestDrawSpriteAtTime_AdvancesAcrossFrames(t *testing.T) {
	// 3 frames with distinct fills; check the picked frame walks 0/1/2
	// as elapsed crosses period boundaries.
	frames := []frameDesc{
		makeSpriteFrame(0x10, 2, 2),
		makeSpriteFrame(0x20, 2, 2),
		makeSpriteFrame(0x30, 2, 2),
	}
	sp := buildSpriteRaw(t, frames)
	cases := []struct {
		elapsed float32
		want    byte
	}{
		{0.00, 0x10},
		{0.05, 0x10},
		{0.15, 0x20},
		{0.25, 0x30},
		{0.35, 0x10}, // wraps
	}
	for _, c := range cases {
		fb, _ := NewFrameBuffer(64, 64)
		rd, _ := NewRefDef(VRect{Width: 64, Height: 64},
			[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, 90)
		if err := DrawSpriteAtTime(fb, rd, sp, [3]float32{50, 0, 0}, c.elapsed); err != nil {
			t.Fatalf("DrawSpriteAtTime elapsed=%v: %v", c.elapsed, err)
		}
		cx, cy := fb.Width/2, fb.Height/2
		got := fb.Pixels[(cy-1)*fb.Pitch+(cx-1)]
		if got != c.want {
			t.Fatalf("elapsed=%v centre fill = %#x want %#x", c.elapsed, got, c.want)
		}
	}
}

func TestDrawSpriteAtTime_OutOfRangePropagates(t *testing.T) {
	// Forge a sprite with zero frames so SpriteFramePic returns
	// spr.ErrFrameOutOfRange after SpriteFrameForElapsed picks 0.
	// A zero-frames sprite trips the spr.ErrNoFrames guard inside
	// Flatten -> SelectFrame; DrawSpriteAtTime propagates it.
	fb, _ := NewFrameBuffer(64, 64)
	rd, _ := NewRefDef(VRect{Width: 64, Height: 64},
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, 90)
	emptySp := &spr.Sprite{}
	err := DrawSpriteAtTime(fb, rd, emptySp, [3]float32{50, 0, 0}, 0)
	if !errors.Is(err, spr.ErrNoFrames) {
		t.Fatalf("err = %v want spr.ErrNoFrames", err)
	}
}

// TestSpriteFramePeriod_Stable pins the cadence constant against
// drift; a future "make explosions faster" PR should consciously
// bump this with a matching test update.
func TestSpriteFramePeriod_Stable(t *testing.T) {
	const want = 0.1
	if math.Abs(float64(SpriteFramePeriod)-want) > 1e-7 {
		t.Fatalf("SpriteFramePeriod = %v want %v", SpriteFramePeriod, want)
	}
}
