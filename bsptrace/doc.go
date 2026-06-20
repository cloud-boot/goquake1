// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package bsptrace is the BSP collision-tree walker. tyrquake
// splits this into two layers: Mod_HullPointContents (point->contents
// query, in common/model.c) and SV_TraceMove (swept-box query, in
// common/world.c plus part of model.c). This package starts with the
// point-contents walker; the swept trace lands as a follow-up that
// composes the same Hull struct.
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// A Hull is a self-contained collision tree: a flat array of
// ClipNodes (binary tree, indexed by child links), a flat array of
// Planes (split planes referenced by ClipNode.PlaneNum), a
// FirstClipNode + LastClipNode pair bracketing valid indices, and
// a ClipMins/ClipMaxs offset the swept-box trace applies to
// asymmetric bounding boxes. A typical Quake map carries 3 hulls:
//
//	hull[0] -- BSP rendering tree (Mod_MakeHull0 reuses Nodes)
//	hull[1] -- player size (-16,-16,-24) to (16,16,32)
//	hull[2] -- crouch monster (-32,-32,-24) to (32,32,64)
//
// Building the 3 hulls from a [bspfile.File] is one Mod_MakeHull0 +
// two Mod_LoadClipnodes-with-different-offsets passes that this
// package will expose via BuildHulls once the model-loader port
// lands. For now, callers can construct Hulls directly from
// [bspfile.File] data and pass them to HullPointContents.
//
// Point-contents semantics: walking from a non-negative nodenum,
// follow the +children/-children link based on which side of the
// split plane `point` falls on, until a negative nodenum is reached.
// The negative value IS the CONTENTS_* tag for the leaf the point
// lives in (CONTENTS_EMPTY = -1, CONTENTS_SOLID = -2, etc. -- see
// bspfile.Contents*).
package bsptrace
