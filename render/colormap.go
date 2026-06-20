// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// ColorMapRows is the colormap's light-level row count (64 entries
// indexed 0..63; 0 = full lit, 63 = darkest). Matches the upstream
// VID_GRADES = (1 << VID_CBITS) = 64 in tyrquake's include/vid.h.
const ColorMapRows = 64

// ColorMapCols is the number of source palette indices per row
// (256). Each cell is the destination palette index after applying
// the row's light level to the column's source palette index.
const ColorMapCols = 256

// ColorMapLumpSize is the canonical on-disk byte count of the gfx.wad
// COLORMAP lump: 64 rows * 256 columns * 1 byte = 16384. tyrquake's
// COM_LoadHunkFile("gfx/colormap.lmp") accepts whatever the disk file
// contains and does not size-check; vanilla id-shipped gfx.wad uses a
// 16385-byte lump where the trailing byte stores the so-called
// "fullbright" / translucency marker (vid.fullbright is computed at
// `(int *)vid.colormap + 2048`, which sits inside the 16384-byte
// table, so the trailer is metadata not lookup data). LoadColorMap
// accepts BOTH 16384 (stripped) and 16385 (vanilla-with-trailer) and
// drops the trailer when present.
const ColorMapLumpSize = ColorMapRows * ColorMapCols

// colorMapLumpSizeWithTrailer is the vanilla id-shipped lump size:
// the 16384-byte lookup table plus one trailing metadata byte.
const colorMapLumpSizeWithTrailer = ColorMapLumpSize + 1

// ColorMap is the precomputed lit-palette table. Rows are light
// levels 0..63 (0 = full bright); columns are source palette indices
// 0..255. Cell [light][src] = the destination palette index when
// rendering the texel `src` under light `light`. Software lighting
// is a single byte lookup -- no per-pixel multiplies.
//
// (tyrquake: `vid.colormap` / `host_colormap` in include/vid.h and
// the load site in NQ/host.c's Host_LoadPalettes.)
type ColorMap [ColorMapRows][ColorMapCols]byte

// Sentinel errors returned by LoadColorMap.
var (
	ErrColorMapLumpShort = errors.New("render: COLORMAP lump shorter than 16384 bytes")
	ErrColorMapLumpLong  = errors.New("render: COLORMAP lump longer than 16385 bytes")
)

// LoadColorMap parses raw bytes from the gfx.wad COLORMAP lump into
// a *ColorMap. The on-disk format is row-major: 64 rows of 256 bytes
// (16384 total), optionally followed by a single trailing metadata
// byte (the vanilla id-shipped lump is 16385 bytes). Both sizes are
// accepted; the trailer is dropped.
//
// Returns:
//
//	ErrColorMapLumpShort  if len(lump) < ColorMapLumpSize
//	ErrColorMapLumpLong   if len(lump) > ColorMapLumpSize + 1
//	nil + populated colormap on success.
func LoadColorMap(lump []byte) (*ColorMap, error) {
	switch {
	case len(lump) < ColorMapLumpSize:
		return nil, ErrColorMapLumpShort
	case len(lump) > colorMapLumpSizeWithTrailer:
		return nil, ErrColorMapLumpLong
	}
	cm := new(ColorMap)
	for row := 0; row < ColorMapRows; row++ {
		copy(cm[row][:], lump[row*ColorMapCols:(row+1)*ColorMapCols])
	}
	return cm, nil
}

// LightIndex returns the lit palette entry for src under light.
// Equivalent to cm[light][src], with light clamped to [0, 63] -- the
// renderer can pass any int from its light arithmetic; the clamp is
// the safe behavior the C upstream relies on at every lookup site.
//
// (The C upstream's r_drawsurf.c clamps light into a 6-bit range via
// `lightleft = lightleftstep = ... >> 8` arithmetic. The Go port
// surfaces the clamp explicitly.)
func (cm *ColorMap) LightIndex(light int, src byte) byte {
	if light < 0 {
		light = 0
	} else if light >= ColorMapRows {
		light = ColorMapRows - 1
	}
	return cm[light][src]
}
