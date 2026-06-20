// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

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
