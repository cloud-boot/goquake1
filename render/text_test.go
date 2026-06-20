// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestNewConsoleHappy(t *testing.T) {
	c, err := NewConsole(40, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Width != 40 || c.Lines != 8 {
		t.Errorf("dims: got (%d,%d) want (40,8)", c.Width, c.Lines)
	}
	if len(c.Buf) != 40*8 {
		t.Errorf("len(Buf): got %d want %d", len(c.Buf), 40*8)
	}
	if len(c.NotifyTime) != 8 {
		t.Errorf("len(NotifyTime): got %d want 8", len(c.NotifyTime))
	}
	for i, b := range c.Buf {
		if b != ' ' {
			t.Fatalf("Buf[%d] = %#x, want space", i, b)
		}
	}
	for i, n := range c.NotifyTime {
		if n != 0 {
			t.Fatalf("NotifyTime[%d] = %v, want 0", i, n)
		}
	}
	if c.CurrentRow != 0 || c.CursorX != 0 || c.BackScroll != 0 {
		t.Errorf("zero state: row=%d x=%d back=%d", c.CurrentRow, c.CursorX, c.BackScroll)
	}
}

func TestNewConsoleErrConDim(t *testing.T) {
	cases := []struct{ w, l int }{
		{0, 8},
		{40, 0},
		{-1, 8},
		{40, -1},
		{0, 0},
	}
	for _, tc := range cases {
		_, err := NewConsole(tc.w, tc.l)
		if !errors.Is(err, ErrConDim) {
			t.Errorf("NewConsole(%d,%d): got %v, want ErrConDim", tc.w, tc.l, err)
		}
	}
}

func TestNewConsoleErrConBadWidth(t *testing.T) {
	_, err := NewConsole(MinConsoleWidth-1, MinConsoleLines)
	if !errors.Is(err, ErrConBadWidth) {
		t.Errorf("got %v, want ErrConBadWidth", err)
	}
}

func TestNewConsoleErrConBadLines(t *testing.T) {
	_, err := NewConsole(MinConsoleWidth, MinConsoleLines-1)
	if !errors.Is(err, ErrConBadLines) {
		t.Errorf("got %v, want ErrConBadLines", err)
	}
}

func TestConsoleCell(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.Buf[2*40+5] = 'A'
	if got := c.Cell(5, 2); got != 'A' {
		t.Errorf("Cell(5,2): got %#x want 'A'", got)
	}
	// Out-of-bounds returns space.
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {40, 0}, {0, 8}, {100, 100}} {
		if got := c.Cell(p[0], p[1]); got != ' ' {
			t.Errorf("Cell(%d,%d): got %#x want space", p[0], p[1], got)
		}
	}
}

func TestConsoleSetCell(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.SetCell(3, 1, 'Z')
	if c.Buf[1*40+3] != 'Z' {
		t.Errorf("SetCell did not store: got %#x", c.Buf[1*40+3])
	}
	// Out-of-bounds is silent no-op.
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {40, 0}, {0, 8}} {
		c.SetCell(p[0], p[1], 'X')
	}
	// Buf untouched outside (3,1).
	for i, b := range c.Buf {
		if i == 1*40+3 {
			continue
		}
		if b != ' ' {
			t.Fatalf("Buf[%d] = %#x, want space (oob writes leaked)", i, b)
		}
	}
}

func TestPrintCharNormal(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.PrintChar('H')
	c.PrintChar('i')
	if c.Buf[0] != 'H' || c.Buf[1] != 'i' {
		t.Errorf("Buf[0..1] = %q %q want 'H' 'i'", c.Buf[0], c.Buf[1])
	}
	if c.CursorX != 2 {
		t.Errorf("CursorX = %d want 2", c.CursorX)
	}
	if c.CurrentRow != 0 {
		t.Errorf("CurrentRow = %d want 0", c.CurrentRow)
	}
}

func TestPrintCharNewline(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.PrintChar('A')
	c.PrintChar('\n')
	if c.CurrentRow != 1 {
		t.Errorf("CurrentRow = %d want 1", c.CurrentRow)
	}
	if c.CursorX != 0 {
		t.Errorf("CursorX = %d want 0", c.CursorX)
	}
}

func TestPrintCharCarriageReturn(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.PrintChar('A')
	c.PrintChar('B')
	c.PrintChar('\r')
	if c.CursorX != 0 {
		t.Errorf("CursorX = %d want 0", c.CursorX)
	}
	if c.CurrentRow != 0 {
		t.Errorf("CurrentRow = %d want 0", c.CurrentRow)
	}
}

func TestPrintCharTab(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.PrintChar('\t')
	if c.CursorX != 4 {
		t.Errorf("CursorX = %d want 4", c.CursorX)
	}
	for i := 0; i < 4; i++ {
		if c.Buf[i] != ' ' {
			t.Errorf("Buf[%d] = %#x want space", i, c.Buf[i])
		}
	}
}

func TestPrintCharNul(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.PrintChar(0)
	if c.CursorX != 0 {
		t.Errorf("CursorX = %d want 0", c.CursorX)
	}
	if c.CurrentRow != 0 {
		t.Errorf("CurrentRow = %d want 0", c.CurrentRow)
	}
}

func TestPrintCharWidthWrap(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	for i := 0; i < MinConsoleWidth; i++ {
		c.PrintChar('x')
	}
	// Should have wrapped via implicit Linefeed.
	if c.CurrentRow != 1 {
		t.Errorf("CurrentRow = %d want 1", c.CurrentRow)
	}
	if c.CursorX != 0 {
		t.Errorf("CursorX = %d want 0", c.CursorX)
	}
}

func TestPrint(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.Print("Hi")
	if c.Buf[0] != 'H' || c.Buf[1] != 'i' {
		t.Errorf("Buf[0..1] = %q %q", c.Buf[0], c.Buf[1])
	}
	if c.CursorX != 2 {
		t.Errorf("CursorX = %d want 2", c.CursorX)
	}
}

func TestPrintf(t *testing.T) {
	c, _ := NewConsole(40, 8)
	c.Printf("%s=%d", "x", 7)
	got := string(c.Buf[0:4])
	if got != "x=7 " && got != "x=7\x00" {
		// We expect "x=7 " (space already there) -- the prints write x, =, 7.
		if c.Buf[0] != 'x' || c.Buf[1] != '=' || c.Buf[2] != '7' {
			t.Errorf("Buf[0..3] = %q want 'x=7'", string(c.Buf[0:3]))
		}
	}
	if c.CursorX != 3 {
		t.Errorf("CursorX = %d want 3", c.CursorX)
	}
}

func TestLinefeedWraps(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	for i := 0; i < MinConsoleLines; i++ {
		c.Linefeed(float32(i + 1))
	}
	// After Lines linefeeds, CurrentRow should be back to 0.
	if c.CurrentRow != 0 {
		t.Errorf("CurrentRow = %d want 0 after %d linefeeds", c.CurrentRow, MinConsoleLines)
	}
}

func TestLinefeedClearsRow(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	// Dirty row 1 directly, then linefeed into it.
	for i := 0; i < MinConsoleWidth; i++ {
		c.Buf[1*MinConsoleWidth+i] = 'X'
	}
	c.Linefeed(0)
	if c.CurrentRow != 1 {
		t.Fatalf("CurrentRow = %d want 1", c.CurrentRow)
	}
	for i := 0; i < MinConsoleWidth; i++ {
		if c.Buf[1*MinConsoleWidth+i] != ' ' {
			t.Fatalf("Buf[1,%d] = %#x want space (Linefeed should clear)", i, c.Buf[1*MinConsoleWidth+i])
		}
	}
	if c.CursorX != 0 {
		t.Errorf("CursorX = %d want 0", c.CursorX)
	}
}

func TestLinefeedStampsNotifyTime(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	c.Linefeed(42.5)
	if c.NotifyTime[1] != 42.5 {
		t.Errorf("NotifyTime[1] = %v want 42.5", c.NotifyTime[1])
	}
}

func TestScrollUpClamping(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, 10)
	c.ScrollUp(3)
	if c.BackScroll != 3 {
		t.Errorf("BackScroll = %d want 3", c.BackScroll)
	}
	c.ScrollUp(9999)
	limit := 10 - MinConsoleLines
	if c.BackScroll != limit {
		t.Errorf("BackScroll = %d want %d (clamped)", c.BackScroll, limit)
	}
}

func TestScrollDownClamping(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, 10)
	c.ScrollUp(4)
	c.ScrollDown(2)
	if c.BackScroll != 2 {
		t.Errorf("BackScroll = %d want 2", c.BackScroll)
	}
	c.ScrollDown(9999)
	if c.BackScroll != 0 {
		t.Errorf("BackScroll = %d want 0 (clamped)", c.BackScroll)
	}
}

func TestClearResets(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	c.Print("hello")
	c.Linefeed(5)
	c.ScrollUp(0) // exercise the path; clear should still zero BackScroll
	c.BackScroll = 1
	c.Clear()
	if c.CursorX != 0 || c.CurrentRow != 0 || c.BackScroll != 0 {
		t.Errorf("Clear: cursor/row/back = %d/%d/%d want 0/0/0", c.CursorX, c.CurrentRow, c.BackScroll)
	}
	for i, b := range c.Buf {
		if b != ' ' {
			t.Fatalf("Buf[%d] = %#x want space after Clear", i, b)
		}
	}
	for i, n := range c.NotifyTime {
		if n != 0 {
			t.Fatalf("NotifyTime[%d] = %v want 0 after Clear", i, n)
		}
	}
}

func TestVisibleRowHappy(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	c.CurrentRow = 2
	// visualRow=0 => CurrentRow itself.
	if got := c.VisibleRow(0); got != 2 {
		t.Errorf("VisibleRow(0) = %d want 2", got)
	}
	// visualRow=1 => row above (1).
	if got := c.VisibleRow(1); got != 1 {
		t.Errorf("VisibleRow(1) = %d want 1", got)
	}
	// visualRow=3 => walk back past 0, wrap to Lines-1.
	if got := c.VisibleRow(3); got != MinConsoleLines-1 {
		t.Errorf("VisibleRow(3) = %d want %d (wrap)", got, MinConsoleLines-1)
	}
	// BackScroll shifts everything up.
	c.BackScroll = 1
	if got := c.VisibleRow(0); got != 1 {
		t.Errorf("VisibleRow(0) with BackScroll=1 = %d want 1", got)
	}
}

func TestVisibleRowOutOfRange(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	if got := c.VisibleRow(-1); got != -1 {
		t.Errorf("VisibleRow(-1) = %d want -1", got)
	}
	if got := c.VisibleRow(MinConsoleLines); got != -1 {
		t.Errorf("VisibleRow(Lines) = %d want -1", got)
	}
}

func TestNotifyRows(t *testing.T) {
	c, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	// Print 3 rows at different timestamps.
	c.Linefeed(1.0) // row 1
	c.Linefeed(2.0) // row 2
	c.Linefeed(3.0) // row 3
	// Now = 4.0, lifetime = 2.0: rows with t in (2.0, 4.0] match
	// i.e. rows stamped 3.0 and 2.0 (3-time delta = 1.0 and 2.0).
	got := c.NotifyRows(4.0, 2.0, MaxNotifyLines)
	if len(got) != 2 {
		t.Fatalf("got %d rows %v want 2 (rows 2 and 3)", len(got), got)
	}
	// Oldest-first.
	if got[0] != 2 || got[1] != 3 {
		t.Errorf("got %v want [2 3] (oldest-first)", got)
	}

	// Lifetime 10 picks up all 3 stamped rows.
	got = c.NotifyRows(4.0, 10.0, MaxNotifyLines)
	if len(got) != 3 {
		t.Errorf("lifetime=10: got %d want 3 (%v)", len(got), got)
	}

	// maxRows=0 -> nil.
	if g := c.NotifyRows(4.0, 10.0, 0); g != nil {
		t.Errorf("maxRows=0: got %v want nil", g)
	}

	// maxRows > MaxNotifyLines is clamped.
	got = c.NotifyRows(4.0, 10.0, MaxNotifyLines*10)
	if len(got) > MaxNotifyLines {
		t.Errorf("maxRows clamp: got %d > MaxNotifyLines=%d", len(got), MaxNotifyLines)
	}

	// Lifetime 0.1: delta from 4.0 to 3.0 = 1.0 > 0.1, so none
	// match -- exercises the "now-t > lifetime" continue branch.
	got = c.NotifyRows(4.0, 0.1, MaxNotifyLines)
	if len(got) != 0 {
		t.Errorf("lifetime=0.1: got %v want []", got)
	}

	// Wrap branch: CurrentRow=0 with maxRows=4 forces (0-3)%Lines
	// to be negative and exercise the `r += c.Lines` correction.
	wrap, _ := NewConsole(MinConsoleWidth, MinConsoleLines)
	wrap.CurrentRow = 0
	wrap.NotifyTime[MinConsoleLines-1] = 5.0 // stamp the wrap target
	got = wrap.NotifyRows(5.0, 10.0, MaxNotifyLines)
	found := false
	for _, r := range got {
		if r == MinConsoleLines-1 {
			found = true
		}
	}
	if !found {
		t.Errorf("wrap: row %d not in %v", MinConsoleLines-1, got)
	}
}
