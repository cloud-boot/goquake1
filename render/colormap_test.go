// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

// syntheticColorMapLump builds a deterministic 16384-byte colormap
// where cell [row][col] = byte((row*7 + col*31) & 0xFF). Closed-form
// so each cell can be verified without a hand-coded fixture.
func syntheticColorMapLump() []byte {
	lump := make([]byte, ColorMapLumpSize)
	for row := 0; row < ColorMapRows; row++ {
		for col := 0; col < ColorMapCols; col++ {
			lump[row*ColorMapCols+col] = byte((row*7 + col*31) & 0xFF)
		}
	}
	return lump
}

func TestLoadColorMapHappyPath(t *testing.T) {
	lump := syntheticColorMapLump()
	cm, err := LoadColorMap(lump)
	if err != nil {
		t.Fatalf("LoadColorMap: unexpected err: %v", err)
	}
	if cm == nil {
		t.Fatal("LoadColorMap: nil colormap on success")
	}
	// Spot-check cells across the rows and columns.
	for _, rc := range [][2]int{
		{0, 0}, {0, 255}, {1, 1}, {17, 64},
		{31, 128}, {32, 192}, {63, 0}, {63, 255},
	} {
		row, col := rc[0], rc[1]
		got := cm[row][col]
		want := byte((row*7 + col*31) & 0xFF)
		if got != want {
			t.Errorf("cm[%d][%d] = 0x%02x, want 0x%02x", row, col, got, want)
		}
	}
}

func TestLoadColorMapVanillaTrailer(t *testing.T) {
	// 16385-byte lump: 16384 of payload + 1 trailing metadata byte.
	// LoadColorMap must accept and drop the trailer.
	payload := syntheticColorMapLump()
	lump := make([]byte, ColorMapLumpSize+1)
	copy(lump, payload)
	lump[ColorMapLumpSize] = 0xAB // trailer
	cm, err := LoadColorMap(lump)
	if err != nil {
		t.Fatalf("LoadColorMap: unexpected err: %v", err)
	}
	if cm == nil {
		t.Fatal("LoadColorMap: nil colormap on success")
	}
	// Last payload cell must still be the synthetic value, not 0xAB.
	want := byte((63*7 + 255*31) & 0xFF)
	if cm[63][255] != want {
		t.Errorf("cm[63][255] = 0x%02x, want 0x%02x (trailer not dropped)", cm[63][255], want)
	}
}

func TestLoadColorMapShort(t *testing.T) {
	cases := [][]byte{
		make([]byte, ColorMapLumpSize-1),
		make([]byte, 0),
		nil,
	}
	for i, lump := range cases {
		cm, err := LoadColorMap(lump)
		if !errors.Is(err, ErrColorMapLumpShort) {
			t.Errorf("case %d (len=%d): err = %v, want ErrColorMapLumpShort", i, len(lump), err)
		}
		if cm != nil {
			t.Errorf("case %d: colormap = %v, want nil", i, cm)
		}
	}
}

func TestLoadColorMapLong(t *testing.T) {
	lump := make([]byte, ColorMapLumpSize+2) // 1 past the accepted trailer
	cm, err := LoadColorMap(lump)
	if !errors.Is(err, ErrColorMapLumpLong) {
		t.Errorf("err = %v, want ErrColorMapLumpLong", err)
	}
	if cm != nil {
		t.Errorf("colormap = %v, want nil", cm)
	}
}

func TestColorMapConstants(t *testing.T) {
	if ColorMapRows != 64 {
		t.Errorf("ColorMapRows = %d, want 64", ColorMapRows)
	}
	if ColorMapCols != 256 {
		t.Errorf("ColorMapCols = %d, want 256", ColorMapCols)
	}
	if ColorMapLumpSize != 64*256 {
		t.Errorf("ColorMapLumpSize = %d, want %d", ColorMapLumpSize, 64*256)
	}
}

func TestColorMapLightIndex(t *testing.T) {
	lump := syntheticColorMapLump()
	cm, err := LoadColorMap(lump)
	if err != nil {
		t.Fatalf("LoadColorMap: %v", err)
	}
	// light=0 returns the row-0 entry.
	if got, want := cm.LightIndex(0, 42), cm[0][42]; got != want {
		t.Errorf("LightIndex(0, 42) = 0x%02x, want 0x%02x", got, want)
	}
	// light=63 returns the row-63 entry.
	if got, want := cm.LightIndex(63, 200), cm[63][200]; got != want {
		t.Errorf("LightIndex(63, 200) = 0x%02x, want 0x%02x", got, want)
	}
	// light=-5 clamps to 0.
	if got, want := cm.LightIndex(-5, 17), cm[0][17]; got != want {
		t.Errorf("LightIndex(-5, 17) = 0x%02x, want 0x%02x (no clamp to 0)", got, want)
	}
	// light=999 clamps to 63.
	if got, want := cm.LightIndex(999, 17), cm[63][17]; got != want {
		t.Errorf("LightIndex(999, 17) = 0x%02x, want 0x%02x (no clamp to 63)", got, want)
	}
	// In-range mid value passes through.
	if got, want := cm.LightIndex(32, 128), cm[32][128]; got != want {
		t.Errorf("LightIndex(32, 128) = 0x%02x, want 0x%02x", got, want)
	}
}
