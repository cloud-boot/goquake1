// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
)

// ErrBadClcOpcode is returned by [ReadClientMoves] when the inbound
// datagram opens with a clc_* opcode the parser does not understand.
// Wire-corruption surface: the upstream NQ server drops the client in
// this case (Host_Error path); the Go port surfaces it as a typed
// sentinel so callers can decide (drop vs log + continue).
var ErrBadClcOpcode = errors.New("server: unknown clc opcode")

// ErrNilNetConn is returned by [ReadClientMoves] when c.NetConnection
// is nil or not a NetConn implementation. SpawnServer + ConnectClient
// always populate NetConnection with a real handle, so this only fires
// on test stubs that bypass ConnectClient OR on a slot in the process
// of being torn down (DropAsap).
var ErrNilNetConn = errors.New("server: client has no NetConn")

// ReadClientMoves drains every pending message from c.NetConnection
// and applies each recognised clc_* opcode to c. The only opcode this
// minimal parser handles is [protocol.ClcMove] (the per-tic player-
// input snapshot the client sends via [client.EncodeClcMove]); other
// opcodes are skipped silently so a future expansion (clc_stringcmd /
// clc_disconnect) can layer in without breaking this contract.
//
// For each clc_move datagram parsed, the latest values overwrite
// c.Cmd (ViewAngles / ForwardMove / SideMove / UpMove / Buttons /
// Impulse). The C upstream's per-frame physics integrator
// (SV_RunClients -> SV_RunCmd in NQ/sv_user.c) then reads c.Cmd to
// drive the player edict; the Go port's bring-up phase wires
// SV_Physics_Walk to the same field via [Host.cmdAt].
//
// Wire layout (mirror of [client.EncodeClcMove], NQ-vanilla):
//
//	byte    clc_move
//	float   sendTime
//	angle   ViewAngles[0]   (pitch, 1-byte WriteAngle)
//	angle   ViewAngles[1]   (yaw)
//	angle   ViewAngles[2]   (roll)
//	short   ForwardMove
//	short   SideMove
//	short   UpMove
//	byte    Buttons
//	byte    Impulse
//
// The sendTime float is read + discarded -- the Go port's per-tic
// scheduler does not yet track per-client ping deltas (the
// [Client.PingTimes] ring is populated elsewhere via [State.RecordPing]
// once the full Host_Frame ping path lands).
//
// Returns:
//
//   - (n, nil)             -- n successful clc_move applies; the inbox
//     is drained on success
//   - (n, ErrNilNetConn)   -- c is nil OR c.NetConnection is not a
//     [NetConn]; n is 0
//   - (n, ErrBadClcOpcode) -- the parser hit an unknown opcode; n is
//     the count of successful applies BEFORE the bad opcode
//   - (n, err) on any other transport / decode error
//
// MessageDisconnect short-circuits the loop with (n, nil) -- the peer
// closed cleanly, no error.
func ReadClientMoves(c *Client) (int, error) {
	if c == nil || c.NetConnection == nil {
		return 0, ErrNilNetConn
	}
	conn, ok := c.NetConnection.(NetConn)
	if !ok {
		return 0, ErrNilNetConn
	}
	applied := 0
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return applied, err
		}
		if kind == MessageNone || kind == MessageDisconnect {
			return applied, nil
		}
		r := msg.NewReader(data)
		op := r.ReadU8()
		switch op {
		case protocol.ClcMove:
			if err := decodeClcMove(r, c); err != nil {
				return applied, err
			}
			applied++
		case protocol.ClcNop:
			// Keepalive; nothing to do. Inbox-drain continues.
		default:
			return applied, ErrBadClcOpcode
		}
	}
}

// decodeClcMove reads the fixed 14-byte clc_move payload (after the
// opcode byte the caller already consumed) and overwrites c.Cmd. On a
// short payload the msg.Reader sets Bad; we surface that as
// [msg.ErrShortRead] equivalent (a synthesized sentinel).
func decodeClcMove(r *msg.Reader, c *Client) error {
	_ = r.ReadFloat() // sendTime, discarded for now
	c.Cmd.ViewAngles[0] = r.ReadAngle()
	c.Cmd.ViewAngles[1] = r.ReadAngle()
	c.Cmd.ViewAngles[2] = r.ReadAngle()
	c.Cmd.ForwardMove = float32(r.ReadShort())
	c.Cmd.SideMove = float32(r.ReadShort())
	c.Cmd.UpMove = float32(r.ReadShort())
	c.Cmd.Buttons = uint8(r.ReadU8())
	c.Cmd.Impulse = uint8(r.ReadU8())
	if r.Bad() {
		return ErrShortClcMove
	}
	return nil
}

// ErrShortClcMove is returned by [ReadClientMoves] when a clc_move
// datagram is shorter than the fixed 14-byte payload (read past EOF).
var ErrShortClcMove = errors.New("server: short clc_move payload")
