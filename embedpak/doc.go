// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package embedpak ships an in-binary PAK archive that the engine can
// register with a [vfs.SearchPath] without any host filesystem access.
//
// The default shipped blob (empty.pak) is a 12-byte stub: a valid PAK
// header with zero directory entries. The package's [Bytes] returns
// the raw blob, [IsEmpty] reports whether it is the placeholder,
// [OpenAsFS] turns it into an [io/fs.FS] backed by [pak.Open], and
// [AddToVFS] is a one-call helper that prepends the embedded pak to a
// supplied SearchPath.
//
// Swapping in real shareware data:
//
// id Software granted free redistribution of the Episode 1 shareware
// `pak0.pak` (palette, colormap, conchars, sprites, sounds, and the
// first set of maps). The repository deliberately does NOT carry a
// copy of that file; the operator drops it in by overwriting
// embedpak/empty.pak and rebuilding:
//
//	cp /path/to/quake/id1/pak0.pak embedpak/empty.pak
//	go build ./...
//
// After the swap [IsEmpty] returns false and [AddToVFS] succeeds; the
// engine can then load real palette/colormap/conchars + the shareware
// maps through the same code path that the synthetic-asset bootstrap
// uses today.
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
package embedpak
