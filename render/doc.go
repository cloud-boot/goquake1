// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package render is the software rasteriser subsystem of the Quake 1
// engine port. It owns the palettized 8-bit display surface that the
// span filler writes into, the palette + colour-map lookup tables
// that drive every shading decision (lightmaps, fullbrights, water
// warp, gun-flash flash), and the screen-space transform math that
// turns world-space vertices into pixel coordinates.
//
// The package is delivered in three sibling batches so each concern
// stays in its own file:
//
//   - [FrameBuffer] + [Palette] + [FrameBuffer.Expand] / [FrameBuffer.ExpandTo32]
//     in framebuffer.go -- the 8-bit display surface plus the per-frame
//     palette expand path to RGBA8 / packed ARGB uint32 (the format
//     the SDL + TamaGo display backends consume).
//   - The palette + 256x64 colormap loader in palette.go + colormap.go --
//     the gfx.wad PALETTE lump (256 RGB triples) and COLORMAP lump
//     (the 64-level light-attenuation lookup table the span filler
//     indexes per-pixel).
//   - The screen-space transform math in transform.go -- world ->
//     view -> projected-pixel coordinates, including the FOV-driven
//     scaling factors the upstream's R_SetupFrame computes once per
//     frame.
//
// tyrquake origin: vid.h (the display surface), gfx.h (the PALETTE +
// COLORMAP lumps), r_main.c (the per-frame transform setup).
package render
