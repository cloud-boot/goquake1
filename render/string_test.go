// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestDrawString_Happy(t *testing.T) {
	fb := newTestFB(t, 64, 16, 0)
	chars := makeCharsSheet(t)
	if err := DrawString(fb, chars, 0, 0, "AB"); err != nil {
		t.Fatalf("DrawString: %v", err)
	}
	// 'A' = 0x41 -> row=4 col=1 -> fill = 0x10 + 4*16 + 1 = 0x51
	if got := fb.Pixels[0]; got != 0x51 {
		t.Fatalf("first glyph fill = %#x want 0x51", got)
	}
	// 'B' = 0x42 -> row=4 col=2 -> fill = 0x10 + 4*16 + 2 = 0x52
	// at x=CharWidth
	if got := fb.Pixels[CharWidth]; got != 0x52 {
		t.Fatalf("second glyph fill = %#x want 0x52", got)
	}
}

func TestDrawString_EmptyOK(t *testing.T) {
	fb := newTestFB(t, 32, 16, 0xAA)
	chars := makeCharsSheet(t)
	if err := DrawString(fb, chars, 0, 0, ""); err != nil {
		t.Fatalf("DrawString empty: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0xAA {
			t.Fatalf("empty-string draw modified framebuffer")
		}
	}
}

func TestDrawString_ErrPropagated(t *testing.T) {
	fb := newTestFB(t, 32, 16, 0)
	bad := &Pic{Width: 32, Height: 32, Pixels: make([]byte, 32*32)}
	if err := DrawString(fb, bad, 0, 0, "X"); !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape", err)
	}
}

func TestDrawString_NilFB(t *testing.T) {
	chars := makeCharsSheet(t)
	if err := DrawString(nil, chars, 0, 0, "X"); !errors.Is(err, ErrDrawNilFB) {
		t.Fatalf("err = %v want ErrDrawNilFB", err)
	}
}

func TestDrawColorString_HappyHighBit(t *testing.T) {
	fb := newTestFB(t, 32, 16, 0)
	chars := makeCharsSheet(t)
	if err := DrawColorString(fb, chars, 0, 0, "A"); err != nil {
		t.Fatalf("DrawColorString: %v", err)
	}
	// 'A' | 0x80 = 0xC1 -> row=12 col=1 -> fill = 0x10 + 12*16 + 1 = 0xD1
	if got := fb.Pixels[0]; got != 0xD1 {
		t.Fatalf("colored 'A' fill = %#x want 0xD1", got)
	}
}

func TestDrawColorString_ErrPropagated(t *testing.T) {
	fb := newTestFB(t, 32, 16, 0)
	bad := &Pic{Width: 32, Height: 32, Pixels: make([]byte, 32*32)}
	if err := DrawColorString(fb, bad, 0, 0, "X"); !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape", err)
	}
}

func TestDrawCenteredString_Happy(t *testing.T) {
	fb := newTestFB(t, 80, 16, 0)
	chars := makeCharsSheet(t)
	// 4 chars * CharWidth = 32 px wide; centerX=40 -> left = 40 - 16 = 24.
	if err := DrawCenteredString(fb, chars, 40, 0, "ABCD"); err != nil {
		t.Fatalf("DrawCenteredString: %v", err)
	}
	// 'A' fill 0x51 lands at pixel column 24.
	if got := fb.Pixels[24]; got != 0x51 {
		t.Fatalf("centered 'A' fill at col 24 = %#x want 0x51", got)
	}
}

func TestDrawCenteredString_ErrPropagated(t *testing.T) {
	fb := newTestFB(t, 80, 16, 0)
	bad := &Pic{Width: 32, Height: 32, Pixels: make([]byte, 32*32)}
	if err := DrawCenteredString(fb, bad, 40, 0, "X"); !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape", err)
	}
}
