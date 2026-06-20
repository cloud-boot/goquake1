// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func newComposeCtx(t *testing.T) (*FrameBuffer, FrameContext) {
	t.Helper()
	fb, err := NewFrameBuffer((1+MinConsoleWidth)*CharWidth, 64)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	chars := makeCharsSheet(t)
	con, err := NewConsole(MinConsoleWidth, MinConsoleLines)
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	s, err := NewScreen(fb.Width, fb.Height)
	if err != nil {
		t.Fatalf("NewScreen: %v", err)
	}
	ctx := FrameContext{
		Screen:         s,
		Console:        con,
		Chars:          chars,
		Now:            10,
		NotifyLifetime: 3,
		MaxNotifyRows:  2,
		BackgroundIdx:  0x99,
	}
	return fb, ctx
}

// ----- Compose2D happy + error paths -------------------------------

func TestCompose2D_HappyConsoleClosed(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	// Console closed -> only the fill + (empty) notify overlay runs.
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	// Every pixel should be the background fill (no console drawn).
	for i, p := range fb.Pixels {
		if p != 0x99 {
			t.Fatalf("pixel[%d] = %#x want background 0x99", i, p)
		}
	}
}

func TestCompose2D_HappyConsoleOpen(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Console.Print("HELLO")
	ctx.Screen.ConCurrent = 2 * CharHeight // 2 rows visible
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	// Some glyph in the bottom row (CursorX area) was written;
	// at the left padding column the first glyph of "HELLO" lands.
	// 'H' = 0x48 -> row=4 col=8 -> fill = 0x10 + 4*16 + 8 = 0x58.
	got := fb.Pixels[(CharHeight)*fb.Pitch+CharWidth]
	if got != 0x58 {
		t.Fatalf("first 'H' glyph fill = %#x want 0x58", got)
	}
}

func TestCompose2D_NotifyOverlay(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	// Print a row + Linefeed-stamp it as "now" so the notify path
	// renders it (within lifetime window).
	ctx.Console.Linefeed(ctx.Now)
	ctx.Console.Print("X")
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	// 'X' = 0x58 -> row=5 col=8 -> fill = 0x10 + 5*16 + 8 = 0x68.
	// Notify overlay starts at y=0 with a CharWidth left padding
	// (matching DrawConsole's colXOffset convention).
	got := fb.Pixels[0*fb.Pitch+CharWidth]
	if got != 0x68 {
		t.Fatalf("notify 'X' fill = %#x want 0x68", got)
	}
}

func TestCompose2D_NilFB(t *testing.T) {
	_, ctx := newComposeCtx(t)
	err := Compose2D(nil, ctx)
	if !errors.Is(err, ErrComposeNilFB) {
		t.Fatalf("err = %v want ErrComposeNilFB", err)
	}
}

func TestCompose2D_NilScreen(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Screen = nil
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrComposeNilScreen) {
		t.Fatalf("err = %v want ErrComposeNilScreen", err)
	}
}

func TestCompose2D_NilConsole(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Console = nil
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrComposeNilConsole) {
		t.Fatalf("err = %v want ErrComposeNilConsole", err)
	}
}

func TestCompose2D_NilChars(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Chars = nil
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrComposeNilChars) {
		t.Fatalf("err = %v want ErrComposeNilChars", err)
	}
}

// Propagate DrawConsole / DrawNotify errors. We trigger the
// DrawConsole error via a bad-shape chars Pic with the console open.
func TestCompose2D_DrawConsoleErrorPropagated(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Console.Print("Z")
	ctx.Screen.ConCurrent = CharHeight
	// 64x64 chars (must be 128x128) -> DrawCharacter returns
	// ErrDrawCharsShape during DrawConsole's per-cell loop.
	ctx.Chars = &Pic{Width: 64, Height: 64, Pixels: make([]byte, 64*64)}
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape", err)
	}
}

// DrawNotify error propagation: console closed (so DrawConsole is
// skipped) + console row stamped "now" so DrawNotify enters the
// per-glyph loop + bad-shape chars to trip DrawCharacter.
func TestCompose2D_DrawNotifyErrorPropagated(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Console.Linefeed(ctx.Now)
	ctx.Console.Print("Z")
	ctx.Screen.ConCurrent = 0 // closed -> DrawConsole short-circuit
	ctx.Chars = &Pic{Width: 64, Height: 64, Pixels: make([]byte, 64*64)}
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape", err)
	}
}

// ----- ExpandFrame ------------------------------------------------

func TestExpandFrame_Happy(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	// A small palette: index 0x99 (the background fill) maps to a
	// distinctive RGB so we can verify the expand path ran.
	var pal Palette
	pal[0x99] = [3]byte{0x10, 0x20, 0x30}
	ctx.Palette = &pal

	dst := make([]byte, fb.Width*fb.Height*4)
	if err := ExpandFrame(fb, dst, ctx); err != nil {
		t.Fatalf("ExpandFrame: %v", err)
	}
	// First pixel = (R=0x10, G=0x20, B=0x30, A=0xFF).
	if dst[0] != 0x10 || dst[1] != 0x20 || dst[2] != 0x30 || dst[3] != 0xFF {
		t.Fatalf("first pixel RGBA = (%#x,%#x,%#x,%#x) want (0x10,0x20,0x30,0xFF)",
			dst[0], dst[1], dst[2], dst[3])
	}
}

func TestExpandFrame_NilPalette(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Palette = nil
	dst := make([]byte, fb.Width*fb.Height*4)
	err := ExpandFrame(fb, dst, ctx)
	if !errors.Is(err, ErrFBPaletteShape) {
		t.Fatalf("err = %v want ErrFBPaletteShape", err)
	}
}

func TestExpandFrame_ComposeErrorPropagated(t *testing.T) {
	_, ctx := newComposeCtx(t)
	// nil fb -> Compose2D fails before Expand runs
	var pal Palette
	ctx.Palette = &pal
	dst := make([]byte, 1024)
	err := ExpandFrame(nil, dst, ctx)
	if !errors.Is(err, ErrComposeNilFB) {
		t.Fatalf("err = %v want ErrComposeNilFB", err)
	}
}

func TestExpandFrame_DstTooSmall(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	var pal Palette
	ctx.Palette = &pal
	dst := make([]byte, 4) // way too small
	err := ExpandFrame(fb, dst, ctx)
	if !errors.Is(err, ErrFBDstTooSmall) {
		t.Fatalf("err = %v want ErrFBDstTooSmall", err)
	}
}
