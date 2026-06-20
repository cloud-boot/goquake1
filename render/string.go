// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

// HighBitMask is the bit OR'd into a character byte to select the
// alternate (yellow) glyph variant in the 256-cell conchars sheet:
// indices 0..127 are the white set; indices 128..255 are the yellow
// recolor. tyrquake convention.
const HighBitMask byte = 0x80

// DrawString blits a string at (x, y) using the white glyph set.
// Walks the bytes of s and calls [DrawCharacter] for each. Stops at
// the first DrawCharacter error and propagates it. Clipping is
// per-character (DrawCharacter handles out-of-range positions).
//
// Each glyph advances [CharWidth] horizontally. tyrquake: the
// loop body of Draw_String.
func DrawString(fb *FrameBuffer, chars *Pic, x, y int, s string) error {
	for i := 0; i < len(s); i++ {
		if err := DrawCharacter(fb, chars, x+i*CharWidth, y, s[i]); err != nil {
			return err
		}
	}
	return nil
}

// DrawColorString is DrawString with HighBitMask OR'd into each
// character, selecting the alternate (yellow) conchars variant.
// tyrquake: Draw_String when called with the high-bit-set strings
// the menu uses for highlighted entries.
func DrawColorString(fb *FrameBuffer, chars *Pic, x, y int, s string) error {
	for i := 0; i < len(s); i++ {
		if err := DrawCharacter(fb, chars, x+i*CharWidth, y, s[i]|HighBitMask); err != nil {
			return err
		}
	}
	return nil
}

// DrawCenteredString blits s horizontally centred on `centerX` at
// row y. The string's pixel width is len(s) * CharWidth; the left
// edge is centerX - width/2. Walks each byte and DrawCharacters it.
// Propagates the first DrawCharacter error.
//
// tyrquake: the centred-string shortcut Sbar_DrawCenterString uses
// for intermission text + the centerprint overlay.
func DrawCenteredString(fb *FrameBuffer, chars *Pic, centerX, y int, s string) error {
	width := len(s) * CharWidth
	left := centerX - width/2
	return DrawString(fb, chars, left, y, s)
}
