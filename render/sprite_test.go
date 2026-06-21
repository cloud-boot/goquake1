// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func makeSpritePic(fill byte, w, h int) *Pic {
	pixels := make([]byte, w*h)
	for i := range pixels {
		pixels[i] = fill
	}
	return &Pic{Width: w, Height: h, Pixels: pixels}
}

func newSpriteCtx(t *testing.T) (*FrameBuffer, *RefDef, *Pic) {
	t.Helper()
	fb, err := NewFrameBuffer(320, 200)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	rd, err := NewRefDef(
		VRect{Width: 320, Height: 200},
		[3]float32{0, 0, 0},
		[3]float32{0, 0, 0},
		90,
	)
	if err != nil {
		t.Fatalf("NewRefDef: %v", err)
	}
	pic := makeSpritePic(0x42, 4, 4)
	return fb, rd, pic
}

func TestDrawSpriteBillboard_NilFB(t *testing.T) {
	_, rd, pic := newSpriteCtx(t)
	err := DrawSpriteBillboard(nil, rd, pic, [3]float32{100, 0, 0})
	if !errors.Is(err, ErrSpriteNilFB) {
		t.Fatalf("err = %v want ErrSpriteNilFB", err)
	}
}

func TestDrawSpriteBillboard_NilRefDef(t *testing.T) {
	fb, _, pic := newSpriteCtx(t)
	if err := DrawSpriteBillboard(fb, nil, pic, [3]float32{100, 0, 0}); err != nil {
		t.Fatalf("nil-rd err = %v", err)
	}
}

func TestDrawSpriteBillboard_NilPic(t *testing.T) {
	fb, rd, _ := newSpriteCtx(t)
	if err := DrawSpriteBillboard(fb, rd, nil, [3]float32{100, 0, 0}); err != nil {
		t.Fatalf("nil-pic err = %v", err)
	}
}

func TestDrawSpriteBillboard_HappyCenter(t *testing.T) {
	fb, rd, pic := newSpriteCtx(t)
	// Sprite straight ahead at +X depth = 100.
	if err := DrawSpriteBillboard(fb, rd, pic, [3]float32{100, 0, 0}); err != nil {
		t.Fatalf("DrawSpriteBillboard: %v", err)
	}
	cx, cy := fb.Width/2, fb.Height/2
	// Sprite is 4x4 centered on (cx,cy); top-left corner at
	// (cx-2, cy-2). That pixel should be the fill 0x42.
	if got := fb.Pixels[(cy-2)*fb.Pitch+(cx-2)]; got != 0x42 {
		t.Fatalf("sprite center fill = %#x want 0x42", got)
	}
}

func TestDrawSpriteBillboard_BehindCamera(t *testing.T) {
	fb, rd, pic := newSpriteCtx(t)
	if err := DrawSpriteBillboard(fb, rd, pic, [3]float32{-100, 0, 0}); err != nil {
		t.Fatalf("behind-camera err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("behind-camera sprite drew a pixel")
		}
	}
}

func TestDrawSpriteBillboard_NearClipped(t *testing.T) {
	fb, rd, pic := newSpriteCtx(t)
	if err := DrawSpriteBillboard(fb, rd, pic, [3]float32{SpriteNearClip - 1, 0, 0}); err != nil {
		t.Fatalf("near-clip err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("too-close sprite drew a pixel")
		}
	}
}

func TestDrawSpriteBillboard_BadFov(t *testing.T) {
	fb, _, pic := newSpriteCtx(t)
	rd := &RefDef{FovX: 0, FovY: 0}
	if err := DrawSpriteBillboard(fb, rd, pic, [3]float32{100, 0, 0}); err != nil {
		t.Fatalf("bad-fov err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("bad-fov sprite drew a pixel")
		}
	}
}
