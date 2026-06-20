// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
)

// ClientPrint writes one svc_print message to a single client's
// reliable Message buffer: the byte svc_print followed by the
// NUL-terminated string. Used by PF_sprint (QC sprint builtin)
// + by the engine to send per-client console output.
//
// Returns nil if client is nil or inactive (silent drop, matching
// the C upstream's "if (!client->active) return" guard at the call
// sites). Returns the propagated sizebuf overflow if the message
// is too long.
//
// tyrquake: SV_ClientPrintf in NQ/host.c (the wire-emit, not the
// printf format step -- the caller is responsible for any formatting
// + must pass the final string).
func ClientPrint(client *Client, text string) error {
	if client == nil || !client.Active {
		return nil
	}
	if err := msg.WriteByte(client.Message, protocol.SvcPrint); err != nil {
		return err
	}
	return msg.WriteString(client.Message, text)
}

// BroadcastPrint writes one svc_print message to EVERY active +
// spawned client. tyrquake: SV_BroadcastPrintf.
//
// Iteration semantics match the upstream: only Active + Spawned
// slots get the print; inactive / unspawned / dropasap slots are
// skipped. Returns the first overflow error encountered (the
// upstream silently overflows; the Go port surfaces it so callers
// can decide to log or drop the message). The iteration still
// continues even after one client's buffer overflows -- a
// partial-broadcast is better than no-broadcast.
func BroadcastPrint(static *Static, text string) error {
	if static == nil {
		return nil
	}
	var firstErr error
	for _, c := range static.Clients {
		if c == nil || !c.Active || !c.Spawned {
			continue
		}
		if err := msg.WriteByte(c.Message, protocol.SvcPrint); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := msg.WriteString(c.Message, text); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	return firstErr
}

// DropClient marks a client for disconnect. After DropClient, the
// next SV_SendClientMessages tick processes the DropAsap flag and
// closes the slot. tyrquake: SV_DropClient.
//
// crash=true is the "client crashed / netcode lost the socket" path:
// no svc_disconnect is written (the connection is already dead).
// crash=false is the graceful drop: svc_disconnect is queued to the
// client's Message buffer first so the wire layer sends it before
// tearing down.
//
// The lifecycle layer owns Edict + NetConnection teardown; DropClient
// only flips the per-slot flags (Active / Spawned / DropAsap /
// SendSignon) so the next SV_SendClientMessages tick can drain.
//
// Safe to call on a nil or already-inactive client (silent no-op).
func DropClient(client *Client, crash bool) {
	if client == nil || !client.Active {
		return
	}
	if !crash {
		// Queue svc_disconnect to the reliable buffer so the wire
		// layer flushes it before the netcode tears the socket down.
		// Errors are intentionally swallowed (matches the upstream's
		// "don't check for errors" comment on the final-message send).
		_ = msg.WriteByte(client.Message, protocol.SvcDisconnect)
	}
	client.Active = false
	client.Spawned = false
	client.DropAsap = true
	client.SendSignon = false
}
