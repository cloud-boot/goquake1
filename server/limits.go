// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

// Allocation caps the server reserves slots against at startup.
// Mostly preserved verbatim from tyrquake's NQ build; MAX_EDICTS
// is the post-tyrquake bump (was 600 in id1, lifted to 8192 in
// tyrquake to support modern mods).
//
// tyrquake: NQ/quakedef.h:67-82.
const (
	MaxEdicts      = 8192    // NQ/quakedef.h:74 -- post-tyrquake bump
	MaxLightStyles = 64      // NQ/quakedef.h:75
	MaxModels      = 2048    // NQ/quakedef.h:81
	MaxSounds      = 1024    // NQ/quakedef.h:82
	MaxDatagram    = 1 << 18 // NQ/quakedef.h:67 -- max unreliable message
)

// MaxClients is the NetQuake hard cap on simultaneous clients.
// tyrquake: NQ/protocol.h:302.
const MaxClients = 16

// AreaTree caps -- the broadphase quadtree built by SV_ClearWorld.
// AreaNodes is the maximum number of internal nodes (root + 4
// subdivisions at each of AreaDepth levels = 1 + 2 + 4 + 8 + 16 =
// 31, rounded up to 32). AreaDepth is the recursion depth.
// tyrquake: common/world.c:133-134.
const (
	AreaDepth = 4
	AreaNodes = 32
)
