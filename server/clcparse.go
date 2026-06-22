// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"strings"

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
// and applies each recognised clc_* opcode to c. Handled opcodes:
//
//   - [protocol.ClcMove]      per-tic player-input snapshot
//     (client.EncodeClcMove); overwrites c.Cmd.
//   - [protocol.ClcNop]       keepalive; silent skip.
//   - [protocol.ClcStringCmd] console-command forward
//     (client.EncodeClcStringCmd); dispatched via [ParseClcStringCmd]
//     to drive the signon-stage progression (spawn / begin flip
//     c.Spawned + queue stage-4 byte) + the per-client display name.
//
// Other opcodes return [ErrBadClcOpcode] -- the upstream NQ server
// Host_Error's on the same path; the Go port surfaces it as a typed
// sentinel so callers can decide drop vs log + continue.
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
		case protocol.ClcStringCmd:
			payload := r.ReadString()
			if r.Bad() {
				return applied, ErrShortClcStringCmd
			}
			if err := ParseClcStringCmd(c, payload); err != nil {
				return applied, err
			}
			applied++
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

// ErrShortClcStringCmd is returned by [ReadClientMoves] when a
// clc_stringcmd datagram is missing the NUL terminator (the inbound
// reader hits EOF before a 0 byte).
var ErrShortClcStringCmd = errors.New("server: short clc_stringcmd payload")

// SignonStageSpawn is the signon stage byte the server emits in
// response to the client's "spawn" / "begin" clc_stringcmd. Stage 4
// is the upstream's "fully signed on, in-game" marker (cls.state =
// ca_active in CL_SignonReply).
const SignonStageSpawn = 4

// ParseClcStringCmd dispatches one clc_stringcmd payload (the
// space-separated console command line the client forwarded via
// EncodeClcStringCmd) on c. The Go port handles the subset the
// signon handshake actually needs:
//
//   - "prespawn ..."   silently acknowledged (the upstream uses this
//     to gate the per-tic baseline broadcast; the Go port queues all
//     baselines up front in the SpawnServer post-pass, so the
//     stringcmd is a no-op here).
//
//   - "spawn" / "begin" flips c.Spawned = true + queues
//     svc_signonnum(SignonStageSpawn) onto c.Message so the next
//     FlushClientMessage delivers the stage-4 byte to the client; the
//     client's applySignonNum then transitions State.Connection to
//     StateConnected. This is the wire-driven equivalent of the
//     upstream Host_Spawn_f / Host_Begin_f pair.
//
//   - "name <new>"     sets c.Name to <new> (trimmed). The upstream
//     caps the display name at 32 bytes via Q_strncpy; the Go port
//     stores whatever the client sent verbatim (caller-side
//     responsibility).
//
//   - anything else    silently skipped. The upstream Host_Error's on
//     unknown commands; the Go port logs nothing + continues so a
//     future expansion (kill / say / color / ...) can layer in
//     without breaking the bring-up wire path.
//
// Returns:
//
//   - nil on success (including the silent-skip paths).
//   - the propagated [EncodeSignonNum] error (sizebuf overflow) for
//     the spawn / begin arms.
//
// A nil c is a silent no-op (matches the rest of the package's
// "skip the silent slot" convention).
func ParseClcStringCmd(c *Client, payload string) error {
	if c == nil {
		return nil
	}
	cmd := strings.TrimSpace(payload)
	if cmd == "" {
		return nil
	}
	word, rest := splitFirstWord(cmd)
	switch word {
	case "prespawn":
		// Baselines are queued up front in the Go port (SpawnServer +
		// SendBaselines on connect); the stringcmd has nothing left to
		// gate.
		return nil
	case "spawn", "begin":
		c.Spawned = true
		if c.Message == nil {
			return nil
		}
		return EncodeSignonNum(c.Message, SignonStageSpawn)
	case "name":
		c.Name = strings.TrimSpace(rest)
		return nil
	}
	return nil
}

// splitFirstWord splits s on the first ASCII space and returns the
// (word, rest) pair. When s has no space, returns (s, "").
func splitFirstWord(s string) (word, rest string) {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
