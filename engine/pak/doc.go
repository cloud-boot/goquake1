// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package pak reads id Software's .pak archive format and exposes
// its contents as an [io/fs.FS]. tyrquake's COM_LoadPackFile +
// dpackheader_t + dpackfile_t are inlined; the upstream's
// COM_AddGameDirectory / search-path layering lives one level up
// in engine/vfs (next port).
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// On-disk layout (little-endian throughout):
//
//   header (12 bytes)
//     [0:4]   id   = "PACK"
//     [4:8]   dirofs (int32)  offset to directory table
//     [8:12]  dirlen (int32)  byte length of directory table
//
//   each directory entry (64 bytes)
//     [0:56]  name (NUL-padded, no NUL required if name fills 56 bytes)
//     [56:60] filepos (int32) byte offset of payload in the .pak
//     [60:64] filelen (int32) byte length of payload
//
//   payloads
//     stored uncompressed at filepos, contiguous, in arbitrary order.
//
// The shareware Episode 1 `pak0.pak` ID Software released into the
// public-redistributable category contains 339 entries totalling
// ~18 MiB.
//
// fs.FS contract notes:
//   - Open succeeds with fs.ErrNotExist for missing names.
//   - Open returns an fs.File whose Read advances; Seek is supported
//     via the io.ReadSeeker layer (no separate fs.ReadFileFS contract).
//   - Stat returns a synthesised fs.FileInfo; mtime is zero because
//     .pak archives carry no per-file timestamps.
//   - Directory enumeration (ReadDir) is supported -- the engine's
//     loader walks pak0/pak1 looking for `maps/start.bsp` etc.
package pak
