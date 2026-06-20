// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package bspfile reads id Software's BSP map file format. Scope of
// THIS commit: bsp29 (the original Quake BSP version) header + the
// version-agnostic simple typed lumps (Vertex, Edge, Surfedge, Plane,
// Model, MipTexLump). Renderer-coupled types whose on-disk shape
// changed between bsp29 / bsp2rmq / bsp2 (Node, Leaf, Face,
// ClipNode) land in a follow-up alongside the renderer port that
// actually traverses them.
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// On-disk header (little-endian):
//
//	dheader_t
//	  [0:4]   version  (int32) -- BSPVERSION 29 / BSP2VERSION /
//	                              BSP2RMQVERSION (only 29 parsed here)
//	  [4:124] lumps    [15]lump_t -- each lump_t is 8 bytes:
//	                                 fileofs (int32) + filelen (int32)
//
// The 15 lump kinds are numbered by LUMP_ENTITIES..LUMP_MODELS;
// each names a typed section of the file. See per-lump decoders for
// the byte layout each expects.
//
// CONTENTS_* and PLANE_* constants are exposed because the engine's
// collision and rendering code switches on them by name; preserving
// the upstream values lets demo replay GATE A stay byte-equal.
package bspfile
