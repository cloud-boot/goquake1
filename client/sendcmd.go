// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sizebuf"
)

// ErrSendNilBuf is returned by every clc_* encoder when the caller
// passes a nil sizebuf. Surfacing it as a typed sentinel keeps the
// encoders errors.Is-friendly (mirrors [ErrUnknownSvc] / [ErrEOF]
// in decode.go).
var ErrSendNilBuf = errors.New("client: nil sizebuf for clc encoder")

// ErrSendBadOpcode is returned by [EncodeClcOpcode] when the caller
// asks for a single-byte emit of an opcode that is NOT actually
// payload-free. Only clc_nop and clc_disconnect are 1-byte arms;
// clc_move and clc_stringcmd carry payloads and have dedicated
// encoders ([EncodeClcMove] / [EncodeClcStringCmd]).
var ErrSendBadOpcode = errors.New("client: clc opcode not single-byte")

// ErrSendEmptyString is returned by [EncodeClcStringCmd] when the
// caller passes "". The C upstream's Cmd_ForwardToServer happens
// to generate zero output in that case (it skips the MSG_WriteByte
// for argc==0), but the Go port surfaces the bug instead of
// silently emitting clc_stringcmd + "" -- which the server would
// then parse as an empty command and Host_Error on.
var ErrSendEmptyString = errors.New("client: clc_stringcmd needs a non-empty command")

// EncodeClcOpcode writes a single payload-free clc opcode byte
// (clc_nop or clc_disconnect) into buf. Returns [ErrSendNilBuf] if
// buf is nil; returns [ErrSendBadOpcode] if op is neither
// protocol.ClcNop nor protocol.ClcDisconnect (clc_move +
// clc_stringcmd carry payloads and must go through their own
// encoders).
//
// Most callers want [EncodeClcNop] / [EncodeClcDisconnect] (the
// thin opcode-fixed wrappers). This generic form exists for
// callers that already hold an opcode value from a switch.
func EncodeClcOpcode(buf *sizebuf.Buffer, op int) error {
	if buf == nil {
		return ErrSendNilBuf
	}
	if op != protocol.ClcNop && op != protocol.ClcDisconnect {
		return ErrSendBadOpcode
	}
	return msg.WriteByte(buf, op)
}

// EncodeClcNop writes a clc_nop opcode (1 byte). tyrquake: clc_nop
// is the keepalive the client sends when there's nothing else to
// send this tic (NetQuake's CL_SendCmd path -- if no movement
// message is built yet, it emits the lone clc_nop so the server's
// last_message timer doesn't expire and drop the connection).
//
// Returns [ErrSendNilBuf] if buf is nil; otherwise propagates the
// first msg.WriteByte error verbatim.
func EncodeClcNop(buf *sizebuf.Buffer) error {
	return EncodeClcOpcode(buf, protocol.ClcNop)
}

// EncodeClcDisconnect writes a clc_disconnect opcode (1 byte).
// tyrquake: clc_disconnect is sent during CL_Disconnect (NQ/cl_main.c
// L172) -- the polite server-side notification that this client is
// going away so the server can free the slot immediately instead of
// waiting for the connection-timeout reaper.
//
// Returns [ErrSendNilBuf] if buf is nil; otherwise propagates the
// first msg.WriteByte error verbatim.
func EncodeClcDisconnect(buf *sizebuf.Buffer) error {
	return EncodeClcOpcode(buf, protocol.ClcDisconnect)
}

// EncodeClcStringCmd writes a clc_stringcmd opcode + the NUL-
// terminated command string. tyrquake: clc_stringcmd is the wire
// shape for "name X" / "kill" / "say hello" -- every console
// command Cmd_ForwardToServer forwards.
//
// Wire layout:
//
//	byte    clc_stringcmd
//	str     cmd + NUL                  (the terminator is added here;
//	                                    the caller must NOT pre-NUL)
//
// Returns [ErrSendNilBuf] if buf is nil; [ErrSendEmptyString] if
// cmd == "" (see the sentinel doc). Otherwise propagates the first
// msg.Write* error verbatim.
func EncodeClcStringCmd(buf *sizebuf.Buffer, cmd string) error {
	if buf == nil {
		return ErrSendNilBuf
	}
	if cmd == "" {
		return ErrSendEmptyString
	}
	if err := msg.WriteByte(buf, protocol.ClcStringCmd); err != nil {
		return err
	}
	return msg.WriteString(buf, cmd)
}

// EncodeClcMove writes a clc_move opcode (the per-tic player-input
// snapshot) into buf. tyrquake: CL_SendMove in common/cl_input.c
// L522 (the NQ_HACK arm).
//
// Wire layout (NQ-vanilla, no checksum byte, no FITZ-extend):
//
//	byte    clc_move
//	float   sendTime              (cl.mtime[0] -- server uses for ping)
//	angle   ViewAngles[0]         (pitch, 1-byte WriteAngle)
//	angle   ViewAngles[1]         (yaw,   1-byte WriteAngle)
//	angle   ViewAngles[2]         (roll,  1-byte WriteAngle)
//	short   ForwardMove
//	short   SideMove
//	short   UpMove
//	byte    buttons               (BUTTON_ATTACK / BUTTON_JUMP / ...)
//	byte    impulse               ("+impulse N" weapon-switch key etc.)
//
// DEVIATION from the per-task spec: [server.UserCmd] does NOT
// currently carry Buttons / Impulse fields (it only has
// ViewAngles + ForwardMove + SideMove + UpMove). Rather than mutate
// state.go (owned by a sibling agent's commit window), the buttons
// + impulse bitfields are passed as explicit uint8 parameters --
// the caller threads them in from the +attack / +jump kbutton
// states + the in_impulse global the way CL_SendMove does upstream.
//
// The FITZ protocol arm uses WriteAngle16 instead of WriteAngle
// (common/cl_input.c L542); the vanilla NQ path here writes the
// 1-byte form. The C source also has an optional 1-byte checksum
// at the start (between the opcode and the time) gated on the QSG2
// / QHOST-protect flags; this port omits it and documents the
// omission. A FITZ-extend / QSG2 follow-up pass can layer the
// checksum + Angle16 on top.
//
// ForwardMove / SideMove / UpMove are SHORT (signed 16-bit) on the
// wire (MSG_WriteShort). The C source stores them as floats in
// usercmd_t but truncates to short on emit; the Go port performs
// the same truncation by feeding the float through int conversion
// inside msg.WriteShort, which masks down to int16 internally. The
// nominal Quake range (+/- cl_forwardspeed * cl_movespeedkey ~=
// 400 default) fits comfortably in int16.
//
// Returns [ErrSendNilBuf] if buf is nil; otherwise propagates the
// first msg.Write* error verbatim.
func EncodeClcMove(buf *sizebuf.Buffer, sendTime float32, cmd server.UserCmd, buttons, impulse uint8) error {
	if buf == nil {
		return ErrSendNilBuf
	}
	if err := msg.WriteByte(buf, protocol.ClcMove); err != nil {
		return err
	}
	if err := msg.WriteFloat(buf, sendTime); err != nil {
		return err
	}
	for axis := 0; axis < 3; axis++ {
		if err := msg.WriteAngle(buf, cmd.ViewAngles[axis]); err != nil {
			return err
		}
	}
	if err := msg.WriteShort(buf, int(cmd.ForwardMove)); err != nil {
		return err
	}
	if err := msg.WriteShort(buf, int(cmd.SideMove)); err != nil {
		return err
	}
	if err := msg.WriteShort(buf, int(cmd.UpMove)); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, int(buttons)); err != nil {
		return err
	}
	return msg.WriteByte(buf, int(impulse))
}
