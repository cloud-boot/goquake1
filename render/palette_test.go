// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

// syntheticPaletteLump builds a deterministic 768-byte lump where
// entry i has RGB = (i, 255-i, i^0x5A). Lets each cell be verified
// against a closed-form expectation without hardcoding 256 triples.
func syntheticPaletteLump() []byte {
	lump := make([]byte, PaletteLumpSize)
	for i := 0; i < 256; i++ {
		lump[i*3+0] = byte(i)
		lump[i*3+1] = byte(255 - i)
		lump[i*3+2] = byte(i ^ 0x5A)
	}
	return lump
}

func TestLoadPaletteHappyPath(t *testing.T) {
	lump := syntheticPaletteLump()
	pal, err := LoadPalette(lump)
	if err != nil {
		t.Fatalf("LoadPalette: unexpected err: %v", err)
	}
	if pal == nil {
		t.Fatal("LoadPalette: nil palette on success")
	}
	// Spot-check a handful of cells across the table.
	for _, idx := range []int{0, 1, 17, 64, 128, 200, 254, 255} {
		got := pal[idx]
		want := [3]byte{byte(idx), byte(255 - idx), byte(idx ^ 0x5A)}
		if got != want {
			t.Errorf("pal[%d] = %v, want %v", idx, got, want)
		}
	}
}

func TestLoadPaletteShort(t *testing.T) {
	cases := [][]byte{
		make([]byte, PaletteLumpSize-1), // 767
		make([]byte, 0),                 // empty slice
		nil,                             // nil slice
	}
	for i, lump := range cases {
		pal, err := LoadPalette(lump)
		if !errors.Is(err, ErrPaletteLumpShort) {
			t.Errorf("case %d (len=%d): err = %v, want ErrPaletteLumpShort", i, len(lump), err)
		}
		if pal != nil {
			t.Errorf("case %d: palette = %v, want nil", i, pal)
		}
	}
}

func TestLoadPaletteLong(t *testing.T) {
	lump := make([]byte, PaletteLumpSize+1)
	pal, err := LoadPalette(lump)
	if !errors.Is(err, ErrPaletteLumpLong) {
		t.Errorf("err = %v, want ErrPaletteLumpLong", err)
	}
	if pal != nil {
		t.Errorf("palette = %v, want nil", pal)
	}
}

func TestPaletteLumpSizeConstant(t *testing.T) {
	if PaletteLumpSize != 256*3 {
		t.Fatalf("PaletteLumpSize = %d, want %d", PaletteLumpSize, 256*3)
	}
}
