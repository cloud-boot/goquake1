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

// TestCompose2D_SkipBackgroundFill verifies the SkipBackgroundFill
// flag suppresses Compose2D's DrawFill so pre-existing pixels (the
// 3D scene a Pre2DDraw hook just rasterized) survive into the
// composed frame.
func TestCompose2D_SkipBackgroundFill(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	// Pre-fill the framebuffer with a sentinel value (0x42) that
	// is NOT the BackgroundIdx (0x99). With SkipBackgroundFill the
	// sentinel must survive the Compose2D call.
	for i := range fb.Pixels {
		fb.Pixels[i] = 0x42
	}
	ctx.SkipBackgroundFill = true
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	// Console is closed + notify is empty -> every pixel should
	// still be the pre-filled sentinel (no background fill ran).
	for i, p := range fb.Pixels {
		if p != 0x42 {
			t.Fatalf("pixel[%d] = %#x want sentinel 0x42 (SkipBackgroundFill should suppress DrawFill)", i, p)
		}
	}
}

// TestCompose2D_CenterPrintRenderedAtFortyPercent drives the
// centerprint overlay through Compose2D + asserts a glyph lands at
// (fb.Height * 2 / 5). The single-char banner "X" lands one glyph
// wide centered on Screen.CenterX -> the left edge is CenterX -
// CharWidth/2; the glyph fill 'X' = 0x58 maps to row=5 col=8 in the
// makeCharsSheet helper, so fill = 0x10 + 5*16 + 8 = 0x68.
func TestCompose2D_CenterPrintRenderedAtFortyPercent(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.CenterPrintText = "X"
	ctx.CenterPrintExpiry = ctx.Now + 1 // 1s of life left
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	y := fb.Height * 2 / 5
	// Single 'X', centered on CenterX -> left = CenterX - CharWidth/2.
	leftX := ctx.Screen.CenterX - CharWidth/2
	idx := y*fb.Pitch + leftX
	if fb.Pixels[idx] != 0x68 {
		t.Fatalf("centerprint glyph pixel at (x=%d,y=%d) = %#x want 0x68 ('X' fill)", leftX, y, fb.Pixels[idx])
	}
}

// TestCompose2D_CenterPrintEmptyTextSkipped verifies an empty
// CenterPrintText leaves the centerprint anchor untouched (no
// DrawCenteredString runs).
func TestCompose2D_CenterPrintEmptyTextSkipped(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.CenterPrintText = ""
	ctx.CenterPrintExpiry = ctx.Now + 5
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	y := fb.Height * 2 / 5
	leftX := ctx.Screen.CenterX - CharWidth/2
	if fb.Pixels[y*fb.Pitch+leftX] != ctx.BackgroundIdx {
		t.Fatalf("centerprint anchor wrote despite empty text")
	}
}

// TestCompose2D_CenterPrintExpiredSkipped verifies an expired
// centerprint (Now >= CenterPrintExpiry) is NOT drawn.
func TestCompose2D_CenterPrintExpiredSkipped(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.CenterPrintText = "X"
	ctx.CenterPrintExpiry = ctx.Now - 1 // already expired
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	y := fb.Height * 2 / 5
	leftX := ctx.Screen.CenterX - CharWidth/2
	if fb.Pixels[y*fb.Pitch+leftX] != ctx.BackgroundIdx {
		t.Fatalf("expired centerprint was still drawn")
	}
}

// TestCompose2D_CenterPrintExactExpirySkipped verifies the boundary
// case Now == CenterPrintExpiry (the strict-less-than guard treats it
// as expired -- matches the upstream's scr_centertime_off > 0 check).
func TestCompose2D_CenterPrintExactExpirySkipped(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.CenterPrintText = "X"
	ctx.CenterPrintExpiry = ctx.Now // boundary -- treat as expired
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	y := fb.Height * 2 / 5
	leftX := ctx.Screen.CenterX - CharWidth/2
	if fb.Pixels[y*fb.Pitch+leftX] != ctx.BackgroundIdx {
		t.Fatalf("centerprint drawn at Now == Expiry boundary; want expired")
	}
}

// --- Intermission overlay ----------------------------------------

// Intermission true + a single-line block: the centered text lands
// on the centerY row. With one line, y0 = fb.Height/2 - CharHeight/2.
// "X" -> left = CenterX - CharWidth/2; the 'X' fill (0x68) appears
// at the line's anchor pixel.
func TestCompose2D_Intermission_DrawsLineBlockCentered(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Intermission = true
	ctx.IntermissionLines = []string{"X"}
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	y := fb.Height/2 - CharHeight/2
	leftX := ctx.Screen.CenterX - CharWidth/2
	if fb.Pixels[y*fb.Pitch+leftX] != 0x68 {
		t.Fatalf("intermission glyph pixel = %#x want 0x68 ('X' fill)",
			fb.Pixels[y*fb.Pitch+leftX])
	}
}

// Intermission true suppresses centerprint even if CenterPrintText is
// non-empty + expiry in future.
func TestCompose2D_Intermission_SuppressesCenterPrint(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Intermission = true
	ctx.IntermissionLines = nil // empty intermission block
	ctx.CenterPrintText = "X"
	ctx.CenterPrintExpiry = ctx.Now + 100
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	// Centerprint would land at 2/5 height. Intermission must suppress
	// it; that pixel stays at the background fill.
	y := fb.Height * 2 / 5
	leftX := ctx.Screen.CenterX - CharWidth/2
	if fb.Pixels[y*fb.Pitch+leftX] != ctx.BackgroundIdx {
		t.Fatalf("centerprint drawn despite Intermission flag: pixel=%#x",
			fb.Pixels[y*fb.Pitch+leftX])
	}
}

// Intermission true + empty IntermissionLines = no-op (no glyphs
// drawn, no error). The framebuffer stays at the background fill.
func TestCompose2D_Intermission_EmptyLinesNoOp(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Intermission = true
	ctx.IntermissionLines = nil
	if err := Compose2D(fb, ctx); err != nil {
		t.Fatalf("Compose2D: %v", err)
	}
	for i, p := range fb.Pixels {
		if p != 0x99 {
			t.Fatalf("pixel[%d] = %#x; want background 0x99 (empty intermission block draws nothing)", i, p)
		}
	}
}

// Intermission glyph error path: a malformed chars Pic propagates
// the same ErrDrawCharsShape sentinel out of Compose2D. We close the
// console + clear notify so the only DrawCharacter call is the
// intermission line.
func TestCompose2D_Intermission_ErrorPropagated(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	ctx.Chars = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 16)}
	ctx.Console, _ = NewConsole(MinConsoleWidth, MinConsoleLines)
	ctx.Screen.ConCurrent = 0
	ctx.Intermission = true
	ctx.IntermissionLines = []string{"X"}
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape (intermission should propagate DrawCharacter errors)", err)
	}
}

// TestCompose2D_CenterPrintErrorPropagated proves DrawCenteredString
// errors propagate out of Compose2D verbatim. A mis-sized chars Pic
// trips ErrDrawCharsShape inside DrawCharacter.
func TestCompose2D_CenterPrintErrorPropagated(t *testing.T) {
	fb, ctx := newComposeCtx(t)
	// Stash a 4x4 chars Pic that DrawCharacter will reject.
	ctx.Chars = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 16)}
	// Skip the console/notify paths (they'd trip the same error first).
	ctx.CenterPrintText = "X"
	ctx.CenterPrintExpiry = ctx.Now + 1
	// Empty the notify + close the console so the only DrawCharacter
	// call originates from the centerprint arm.
	ctx.Console, _ = NewConsole(MinConsoleWidth, MinConsoleLines)
	ctx.Screen.ConCurrent = 0
	err := Compose2D(fb, ctx)
	if !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("err = %v want ErrDrawCharsShape (centerprint should propagate DrawCharacter errors)", err)
	}
}
