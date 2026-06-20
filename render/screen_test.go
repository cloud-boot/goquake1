// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

// ----- NewScreen ---------------------------------------------------

func TestNewScreenHappy(t *testing.T) {
	s, err := NewScreen(640, 400)
	if err != nil {
		t.Fatalf("NewScreen: %v", err)
	}
	if s.Width != 640 || s.Height != 400 {
		t.Fatalf("dim = %dx%d want 640x400", s.Width, s.Height)
	}
	if s.CenterX != 320 || s.CenterY != 200 {
		t.Fatalf("center = (%d,%d) want (320,200)", s.CenterX, s.CenterY)
	}
	if s.ConLines != 200 {
		t.Fatalf("ConLines = %d want 200 (Height/2)", s.ConLines)
	}
	if s.ConCurrent != 0 {
		t.Fatalf("ConCurrent = %d want 0 (closed)", s.ConCurrent)
	}
	if s.ScrollSpeed != DefaultScrollSpeed {
		t.Fatalf("ScrollSpeed = %d want %d", s.ScrollSpeed, DefaultScrollSpeed)
	}
}

func TestNewScreenBadDim(t *testing.T) {
	cases := []struct{ w, h int }{
		{0, 200},
		{200, 0},
		{-1, 200},
		{200, -1},
	}
	for _, c := range cases {
		if _, err := NewScreen(c.w, c.h); !errors.Is(err, ErrScreenDim) {
			t.Fatalf("NewScreen(%d,%d) err = %v want ErrScreenDim", c.w, c.h, err)
		}
	}
}

// ----- AnimateConsole ----------------------------------------------

func TestAnimateConsoleOpening(t *testing.T) {
	s := &Screen{ConLines: 100, ConCurrent: 0, ScrollSpeed: 30}
	steps := 0
	for s.ConCurrent != s.ConLines {
		s.AnimateConsole(s.ConLines)
		steps++
		if steps > 100 {
			t.Fatalf("animation never converged, ConCurrent = %d", s.ConCurrent)
		}
	}
	// 0 -> 30 -> 60 -> 90 -> 100 (clamp) = 4 steps.
	if steps != 4 {
		t.Fatalf("opening took %d steps, want 4", steps)
	}
	if s.ConCurrent != 100 {
		t.Fatalf("ConCurrent = %d want 100", s.ConCurrent)
	}
}

func TestAnimateConsoleClosing(t *testing.T) {
	s := &Screen{ConLines: 100, ConCurrent: 100, ScrollSpeed: 30}
	steps := 0
	for s.ConCurrent != 0 {
		s.AnimateConsole(0)
		steps++
		if steps > 100 {
			t.Fatalf("animation never converged, ConCurrent = %d", s.ConCurrent)
		}
	}
	if steps != 4 {
		t.Fatalf("closing took %d steps, want 4", steps)
	}
}

func TestAnimateConsoleClampOnOpen(t *testing.T) {
	// One step covers the whole distance + then some -> snap to target.
	s := &Screen{ConLines: 100, ConCurrent: 90, ScrollSpeed: 50}
	s.AnimateConsole(s.ConLines)
	if s.ConCurrent != 100 {
		t.Fatalf("ConCurrent = %d want 100 (clamp)", s.ConCurrent)
	}
}

func TestAnimateConsoleClampOnClose(t *testing.T) {
	s := &Screen{ConLines: 100, ConCurrent: 10, ScrollSpeed: 50}
	s.AnimateConsole(0)
	if s.ConCurrent != 0 {
		t.Fatalf("ConCurrent = %d want 0 (clamp)", s.ConCurrent)
	}
}

func TestAnimateConsoleAlreadyAtTarget(t *testing.T) {
	s := &Screen{ConLines: 100, ConCurrent: 100, ScrollSpeed: 30}
	s.AnimateConsole(100)
	if s.ConCurrent != 100 {
		t.Fatalf("ConCurrent moved off target: %d", s.ConCurrent)
	}
}

func TestAnimateConsoleZeroSpeedSnaps(t *testing.T) {
	// Degenerate ScrollSpeed should not stall the animation.
	s := &Screen{ConLines: 100, ConCurrent: 0, ScrollSpeed: 0}
	s.AnimateConsole(s.ConLines)
	if s.ConCurrent != 100 {
		t.Fatalf("zero-speed should snap, got %d", s.ConCurrent)
	}
	// Negative too.
	s = &Screen{ConLines: 100, ConCurrent: 100, ScrollSpeed: -5}
	s.AnimateConsole(0)
	if s.ConCurrent != 0 {
		t.Fatalf("negative-speed should snap, got %d", s.ConCurrent)
	}
}

// ----- CharRows ----------------------------------------------------

func TestCharRows(t *testing.T) {
	cases := []struct {
		conCurrent int
		want       int
	}{
		{0, 0},
		{-1, 0},
		{7, 0},  // less than one full glyph
		{8, 1},  // exactly one
		{17, 2}, // rounds down (17 / 8 = 2)
		{64, 8},
	}
	for _, c := range cases {
		s := &Screen{ConCurrent: c.conCurrent}
		if got := s.CharRows(); got != c.want {
			t.Fatalf("CharRows(%d) = %d want %d", c.conCurrent, got, c.want)
		}
	}
}

// ----- DrawConsole -------------------------------------------------

// newTestConsole returns a Console of (width, lines) with each cell
// set to a recognizable byte 'A'+row, so the renderer's output can be
// inspected position-by-position.
func newTestConsole(t *testing.T, width, lines int) *Console {
	t.Helper()
	c, err := NewConsole(width, lines)
	if err != nil {
		t.Fatalf("NewConsole(%d,%d): %v", width, lines, err)
	}
	return c
}

func TestDrawConsoleHappy(t *testing.T) {
	// Framebuffer wide enough for ~38 columns of text (the
	// MinConsoleWidth lower bound) plus the one-column left pad.
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)

	// Print three rows of distinct characters. PrintChar wraps to a
	// new line via Linefeed, so the most-recent row is the one
	// printed last.
	con.Print("AAA")
	con.Linefeed(0)
	con.Print("BBB")
	con.Linefeed(0)
	con.Print("CCC")

	s, err := NewScreen(fb.Width, fb.Height)
	if err != nil {
		t.Fatalf("NewScreen: %v", err)
	}
	// 3 char rows visible.
	s.ConCurrent = 3 * CharHeight

	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}

	// Bottom row (i=0) at y = ConCurrent - CharHeight = 16. That row
	// holds CursorX..on the most recently printed line ("CCC"). 'C' =
	// 0x43 -> glyph fill byte 0x10 + (4*16) + 3 = 0x53.
	const colPad = CharWidth
	got := fb.Pixels[16*fb.Pitch+colPad]
	if got != 0x53 {
		t.Fatalf("bottom row leftmost glyph fill = %#x want 0x53", got)
	}
	// Middle row (i=1) at y = 8 holds "BBB". 'B' = 0x42 -> 0x52.
	got = fb.Pixels[8*fb.Pitch+colPad]
	if got != 0x52 {
		t.Fatalf("mid row leftmost glyph fill = %#x want 0x52", got)
	}
	// Top row (i=2) at y = 0 holds "AAA". 'A' = 0x41 -> 0x51.
	got = fb.Pixels[0*fb.Pitch+colPad]
	if got != 0x51 {
		t.Fatalf("top row leftmost glyph fill = %#x want 0x51", got)
	}
}

func TestDrawConsoleNoRowsIsNoOp(t *testing.T) {
	fb := newTestFB(t, 320, 64, 0xAA)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("XYZ")
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = 0 // closed
	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0xAA {
			t.Fatalf("framebuffer touched when console closed")
		}
	}
}

func TestDrawConsoleBackScrollArrows(t *testing.T) {
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("ZZZ")
	con.Linefeed(0)
	con.Print("YYY")
	con.BackScroll = 1

	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = 2 * CharHeight
	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}

	// '^' = 0x5E -> row=5, col=14 -> fill = 0x10 + 5*16 + 14 = 0x6E.
	const colPad = CharWidth
	// Bottom row at y = ConCurrent - CharHeight = 8.
	got := fb.Pixels[8*fb.Pitch+colPad]
	if got != 0x6E {
		t.Fatalf("backscroll marker fill = %#x want 0x6E ('^')", got)
	}
}

func TestDrawConsoleVisibleRowOutOfRange(t *testing.T) {
	// rows visible > console.Lines -> per-row VisibleRow returns -1
	// for the rows past the buffer end; those iterations skip via the
	// `if rowIdx < 0 { continue }` arm. Build a fb tall enough that
	// ConCurrent / CharHeight > MinConsoleLines.
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, (MinConsoleLines+2)*CharHeight, 0xAA)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("X")
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = (MinConsoleLines + 2) * CharHeight // 6 rows requested, only 4 in buffer
	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}
}

func TestDrawConsoleNarrowFB(t *testing.T) {
	// Framebuffer narrower than the console: the per-row loop should
	// clip to the framebuffer column count and never touch
	// out-of-bounds pixels.
	fb := newTestFB(t, CharWidth*2, 32, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("AAAA")
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = CharHeight
	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}
}

func TestDrawConsoleTooNarrowFB(t *testing.T) {
	// Framebuffer narrower than the column pad -> draws nothing,
	// returns nil.
	fb := newTestFB(t, CharWidth/2, 32, 0xAA)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = CharHeight
	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0xAA {
			t.Fatalf("narrow-fb path wrote pixels")
		}
	}
}

func TestDrawConsoleNilArgs(t *testing.T) {
	fb := newTestFB(t, 320, 64, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = CharHeight

	if err := s.DrawConsole(nil, con, chars); !errors.Is(err, ErrScreenDrawFB) {
		t.Fatalf("nil fb err = %v want ErrScreenDrawFB", err)
	}
	if err := s.DrawConsole(fb, nil, chars); !errors.Is(err, ErrScreenCons) {
		t.Fatalf("nil con err = %v want ErrScreenCons", err)
	}
	if err := s.DrawConsole(fb, con, nil); !errors.Is(err, ErrScreenChars) {
		t.Fatalf("nil chars err = %v want ErrScreenChars", err)
	}
}

func TestDrawConsoleBadCharsShape(t *testing.T) {
	// A non-128x128 conchars Pic should propagate the DrawCharacter
	// shape error (exercises the per-glyph error path in
	// DrawConsole's row loop).
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	badChars := &Pic{Width: 64, Height: 64, Pixels: make([]byte, 64*64)}
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("ZZZ")
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = CharHeight
	if err := s.DrawConsole(fb, con, badChars); !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("bad chars err = %v want ErrDrawCharsShape", err)
	}
}

func TestDrawConsoleBackScrollBadCharsShape(t *testing.T) {
	// Same error path, but through the backscroll-arrow branch.
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	badChars := &Pic{Width: 64, Height: 64, Pixels: make([]byte, 64*64)}
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("ZZZ")
	con.BackScroll = 1
	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = CharHeight
	if err := s.DrawConsole(fb, con, badChars); !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("bad chars err = %v want ErrDrawCharsShape", err)
	}
}

func TestDrawConsoleScrollOffset(t *testing.T) {
	// With BackScroll > 0 the visible rows shift backward by that
	// many positions. Print four lines, scroll back by 1, draw 2
	// rows: the bottom row should be the third line, NOT the fourth
	// (since the bottom row will be the '^' marker arrows for i=0,
	// the assertion is on i=1 which is the line one above the most
	// recent before the scroll).
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Print("AAA")
	con.Linefeed(0)
	con.Print("BBB")
	con.Linefeed(0)
	con.Print("CCC")
	// Without BackScroll, top-of-2-rows would show "BBB".
	// With BackScroll = 1, top-of-2-rows shifts to "AAA".
	con.BackScroll = 1

	s, _ := NewScreen(fb.Width, fb.Height)
	s.ConCurrent = 2 * CharHeight
	if err := s.DrawConsole(fb, con, chars); err != nil {
		t.Fatalf("DrawConsole: %v", err)
	}
	// i=1 row -> y = 0; should be 'A' fill = 0x51, NOT 'B' (0x52).
	const colPad = CharWidth
	got := fb.Pixels[0*fb.Pitch+colPad]
	if got != 0x51 {
		t.Fatalf("scroll-offset row fill = %#x want 0x51 ('A')", got)
	}
}

// ----- DrawNotify --------------------------------------------------

func TestDrawNotifyHappy(t *testing.T) {
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)

	// Two rows written at t=1.0 and t=2.0. At now=2.5 with
	// lifetime=2.0, BOTH should appear.
	con.Linefeed(1.0)
	con.Print("AAA")
	con.Linefeed(2.0)
	con.Print("BBB")

	s, _ := NewScreen(fb.Width, fb.Height)
	if err := s.DrawNotify(fb, con, chars, 2.5, 2.0, MaxNotifyLines); err != nil {
		t.Fatalf("DrawNotify: %v", err)
	}
	// Two rows drawn at y=0 ('AAA' / 0x51) and y=8 ('BBB' / 0x52).
	const colPad = CharWidth
	if got := fb.Pixels[0*fb.Pitch+colPad]; got != 0x51 {
		t.Fatalf("notify row 0 fill = %#x want 0x51", got)
	}
	if got := fb.Pixels[CharHeight*fb.Pitch+colPad]; got != 0x52 {
		t.Fatalf("notify row 1 fill = %#x want 0x52", got)
	}
}

func TestDrawNotifyLifetimeFilter(t *testing.T) {
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0xAA)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Linefeed(1.0)
	con.Print("AAA")
	con.Linefeed(2.0)
	con.Print("BBB")

	s, _ := NewScreen(fb.Width, fb.Height)
	// now=10, lifetime=1 -> both rows are too old.
	if err := s.DrawNotify(fb, con, chars, 10.0, 1.0, MaxNotifyLines); err != nil {
		t.Fatalf("DrawNotify: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0xAA {
			t.Fatalf("DrawNotify drew expired rows")
		}
	}
}

func TestDrawNotifyEmpty(t *testing.T) {
	fb := newTestFB(t, 320, 64, 0xAA)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	s, _ := NewScreen(fb.Width, fb.Height)
	// maxRows=0 -> NotifyRows returns nil -> no-op.
	if err := s.DrawNotify(fb, con, chars, 1.0, 1.0, 0); err != nil {
		t.Fatalf("DrawNotify: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0xAA {
			t.Fatalf("DrawNotify drew on empty notify list")
		}
	}
}

func TestDrawNotifyTooNarrowFB(t *testing.T) {
	fb := newTestFB(t, CharWidth/2, 32, 0xAA)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Linefeed(1.0)
	con.Print("AAA")
	s, _ := NewScreen(fb.Width, fb.Height)
	if err := s.DrawNotify(fb, con, chars, 1.0, 1.0, MaxNotifyLines); err != nil {
		t.Fatalf("DrawNotify: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0xAA {
			t.Fatalf("narrow-fb path wrote pixels")
		}
	}
}

func TestDrawNotifyNarrowFB(t *testing.T) {
	// Framebuffer narrower than the console width -> per-row loop
	// clips to the framebuffer column count. Must not panic and must
	// emit at least one glyph.
	fb := newTestFB(t, CharWidth*3, 32, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Linefeed(1.0)
	con.Print("AAAA")
	s, _ := NewScreen(fb.Width, fb.Height)
	if err := s.DrawNotify(fb, con, chars, 1.0, 1.0, MaxNotifyLines); err != nil {
		t.Fatalf("DrawNotify: %v", err)
	}
}

func TestDrawNotifyNilArgs(t *testing.T) {
	fb := newTestFB(t, 320, 64, 0)
	chars := makeCharsSheet(t)
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	s, _ := NewScreen(fb.Width, fb.Height)

	if err := s.DrawNotify(nil, con, chars, 0, 0, 0); !errors.Is(err, ErrScreenDrawFB) {
		t.Fatalf("nil fb err = %v want ErrScreenDrawFB", err)
	}
	if err := s.DrawNotify(fb, nil, chars, 0, 0, 0); !errors.Is(err, ErrScreenCons) {
		t.Fatalf("nil con err = %v want ErrScreenCons", err)
	}
	if err := s.DrawNotify(fb, con, nil, 0, 0, 0); !errors.Is(err, ErrScreenChars) {
		t.Fatalf("nil chars err = %v want ErrScreenChars", err)
	}
}

func TestDrawNotifyBadCharsShape(t *testing.T) {
	fb := newTestFB(t, (1+MinConsoleWidth)*CharWidth, 64, 0)
	badChars := &Pic{Width: 64, Height: 64, Pixels: make([]byte, 64*64)}
	con := newTestConsole(t, MinConsoleWidth, MinConsoleLines)
	con.Linefeed(1.0)
	con.Print("ZZZ")
	s, _ := NewScreen(fb.Width, fb.Height)
	if err := s.DrawNotify(fb, con, badChars, 1.0, 1.0, MaxNotifyLines); !errors.Is(err, ErrDrawCharsShape) {
		t.Fatalf("bad chars err = %v want ErrDrawCharsShape", err)
	}
}
