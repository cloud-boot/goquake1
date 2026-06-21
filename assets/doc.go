// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package assets is the engine's startup asset orchestrator. It
// takes a vfs.SearchPath (typically populated with pak0.pak +
// gfx.wad) and loads the standard set of bootstrap resources:
//
//   - the 256-entry palette (gfx/palette.lmp)
//   - the 64x256 colormap (gfx/colormap.lmp)
//   - the 128x128 conchars font sheet (gfx/conchars.lmp)
//   - a name-keyed map of Pic lumps loaded from a WAD blob
//   - a name-keyed map of Sample lumps loaded from .wav files
//
// Each loader is also exposed individually so callers can
// orchestrate non-standard loading (mods, missionpacks).
//
// tyrquake: the role of W_LoadWadFile + COM_LoadHunkFile + the
// per-startup glue in CL_Init / S_Init / R_Init. The Go port
// collapses these into one package that returns a typed Set.
package assets
