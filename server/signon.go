// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

// SendSignonHandshake queues the minimum-viable signon byte sequence
// onto client.Message so the next FlushClientMessage tic delivers it
// to the bound NetConnection. The handshake is the wire equivalent of
// the C upstream's SV_SendServerinfo + the per-stage Host_Spawn_f /
// Host_Begin_f progression: a single svc_serverinfo payload (which
// itself terminates with svc_signonnum(1)) followed by svc_signonnum
// byte-pairs for stages 2, 3, and 4. The client's per-tick decoder
// drains the queue and Apply walks the lifecycle Disconnected →
// Connecting (stage 1) → Connected (stage 4) without any caller-side
// state-machine pokes.
//
// The full upstream handshake is dozens of message types over many
// round-trips (per-precache acknowledgement, per-entity baseline,
// clientdata, ...); this helper deliberately ships ONLY the lifecycle
// bytes so a loopback single-player path can transition through the
// states via the wire protocol. Once the full clc parser + the
// per-stage baseline / clientdata emission lands, this helper can be
// retired in favour of the upstream's stuffcmd-driven flow.
//
// tyrquake: SV_SendServerinfo in NQ/sv_main.c + Host_Spawn_f /
// Host_Begin_f in NQ/host_cmd.c, collapsed into a single queue-side
// call so a loopback embedder can drive the handshake without
// implementing the inbound clc stringcmd parser.
//
// Returns:
//
//   - nil               on success (handshake bytes appended to
//     client.Message; client.SendSignon flipped false)
//   - ErrEmptyLevelName when info.LevelName is empty (propagated
//     from EncodeServerInfo)
//   - the propagated msg.Write* / sizebuf overflow error otherwise
//
// Silent no-op when client is nil, inactive, or has no Message
// buffer -- matches the rest of the package's "skip the silent slot,
// don't error on a structurally absent target" convention.
func SendSignonHandshake(client *Client, info ServerInfo) error {
	if client == nil || !client.Active || client.Message == nil {
		return nil
	}
	if err := EncodeServerInfo(client.Message, info); err != nil {
		return err
	}
	// EncodeServerInfo already wrote svc_signonnum(1) as its trailer.
	// Walk stages 2..4 so the client's Apply runs the full stage-byte
	// progression even though stages 2 + 3 are currently no-ops --
	// they're cheap, they're visible on the wire log, and they match
	// the C upstream's transcript when the full flow lands.
	for stage := 2; stage <= 4; stage++ {
		if err := EncodeSignonNum(client.Message, stage); err != nil {
			return err
		}
	}
	// SendSignon is the upstream's "needs serverinfo retransmit" flag;
	// flip it false now that the handshake bytes are queued so the
	// per-tic broadcast loop doesn't re-fire the same bytes next tic.
	client.SendSignon = false
	return nil
}
