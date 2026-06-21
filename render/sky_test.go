// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func makeSkyTex(t *testing.T, w, h int) *Pic {
	t.Helper()
	pixels := make([]byte, w*h)
	for v := 0; v < h; v++ {
		for u := 0; u < w; u++ {
			// Encode (u, v) into the byte so tests can verify sampling.
			pixels[v*w+u] = byte((u*7 + v*13) & 0xFF)
		}
	}
	return &Pic{Width: w, Height: h, Pixels: pixels}
}

// ----- DrawSkyHorizon error paths --------------------------------

func TestDrawSkyHorizon_NilFB(t *testing.T) {
	tex := makeSkyTex(t, 256, 128)
	err := DrawSkyHorizon(nil, tex, 0, 0, 100)
	if !errors.Is(err, ErrSkyNilFB) {
		t.Fatalf("err = %v want ErrSkyNilFB", err)
	}
}

func TestDrawSkyHorizon_NilTex(t *testing.T) {
	fb, _ := NewFrameBuffer(320, 200)
	err := DrawSkyHorizon(fb, nil, 0, 0, 100)
	if !errors.Is(err, ErrSkyNilTex) {
		t.Fatalf("err = %v want ErrSkyNilTex", err)
	}
}

func TestDrawSkyHorizon_ZeroBand(t *testing.T) {
	fb, _ := NewFrameBuffer(320, 200)
	tex := makeSkyTex(t, 256, 128)
	if err := DrawSkyHorizon(fb, tex, 0, 0, 0); err != nil {
		t.Fatalf("zero-band err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("zero-band drew pixels")
		}
	}
}

func TestDrawSkyHorizon_NegativeBand(t *testing.T) {
	fb, _ := NewFrameBuffer(320, 200)
	tex := makeSkyTex(t, 256, 128)
	if err := DrawSkyHorizon(fb, tex, 0, 0, -10); err != nil {
		t.Fatalf("negative-band err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("negative-band drew pixels")
		}
	}
}

// ----- DrawSkyHorizon happy paths -------------------------------

func TestDrawSkyHorizon_Happy(t *testing.T) {
	fb, _ := NewFrameBuffer(256, 128)
	tex := makeSkyTex(t, 256, 128)
	if err := DrawSkyHorizon(fb, tex, 0, 0, 128); err != nil {
		t.Fatalf("DrawSkyHorizon: %v", err)
	}
	// At yaw=0, time=0, uOffset=0; (x,y) = (0,0) -> tex(0, 0) =
	// (0*7 + 0*13) & 0xFF = 0
	if fb.Pixels[0] != 0 {
		t.Fatalf("pixel (0,0) = %#x want 0", fb.Pixels[0])
	}
	// (1, 0) -> tex(1, 0) = 7
	if fb.Pixels[1] != 7 {
		t.Fatalf("pixel (1,0) = %#x want 7", fb.Pixels[1])
	}
}

func TestDrawSkyHorizon_YawWraps(t *testing.T) {
	fb, _ := NewFrameBuffer(64, 16)
	tex := makeSkyTex(t, 256, 128)
	// Yaw 360 deg * SkyYawScale = 360 texels; wraps to 360-256 = 104.
	// Pixel (0,0) reads tex(104, 0) = 104*7 & 0xFF = 728&255 = 216
	if err := DrawSkyHorizon(fb, tex, 360, 0, 16); err != nil {
		t.Fatalf("DrawSkyHorizon: %v", err)
	}
	if fb.Pixels[0] != 216 {
		t.Fatalf("yaw 360 pixel (0,0) = %#x want 0xD8", fb.Pixels[0])
	}
}

func TestDrawSkyHorizon_NegativeYaw(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 16)
	tex := makeSkyTex(t, 256, 128)
	// -10 deg * SkyYawScale = -10. After uOffset wrap: -10 + 256 = 246.
	if err := DrawSkyHorizon(fb, tex, -10, 0, 16); err != nil {
		t.Fatalf("DrawSkyHorizon: %v", err)
	}
	// (0,0) -> tex(246, 0) = 246*7 & 0xFF = 1722 & 255 = 186
	want := byte((246 * 7) & 0xFF)
	if fb.Pixels[0] != want {
		t.Fatalf("yaw -10 pixel (0,0) = %#x want %#x", fb.Pixels[0], want)
	}
}

func TestDrawSkyHorizon_TimeScrolls(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 16)
	tex := makeSkyTex(t, 256, 128)
	// time=1, SkyScrollSpeed=8 -> uOffset += 8.
	if err := DrawSkyHorizon(fb, tex, 0, 1, 16); err != nil {
		t.Fatalf("DrawSkyHorizon: %v", err)
	}
	// (0,0) -> tex(8, 0) = 8*7 & 0xFF = 56
	if fb.Pixels[0] != 56 {
		t.Fatalf("time=1 pixel (0,0) = %#x want 56", fb.Pixels[0])
	}
}

func TestDrawSkyHorizon_BandTallerThanFB(t *testing.T) {
	// bandHeight clamps to fb.Height.
	fb, _ := NewFrameBuffer(32, 16)
	tex := makeSkyTex(t, 256, 128)
	if err := DrawSkyHorizon(fb, tex, 0, 0, 100); err != nil {
		t.Fatalf("DrawSkyHorizon: %v", err)
	}
	// Bottom pixel should still be written (band clamped to 16).
	if fb.Pixels[15*fb.Pitch+0] == 0 && fb.Pixels[15*fb.Pitch+1] == 0 {
		// At y=15, v=(15*128/16)=120; tex(0, 120)=120*13&255=24
		t.Fatalf("bottom row not written")
	}
}

func TestDrawSkyHorizon_VScalesToBand(t *testing.T) {
	fb, _ := NewFrameBuffer(32, 64) // band 64, tex height 128 -> v step 2
	tex := makeSkyTex(t, 256, 128)
	if err := DrawSkyHorizon(fb, tex, 0, 0, 64); err != nil {
		t.Fatalf("DrawSkyHorizon: %v", err)
	}
	// y=0 -> v=0; y=1 -> v=1*128/64=2.
	// pixel (0,0) -> tex(0,0) = 0
	if fb.Pixels[0] != 0 {
		t.Fatalf("(0,0) = %#x want 0", fb.Pixels[0])
	}
	// (0,1) -> tex(0, 2) = (0*7 + 2*13) = 26
	if fb.Pixels[fb.Pitch] != 26 {
		t.Fatalf("(0,1) = %#x want 26", fb.Pixels[fb.Pitch])
	}
}
