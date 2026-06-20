// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// PaletteLumpSize is the on-disk byte count of the gfx.wad PALETTE
// lump: 256 entries * 3 bytes (R, G, B) = 768. (tyrquake: the in-
// memory `host_basepal` is initialised in Host_LoadPalettes via
// COM_LoadHunkFile("gfx/palette.lmp"); the file is the raw 768-byte
// triplet table with no header and no terminator.)
const PaletteLumpSize = 768

// Sentinel errors returned by LoadPalette.
var (
	ErrPaletteLumpShort = errors.New("render: PALETTE lump shorter than 768 bytes")
	ErrPaletteLumpLong  = errors.New("render: PALETTE lump longer than 768 bytes")
)

// LoadPalette parses raw bytes from the gfx.wad PALETTE lump into a
// *Palette. The on-disk format is 768 bytes: 256 sequential (R, G, B)
// triplets, no header, no terminator. (tyrquake: the in-memory
// `host_basepal` initialised via COM_LoadHunkFile("gfx/palette.lmp").)
//
// Returns:
//
//	ErrPaletteLumpShort  if len(lump) < PaletteLumpSize
//	ErrPaletteLumpLong   if len(lump) > PaletteLumpSize
//	nil + populated palette on success.
func LoadPalette(lump []byte) (*Palette, error) {
	switch {
	case len(lump) < PaletteLumpSize:
		return nil, ErrPaletteLumpShort
	case len(lump) > PaletteLumpSize:
		return nil, ErrPaletteLumpLong
	}
	pal := new(Palette)
	for i := 0; i < 256; i++ {
		pal[i][0] = lump[i*3+0]
		pal[i][1] = lump[i*3+1]
		pal[i][2] = lump[i*3+2]
	}
	return pal, nil
}
