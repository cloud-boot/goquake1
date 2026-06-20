// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"fmt"
)

// Error sentinels returned by the console constructor.
var (
	ErrConDim      = errors.New("render: console dimensions must be positive")
	ErrConBadWidth = errors.New("render: console width too small")
	ErrConBadLines = errors.New("render: console lines too small")
)

// MinConsoleWidth + MinConsoleLines are the practical lower bounds on
// console dimensions. tyrquake hard-codes 38 cols on the smallest
// video mode (see Con_Resize's `width < 1` fall-back in console.c);
// the renderer's notify-line overlay needs at least 4 scrollback rows
// to behave like the upstream NUM_CON_TIMES window.
const (
	MinConsoleWidth = 38
	MinConsoleLines = 4
)

// MaxNotifyLines is the default upper bound on the number of recent
// scrollback rows the notify-line overlay walks. Matches tyrquake's
// NUM_CON_TIMES (the size of con_times[] in console.c). Callers may
// pass a smaller cap to [Console.NotifyRows].
const MaxNotifyLines = 4

// Console is the fixed-grid scrollback buffer Quake 1 uses for its
// drop-down developer console + the per-frame notify-line overlay.
// Each cell is a byte (palette-indexed glyph slot; the top bit set
// selects the Color High half of conchars, a yellow/orange variant of
// the same glyph). This struct is the PURE state half -- the renderer
// (the DrawCharacter loop in draw.go) reads Buf + CursorX + CurrentRow
// and emits glyphs into the [FrameBuffer]; no rendering happens here.
//
// tyrquake: console_t in console.h, backed by a flat byte[] of size
// con_totallines*con_linewidth (the upstream pre-allocates a fixed
// CON_TEXTSIZE arena and re-derives totallines = CON_TEXTSIZE / width
// in Con_Resize). We size Buf to Lines*Width directly -- callers pick
// the dimensions up front, and re-sizing means allocating a fresh
// Console (Quake's Con_Resize is a video-mode-change concern that
// will live in the renderer's mode-switch path, not here).
type Console struct {
	Width      int       // columns per line
	Lines      int       // total scrollback rows
	Buf        []byte    // row-major, len == Lines*Width
	CurrentRow int       // most recently written row index (0..Lines-1)
	CursorX    int       // column of the next char in CurrentRow (0..Width)
	BackScroll int       // rows scrolled up from CurrentRow (0 = bottom)
	NotifyTime []float32 // len == Lines; per-row "this line is new" timestamp
}

// NewConsole returns a fresh console with the given dimensions, all
// cells = ' ' (0x20), CursorX=0, CurrentRow=0, BackScroll=0,
// NotifyTime all zero.
//
// Errors: ErrConDim if width <= 0 OR lines <= 0; ErrConBadWidth if
// width < MinConsoleWidth; ErrConBadLines if lines < MinConsoleLines.
func NewConsole(width, lines int) (*Console, error) {
	if width <= 0 || lines <= 0 {
		return nil, ErrConDim
	}
	if width < MinConsoleWidth {
		return nil, ErrConBadWidth
	}
	if lines < MinConsoleLines {
		return nil, ErrConBadLines
	}
	buf := make([]byte, lines*width)
	for i := range buf {
		buf[i] = ' '
	}
	return &Console{
		Width:      width,
		Lines:      lines,
		Buf:        buf,
		NotifyTime: make([]float32, lines),
	}, nil
}

// Cell returns the byte at (col, row). row is the absolute scrollback
// row index 0..Lines-1 (NOT relative to CurrentRow). Out-of-bounds
// (col, row) returns space (' '), not an error -- the renderer reads
// these in hot loops and we want safe defaults.
func (c *Console) Cell(col, row int) byte {
	if col < 0 || col >= c.Width || row < 0 || row >= c.Lines {
		return ' '
	}
	return c.Buf[row*c.Width+col]
}

// SetCell writes one byte at (col, row). Out-of-bounds is a silent
// no-op (same safety policy as [Console.Cell]).
func (c *Console) SetCell(col, row int, ch byte) {
	if col < 0 || col >= c.Width || row < 0 || row >= c.Lines {
		return
	}
	c.Buf[row*c.Width+col] = ch
}

// PrintChar writes a single character at (CursorX, CurrentRow),
// advances CursorX, and handles:
//
//   - ch == '\n'   -> implicit Linefeed (advance to next row,
//     CursorX = 0)
//   - ch == '\r'   -> set CursorX = 0 (no row advance) -- tyrquake's
//     CR semantics (Con_Print's `case '\r'` branch)
//   - ch == '\t'   -> emit 4 spaces (tyrquake's tab convention)
//   - ch == 0      -> silently dropped (NUL terminator from C strings)
//   - any other ch -> store at (CursorX, CurrentRow), advance CursorX
//   - if CursorX >= Width after advance -> implicit Linefeed
//
// tyrquake: Con_Print's main per-character loop.
func (c *Console) PrintChar(ch byte) {
	switch ch {
	case 0:
		return
	case '\n':
		c.Linefeed(0)
		return
	case '\r':
		c.CursorX = 0
		return
	case '\t':
		for i := 0; i < 4; i++ {
			c.PrintChar(' ')
		}
		return
	}
	c.Buf[c.CurrentRow*c.Width+c.CursorX] = ch
	c.CursorX++
	if c.CursorX >= c.Width {
		c.Linefeed(0)
	}
}

// Print writes a string via repeated [Console.PrintChar]. Convenience
// wrapper around the per-character loop.
func (c *Console) Print(s string) {
	for i := 0; i < len(s); i++ {
		c.PrintChar(s[i])
	}
}

// Printf is [fmt.Sprintf] composed with [Console.Print]. tyrquake:
// Con_Printf.
func (c *Console) Printf(format string, args ...any) {
	c.Print(fmt.Sprintf(format, args...))
}

// Linefeed advances CurrentRow by one (mod Lines), clears the new
// CurrentRow to all spaces, resets CursorX, and stamps
// NotifyTime[newRow] = now. The wrap is the scrollback's natural
// circular-buffer rotation -- once the buffer is full, the oldest row
// is overwritten and the renderer (driven by [Console.VisibleRow])
// scrolls accordingly.
//
// `now` is wall-clock-like time so the notify-line overlay knows when
// each row was last written.
func (c *Console) Linefeed(now float32) {
	c.CurrentRow = (c.CurrentRow + 1) % c.Lines
	c.CursorX = 0
	row := c.CurrentRow * c.Width
	for i := 0; i < c.Width; i++ {
		c.Buf[row+i] = ' '
	}
	c.NotifyTime[c.CurrentRow] = now
}

// ScrollUp moves BackScroll up by `n` rows (clamped to a maximum
// equal to Lines-MinConsoleLines, so the bottom MinConsoleLines rows
// can always render). tyrquake: the PgUp keybind (the upstream's
// con->display decrement in Key_Console).
func (c *Console) ScrollUp(n int) {
	c.BackScroll += n
	limit := c.Lines - MinConsoleLines
	if c.BackScroll > limit {
		c.BackScroll = limit
	}
}

// ScrollDown moves BackScroll down by `n` rows (clamped to 0).
// tyrquake: PgDn.
func (c *Console) ScrollDown(n int) {
	c.BackScroll -= n
	if c.BackScroll < 0 {
		c.BackScroll = 0
	}
}

// Clear resets the entire buffer to spaces and zeros CursorX,
// CurrentRow, BackScroll, NotifyTime. tyrquake: Con_Clear_f.
func (c *Console) Clear() {
	for i := range c.Buf {
		c.Buf[i] = ' '
	}
	for i := range c.NotifyTime {
		c.NotifyTime[i] = 0
	}
	c.CursorX = 0
	c.CurrentRow = 0
	c.BackScroll = 0
}

// VisibleRow returns the absolute row index that corresponds to
// `visualRow` from the bottom of the displayed console (0 = bottom).
// Walks (CurrentRow - visualRow - BackScroll) mod Lines. Used by the
// renderer's DrawConsole loop in a follow-up batch.
//
// Returns -1 if visualRow is negative or >= Lines.
func (c *Console) VisibleRow(visualRow int) int {
	if visualRow < 0 || visualRow >= c.Lines {
		return -1
	}
	r := (c.CurrentRow - visualRow - c.BackScroll) % c.Lines
	if r < 0 {
		r += c.Lines
	}
	return r
}

// NotifyRows returns the indices of the most recent rows whose
// NotifyTime is within `lifetime` of `now`. Used by the notify-line
// overlay (the per-row pop-up display that fades after a few seconds).
// Walks at most maxRows rows back from CurrentRow; maxRows is further
// clamped to [MaxNotifyLines] -- tyrquake's NUM_CON_TIMES window.
//
// Rows are returned oldest-first (the same draw order Con_DrawNotify
// uses in console.c).
//
// tyrquake: Con_DrawNotify's NotifyTime threshold check.
func (c *Console) NotifyRows(now, lifetime float32, maxRows int) []int {
	if maxRows <= 0 {
		return nil
	}
	if maxRows > MaxNotifyLines {
		maxRows = MaxNotifyLines
	}
	// maxRows is now <= MaxNotifyLines (4), and Lines >=
	// MinConsoleLines (also 4), so maxRows <= c.Lines holds and no
	// Lines-clamp is needed.
	out := make([]int, 0, maxRows)
	// Walk oldest-first: start (maxRows-1) rows back, end at CurrentRow.
	for i := maxRows - 1; i >= 0; i-- {
		r := (c.CurrentRow - i) % c.Lines
		if r < 0 {
			r += c.Lines
		}
		t := c.NotifyTime[r]
		if t == 0 {
			continue
		}
		if now-t > lifetime {
			continue
		}
		out = append(out, r)
	}
	return out
}
