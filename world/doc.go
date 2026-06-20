// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package world is the server-side collision + area-broadphase
// layer. It owns the area tree (a depth-4 binary subdivision of the
// map's bounding box that the server uses to short-circuit
// O(n_entities) sweeps when tracing or query-touching), and it
// exposes the trace primitives the rest of the server layer calls:
//
//   - World.Clear(mins, maxs) -- reset the area tree to fit the
//     loaded map. tyrquake: SV_ClearWorld.
//
//   - (forthcoming) LinkEdict / UnlinkEdict / AreaEdicts --
//     register entities into the tree, query the entities whose
//     bounding box overlaps a region. tyrquake: SV_LinkEdict,
//     SV_UnlinkEdict, SV_AreaEdicts.
//
//   - (forthcoming) TraceMove + PointContents + TestEntityPosition
//     -- swept-trace, point-classify, and overlap-check primitives
//     that compose the world geometry with the area tree's entity
//     enumeration. tyrquake: SV_TraceMove, SV_PointContents,
//     SV_TestEntityPosition.
//
// Mapping vs tyrquake@6531579:
//
//	common/world.c -- the source the whole package mirrors
//	NQ/server.h    -- areanode_t (this package's AreaNode)
//
// The area tree's shape: a binary tree, NOT a quadtree. At each
// internal level the builder picks the LONGER of the (x, y) axes
// and splits the box at its midpoint along that axis -- so adjacent
// children swap which axis subdivides them depending on the bounds.
// Recursion stops at AREA_DEPTH (4), giving 2^4 = 16 leaves and
// 1 + 2 + 4 + 8 = 15 internal nodes (total 31, rounded up to the
// AREA_NODES=32 static budget in [server.AreaNodes]).
package world
