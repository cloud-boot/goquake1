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

// MaxMsgLen is the reliable-message buffer size -- server.signon,
// server.reliable_datagram, and client.message all sized to this.
// Same numeric value as MaxDatagram (both 1 << 18) but semantically
// distinct: MaxDatagram is the per-tick unreliable broadcast budget;
// MaxMsgLen is the reliable-message ceiling.
// tyrquake: NQ/quakedef.h:74.
const MaxMsgLen = 1 << 18

// NumPingTimes is the rolling-window size for the per-client
// ping-rate sample buffer. tyrquake: NQ/server.h:71.
const NumPingTimes = 16

// NumSpawnParms is the carry-over slot count for level-transition
// spawn parameters (HP, weapons, ammo, ...) per client.
// tyrquake: NQ/server.h:72.
const NumSpawnParms = 16

// AreaTree caps -- the broadphase quadtree built by SV_ClearWorld.
// AreaNodes is the maximum number of internal nodes (root + 4
// subdivisions at each of AreaDepth levels = 1 + 2 + 4 + 8 + 16 =
// 31, rounded up to 32). AreaDepth is the recursion depth.
// tyrquake: common/world.c:133-134.
const (
	AreaDepth = 4
	AreaNodes = 32
)
