// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import "github.com/go-quake1/engine/progs"

// SendFrameResult is what SendClientFrames returns: per-client
// success/failure flags so the caller can DropClient any that
// failed mid-send.
//
// PerClientErrs is parallel to static.Clients -- index i corresponds
// to static.Clients[i]. A nil entry means either success or "client
// was skipped" (nil / inactive / unspawned slot). A non-nil entry
// is the propagated sizebuf write error from
// [Server.PreparePerClientMessage].
type SendFrameResult struct {
	PerClientErrs []error // nil entry means success; len == len(static.Clients)
}

// PreparePerClientMessage is the per-client variant of the simplified
// frame builder. Copies sv's reliable + unreliable buffers into ONE
// client's Message buffer. Used by callers that want explicit
// per-slot handling (e.g. integrating with a future
// SV_WriteEntitiesToClient + SV_WriteClientdataToMessage).
//
// Algorithm:
//
//  1. nil / inactive / unspawned client -> silent no-op (nil)
//  2. append sv.ReliableDatagram bytes to client.Message
//  3. append sv.Datagram bytes to client.Message
//  4. return any propagated sizebuf write error
//
// Does NOT clear sv's buffers; the caller is responsible for
// end-of-frame [Server.ClearDatagram] +
// [Server.ClearReliableDatagram] once every client has been served.
//
// tyrquake: the post-WriteEntities tail of SV_SendClientMessages in
// NQ/sv_main.c where reliable_datagram + datagram are appended to
// each client's per-frame message buffer.
//
// Follow-up items (NOT covered by this simplified port -- the C
// upstream's SV_SendClientDatagram does them per-client before the
// buffer copies):
//
//   - svc_time + svc_setangle per-client headers (needs progs state)
//   - SV_WriteClientdataToMessage (needs the per-client view state)
//   - SV_WriteEntitiesToClient (needs PVS + per-entity baseline diff)
//   - NET_SendMessage / NET_SendUnreliableMessage (no netcode yet)
func (s *Server) PreparePerClientMessage(client *Client) error {
	if client == nil || !client.Active || !client.Spawned {
		return nil
	}
	if err := client.Message.Write(s.ReliableDatagram.Bytes()); err != nil {
		return err
	}
	return client.Message.Write(s.Datagram.Bytes())
}

// SendClientFrames is the simplified per-tick frame builder. For
// each active+spawned client, copies:
//
//  1. sv.ReliableDatagram into client.Message (server-wide reliable)
//  2. sv.Datagram into client.Message         (server-wide unreliable)
//
// Skips clients with DropAsap or Active=false (the per-client
// [Server.PreparePerClientMessage] enforces the skip). Does NOT
// clear sv's buffers; caller handles end-of-frame
// [Server.ClearDatagram] + [Server.ClearReliableDatagram].
//
// tyrquake: SV_SendClientMessages -- the post-WriteEntities phase
// where reliable_datagram + datagram are appended to each client.
//
// Returns PerClientErrs (parallel to static.Clients); nil entry per
// successful (or skipped) client, non-nil per failure (typically
// sizebuf overflow). The function never short-circuits on a
// per-client error -- every active client gets attempted.
//
// Follow-up items (NOT covered by this simplified port): see
// [Server.PreparePerClientMessage].
func (s *Server) SendClientFrames(static *Static) SendFrameResult {
	result := SendFrameResult{PerClientErrs: make([]error, len(static.Clients))}
	for i, c := range static.Clients {
		result.PerClientErrs[i] = s.PreparePerClientMessage(c)
	}
	return result
}

// WriteClientData composes one svc_clientdata snapshot for client
// (using [ComposeClientDataFromEdict] over the client's bound
// [progs.Edict]) + writes it into client.Message. Silent no-op
// when:
//
//   - client is nil / inactive / unspawned (matches
//     [Server.PreparePerClientMessage]'s skip),
//   - client.Edict is nil (the edict-pool isn't bound yet --
//     SpawnServer ran without a NewClient pre-allocated slot),
//   - p is nil (no progs bound; the encoder has nothing to read).
//
// On a successful compose the encoded bytes land in client.Message,
// where [Server.FlushClientMessage] picks them up at end-of-tic.
//
// Returns the propagated [EncodeClientData] error (sizebuf overflow);
// nil otherwise. The function never short-circuits on a missing
// edict field -- the compose helper substitutes zero / default for
// anything that isn't declared on the bound progs.
func (s *Server) WriteClientData(client *Client, p *progs.Progs) error {
	if client == nil || !client.Active || !client.Spawned {
		return nil
	}
	if client.Edict == nil || p == nil {
		return nil
	}
	state := ComposeClientDataFromEdict(p, client.Edict)
	return EncodeClientData(client.Message, state)
}

// FlushClientMessage drains client.Message through client.NetConnection
// (cast to [NetConn]) via SendReliable, then clears the buffer. This
// is the missing back-channel: until this fires, the per-tic
// svc_clientdata + svc_update bytes the encoders write into
// client.Message never leave the server-side struct, and the loopback
// client side reads nothing.
//
// Silent no-op when:
//
//   - client is nil / inactive (skip matches the upstream's
//     per-slot loop),
//   - client.Message has zero bytes (nothing to flush this tic),
//   - client.NetConnection is nil or doesn't implement [NetConn]
//     (test stubs without a bound transport).
//
// On a successful flush the buffer is cleared so the next tic
// starts fresh. The send-side payload is the entire Message in one
// SendReliable call (loopback has no MTU on reliable; UDP netcode
// will fragment as needed in a future port).
//
// Returns the propagated SendReliable error (typically
// [ErrNetConnClosed]); nil otherwise. The Message is NOT cleared
// when SendReliable errors -- callers can retry next tic, or
// DropClient the slot.
func (s *Server) FlushClientMessage(client *Client) error {
	if client == nil || !client.Active {
		return nil
	}
	if client.Message == nil || client.Message.Len() == 0 {
		return nil
	}
	conn, ok := client.NetConnection.(NetConn)
	if !ok {
		return nil
	}
	if _, err := conn.SendReliable(client.Message.Bytes()); err != nil {
		return err
	}
	client.Message.Clear()
	return nil
}
