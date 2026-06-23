// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package concharsfont

import "testing"

// TestBuild_Size asserts the produced sheet has the exact byte count
// the Quake engine's DrawCharacter path expects (128*128 = 16384).
func TestBuild_Size(t *testing.T) {
	buf := Build(0xDC, 0x67)
	if len(buf) != SheetSize {
		t.Fatalf("len(buf) = %d, want %d", len(buf), SheetSize)
	}
}

// cell returns the 8x8 byte slice for glyph ch out of a built sheet.
func cell(buf []byte, ch int) [CellSize][CellSize]byte {
	const stride = GridDim * CellSize
	col := ch % GridDim
	row := ch / GridDim
	baseY := row * CellSize
	baseX := col * CellSize
	var out [CellSize][CellSize]byte
	for py := 0; py < CellSize; py++ {
		for px := 0; px < CellSize; px++ {
			out[py][px] = buf[(baseY+py)*stride+baseX+px]
		}
	}
	return out
}

// TestBuild_SpaceIsBlank verifies the ASCII 0x20 (space) cell is fully
// transparent (all palette index 0). DrawCharacter treats 0 as
// transparent, so a blank space must not paint anything.
func TestBuild_SpaceIsBlank(t *testing.T) {
	buf := Build(0xDC, 0x67)
	c := cell(buf, 0x20)
	for py := 0; py < CellSize; py++ {
		for px := 0; px < CellSize; px++ {
			if c[py][px] != 0 {
				t.Fatalf("space cell pixel (%d,%d) = %d, want 0", px, py, c[py][px])
			}
		}
	}
}

// TestBuild_HasGlyphs asserts the cell for ASCII 'A' (0x41) contains
// the expected on-pixels from the inlined 'A' bitmap. The 'A' glyph
// row 0 is 0x18 (0001 1000) -> pixels 3,4 set.
func TestBuild_HasGlyphs(t *testing.T) {
	buf := Build(0xDC, 0x67)
	c := cell(buf, 0x41)
	// Row 0 of 'A' is 0x18 -> bits at columns 3 and 4.
	if c[0][3] != 0xDC || c[0][4] != 0xDC {
		t.Fatalf("'A' row 0 pixels (3,4) = (%d,%d), want (0xDC,0xDC)", c[0][3], c[0][4])
	}
	// Row 0 columns 0,1,2,5,6,7 must be zero (transparent).
	for _, px := range []int{0, 1, 2, 5, 6, 7} {
		if c[0][px] != 0 {
			t.Fatalf("'A' row 0 pixel %d = %d, want 0", px, c[0][px])
		}
	}
	// Count non-zero pixels in the whole 'A' cell; must exceed 10.
	nz := 0
	for py := 0; py < CellSize; py++ {
		for px := 0; px < CellSize; px++ {
			if c[py][px] != 0 {
				nz++
			}
		}
	}
	if nz < 10 {
		t.Fatalf("'A' cell non-zero pixel count = %d, want >= 10", nz)
	}
}

// TestBuild_HighRangeUsesYellow asserts cells in the upper half
// (ch >= 128) paint with fgHigh, not fgLow. Quake's stock conchars
// mirrors the lower half here as the "yellow" text bank.
func TestBuild_HighRangeUsesYellow(t *testing.T) {
	const fgLow, fgHigh = byte(0xDC), byte(0x67)
	buf := Build(fgLow, fgHigh)
	// 'A' at ch=0x41 painted with fgLow; mirror at ch=0x41+128=0xC1
	// painted with fgHigh (same glyph shape).
	cLow := cell(buf, 0x41)
	cHigh := cell(buf, 0xC1)
	if cLow[0][3] != fgLow {
		t.Fatalf("low 'A' pixel = %d, want %d", cLow[0][3], fgLow)
	}
	if cHigh[0][3] != fgHigh {
		t.Fatalf("high 'A' pixel = %d, want %d", cHigh[0][3], fgHigh)
	}
	// Bitmap shapes must match.
	for py := 0; py < CellSize; py++ {
		for px := 0; px < CellSize; px++ {
			loOn := cLow[py][px] != 0
			hiOn := cHigh[py][px] != 0
			if loOn != hiOn {
				t.Fatalf("shape mismatch at (%d,%d): low=%v high=%v", px, py, loOn, hiOn)
			}
		}
	}
}

// TestGlyph_OutOfRange covers the early-return branches of Glyph for
// non-printable codes (below 0x20 and above 0x7e), guaranteeing a
// blank bitmap. This pins the path Build relies on for ch in 0..31
// + 127 + 128..255 (after the -128 offset reuses the same lookup).
func TestGlyph_OutOfRange(t *testing.T) {
	for _, ch := range []byte{0x00, 0x1F, 0x7F, 0xFF} {
		g := Glyph(ch)
		for i, b := range g {
			if b != 0 {
				t.Fatalf("Glyph(0x%02x) byte %d = 0x%02x, want 0", ch, i, b)
			}
		}
	}
}

// TestGlyph_InRange covers the in-range branch of Glyph for a known
// printable glyph; complements TestGlyph_OutOfRange to pin both paths.
func TestGlyph_InRange(t *testing.T) {
	g := Glyph('A')
	// Row 0 of 'A' is 0x18 in the table.
	if g[0] != 0x18 {
		t.Fatalf("Glyph('A')[0] = 0x%02x, want 0x18", g[0])
	}
}

// TestBuild_AllGlyphsPaint sanity-checks that every printable ASCII
// glyph (32..126, excluding the deliberately-blank space) produces at
// least one non-zero pixel in its cell. Catches accidental zeroing of
// the font table at edit time.
func TestBuild_AllGlyphsPaint(t *testing.T) {
	buf := Build(0xDC, 0x67)
	for ch := 0x21; ch <= 0x7E; ch++ {
		c := cell(buf, ch)
		nz := 0
		for py := 0; py < CellSize; py++ {
			for px := 0; px < CellSize; px++ {
				if c[py][px] != 0 {
					nz++
				}
			}
		}
		if nz == 0 {
			t.Fatalf("printable glyph 0x%02x has zero on-pixels", ch)
		}
	}
}
