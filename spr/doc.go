// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package spr reads id Software's .spr sprite-package format. Used
// by the engine for billboards: explosions, item glints, particle
// rotators -- anything that's a flat 8-bit-indexed bitmap presented
// always-facing-camera (or one of the SPR_* orientations).
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// On-disk layout (little-endian throughout, all int32 unless noted):
//
//	dsprite_t (36 bytes)
//	  [0:4]   ident         = "IDSP" (0x50534449)
//	  [4:8]   version       (must be 1)
//	  [8:12]  type          SPR_* orientation tag
//	  [12:16] bounding_radius (float32)
//	  [16:20] width
//	  [20:24] height
//	  [24:28] numframes
//	  [28:32] beamlength    (float32; lightning beam scaling)
//	  [32:36] synctype      ST_SYNC / ST_RAND
//
//	then numframes records, each prefixed by a 4-byte frame-type tag:
//	  SPR_SINGLE (0): one dspriteframe_t + width*height bitmap bytes
//	  SPR_GROUP  (1): dspritegroup_t header + N intervals + N
//	                  (frame + bitmap) sub-records
//
//	dspriteframe_t (16 bytes):
//	  [0:8]   origin[2]     (int32 each; sprite-relative offset of the
//	                         top-left pixel)
//	  [8:12]  width         (per-frame; can differ from header width)
//	  [12:16] height        (per-frame; can differ from header height)
//
// Bitmaps are 8-bit indexed-colour. The engine resolves the index
// to RGBA via the palette lump from gfx.wad at draw time.
package spr
