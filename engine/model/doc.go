// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package model is the dispatch layer that routes "load me this
// file" requests to the right per-format decoder (alias model,
// sprite, or BSP) based on the magic bytes at the file start.
// tyrquake: Mod_LoadModel in common/model.c.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// Detection rules:
//   "IDPO" (0x4F504449 LE) -> alias model (.mdl)  -> engine/mdl.Load
//   "IDSP" (0x50534449 LE) -> sprite       (.spr) -> engine/spr.Load
//   anything else          -> BSP map      (.bsp) -> engine/bspfile.Open
//
// The dispatcher reads only the first 4 bytes to decide; the chosen
// loader then reads the full file independently. Returns a Model
// value that carries the typed payload + a Kind tag so callers can
// switch on the result.
//
// Why a tag + interface instead of separate loaders the caller picks:
// the engine's COM_FindFile + Mod_LoadModel call sites take just a
// filename + don't know what they got back. Demo replay and savegame
// load both go through this dispatcher; preserving its detect-by-
// magic shape is what keeps GATE A's byte-equal model-cache hashes
// stable across renderer + content changes.
package model
