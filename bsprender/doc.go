// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package bsprender is the BSP visibility front-end of the 3D world
// renderer. Each frame the renderer asks "which BSP leaves can the
// viewer see?" and tags them with the current frame counter; the
// subsequent recursive world walk skips any leaf or node not tagged
// at the live frame.
//
// This package owns two primitives that together answer that
// question:
//
//   - [DecompressVis] -- the run-length decoder for one PVS row, the
//     byte-wise RLE the BSP file stores in its LumpVisibility blob
//     (tyrquake: Mod_DecompressVis in common/model.c, vanilla form;
//     the tyrquake leafblock_t variant adds shift accounting on top
//     of the same RLE).
//
//   - [MarkVisibleLeaves] -- the per-frame PVS walk that decompresses
//     the viewer leaf's row, marks each visible leaf, and walks up
//     each leaf's parent chain marking ancestor nodes too (tyrquake:
//     R_MarkLeaves in common/gl_rmain.c / common/r_bsp.c).
//
// The package is intentionally decoupled from engine/model: the
// caller supplies a [MarkContext] of plain function closures so this
// code stays trivially testable against synthetic BSPs and so the
// model package can adopt the per-leaf VisFrame field on its own
// schedule.
package bsprender
