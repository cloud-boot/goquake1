// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package wad reads id Software's WAD2 archive format (the engine
// container for gfx.wad: status-bar artwork, palette, console
// background, and other 8-bit indexed-colour pictures). tyrquake's
// W_LoadWadFile + W_GetLumpName + lumpinfo_t hand-port from
// common/wad.c.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// On-disk layout (little-endian):
//
//   header wadinfo_t (12 bytes)
//     [0:4]   id   = "WAD2"
//     [4:8]   numlumps    (int32)
//     [8:12]  infotableofs (int32) -- byte offset to lump table
//
//   lump table at infotableofs, numlumps * 32 bytes:
//     [0:4]   filepos      (int32)
//     [4:8]   disksize     (int32)
//     [8:12]  size         (int32) -- uncompressed; equals disksize
//                                     when compression == CMP_NONE
//     [12]    type         TYP_* (palette / qtex / qpic / sound / miptex)
//     [13]    compression  CMP_NONE or CMP_LZSS
//     [14:16] pad
//     [16:32] name (NUL-terminated, lowercased + padded by the
//             engine via W_CleanupName)
//
// fs.FS contract notes:
//   - Open succeeds with fs.ErrNotExist for missing names. Name
//     comparison is case-INSENSITIVE (the upstream W_CleanupName
//     lowercases on load AND on lookup). Pass either case; the
//     stored canonical form is lowercase.
//   - Open returns an fs.File whose Read advances through the
//     payload bytes verbatim. Compressed lumps (LZSS) are returned
//     with their disk bytes -- the upstream never uses CMP_LZSS for
//     shipping data, so we punt the LZSS decoder to the day a real
//     shipping WAD needs it.
package wad
