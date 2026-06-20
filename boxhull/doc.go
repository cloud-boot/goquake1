// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package boxhull constructs axis-aligned bounding-box collision
// hulls that the [bsptrace] walker can trace against.
//
// In Quake's collision model, BSP brush entities (doors, lifts,
// trains -- anything that uses the world geometry's clipnodes) are
// traced against their own pre-built hulls. Every OTHER solid
// entity -- monsters, missiles, dropped items, player corpses --
// needs an ad-hoc 6-plane hull built on the fly from the entity's
// mins/maxs bounding box. tyrquake builds this once per trace via
// Mod_CreateBoxhull; this package exposes the same builder.
//
// The 6 planes are the box's faces:
//
//	plane 0: +X face, normal=(1,0,0), dist=maxs[0]
//	plane 1: -X face, normal=(1,0,0), dist=mins[0]
//	plane 2: +Y face, normal=(0,1,0), dist=maxs[1]
//	plane 3: -Y face, normal=(0,1,0), dist=mins[1]
//	plane 4: +Z face, normal=(0,0,1), dist=maxs[2]
//	plane 5: -Z face, normal=(0,0,1), dist=mins[2]
//
// The 6 clipnodes encode a BSP that classifies any point as
// CONTENTS_SOLID (inside the box) or CONTENTS_EMPTY (outside), via
// short-circuit descent down the +X, -X, +Y, -Y, +Z, -Z faces in
// order. The tree shape matches the C upstream's
// `box_clipnodes[6]` table verbatim, so [bsptrace.TraceHull] walks
// it identically.
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// References:
//
//	common/model.c: Mod_CreateBoxhull, box_clipnodes, boxhull_template
//	common/include/model.h: boxhull_t, Mod_CreateBoxhull
package boxhull
