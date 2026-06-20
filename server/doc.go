// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package server exposes the enums and limits the Quake server
// layer reads off entity fields (entvars_t.solid, .movetype, .flags,
// .deadflag, .effects) plus the wire-protocol caps the server
// allocates against (MAX_EDICTS, MAX_MODELS, ...).
//
// Two categories live here:
//
//   - Entity state enums (SOLID_* / MOVETYPE_* / DEAD_* / FL_* /
//     EF_*) -- shared with QuakeC; they are observable on every
//     edict's entvars_t and mutating them changes how the server
//     classifies the entity for collision, physics, rendering,
//     and AI flags.
//
//   - Allocation limits (MAX_EDICTS, MAX_MODELS, MAX_SOUNDS,
//     MAX_LIGHTSTYLES, MAX_DATAGRAM, MAX_CLIENTS, AREA_NODES,
//     AREA_DEPTH) -- the static caps the server reserves slots
//     against when SV_SpawnServer / SV_ClearWorld run.
//
// Everything here is a numeric constant; there are no functions
// because none are needed at this layer. The constants are
// spec-traceable to:
//
//	NQ/server.h     -- entity-state enums (SOLID_*, MOVETYPE_*, ...)
//	NQ/quakedef.h   -- allocation caps (MAX_EDICTS, MAX_MODELS, ...)
//	NQ/protocol.h   -- network-layer caps (MAX_CLIENTS)
//	common/world.c  -- area-tree caps (AREA_NODES, AREA_DEPTH)
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
package server
