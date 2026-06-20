// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/progs"
)

// ErrNoFreeClientSlot fires when every slot is Active.
var ErrNoFreeClientSlot = errors.New("server: no free client slot")

// ConnectClient binds an incoming NetConn to a free Client slot.
// tyrquake: SV_ConnectClient in NQ/sv_main.c lines 419-463.
//
// The C upstream is two functions glued together: SV_CheckForNewClients
// finds the free slot + attaches the qsocket, then SV_ConnectClient
// re-initialises the client_t around that qsocket. The Go port collapses
// both into one call -- the netcode driver hands ConnectClient a
// NetConn + ConnectClient does the slot search + the per-client setup.
//
// Algorithm:
//  1. Find the first free slot (slot.Active == false) in static.Clients.
//  2. If no slot available: return ErrNoFreeClientSlot.
//  3. Bind: slot.Active = true; slot.Spawned = false; slot.SendSignon = true;
//     slot.NetConnection = conn; slot.LastMessage = now.
//  4. Allocate the client's edict via makeEdict (caller hooks this into
//     their progs.EdictArena).
//  5. Reset per-client fields: slot.Name = "" (the C upstream strcpys
//     "unconnected" here; the Go port leaves it empty + lets the signon
//     handshake set the display name), slot.Colors = 0, slot.SpawnParms
//     = zeroed.
//  6. Return the slot index (0..MaxClients-1).
//
// The C upstream's SetNewParms PR_ExecuteProgram call + the
// SV_SendServerinfo call are NOT done here: those touch progs +
// serverinfo encoding which the lifecycle layer owns. ConnectClient
// returns once the slot is bound; the caller drives the rest of the
// signon handshake.
func ConnectClient(static *Static, conn NetConn, now float64, makeEdict func() *progs.Edict) (int, error) {
	slotIdx := -1
	for i, c := range static.Clients {
		if !c.Active {
			slotIdx = i
			break
		}
	}
	if slotIdx == -1 {
		return -1, ErrNoFreeClientSlot
	}

	slot := static.Clients[slotIdx]
	slot.Active = true
	slot.Spawned = false
	slot.SendSignon = true
	slot.NetConnection = conn
	slot.LastMessage = now
	slot.Edict = makeEdict()
	slot.Name = ""
	slot.Colors = 0
	slot.SpawnParms = [NumSpawnParms]float32{}

	return slotIdx, nil
}
