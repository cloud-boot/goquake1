// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// ErrNilBuf fires when EncodeUpdate is handed a nil sizebuf. tyrquake
// silently segfaults; the Go port surfaces the misuse.
var ErrNilBuf = errors.New("server: nil sizebuf")

// ErrEntityNumRange fires when EncodeUpdate is asked to pack an
// entity index outside the 16-bit wire slot. tyrquake silently
// truncates via MSG_WriteShort's int->short cast on the long-entity
// branch and via the byte cast on the short-entity branch; the Go
// port refuses so the caller routes the bug instead of shipping a
// corrupted snapshot. tyrquake: SV_WriteEntitiesToClient's
// MSG_WriteShort(msg, e) / MSG_WriteByte(msg, e) call sites.
var ErrEntityNumRange = errors.New("server: entityNum outside [0, 0xffff]")

// EntityUpdate is the per-entity per-tick delta the server sends
// to one client. Caller computes which fields differ from the
// entity's baseline + sets only those; the encoder packs them
// into the variable-length svc_update wire format.
//
// tyrquake: derived from entity_state_t + baseline_t inside the
// SV_WriteEntitiesToClient per-entity body.
type EntityUpdate struct {
	// Which fields are present in this delta. Caller computes this
	// bitmask by comparing the current entity state against its
	// baseline.
	Bits int // bitmask of U_* constants from protocol

	// Per-axis origin (1/8 fixed-point coords on wire). Only the
	// axes with U_ORIGIN1/2/3 in Bits get written.
	Origin [3]float32

	// Per-axis angles (1/256-circle wire quantisation). Only the
	// axes with U_ANGLE1/2/3 in Bits get written.
	Angles [3]float32

	// Optional fields, each gated on its own U_* bit:
	Model    int // U_MODEL -> byte (NQ) / short (BJP/FITZ-large)
	Frame    int // U_FRAME -> byte (NQ) / short (FITZ-large)
	Skin     int // U_SKIN -> byte
	Effects  int // U_EFFECTS -> byte
	ColorMap int // U_COLORMAP -> byte
	Alpha    int // U_FITZ_ALPHA -> byte (FITZ only)
}

// EncodeUpdate writes one svc_update message for entityNum with
// the supplied delta into buf. tyrquake: the inner per-entity body
// of SV_WriteEntitiesToClient (NQ/sv_main.c lines ~660-710).
//
// Wire layout (variable length, conditional on update.Bits, vanilla
// NQ shape -- FITZ-extend bits + alpha byte are a follow-up pass):
//
//	byte    bits & 0xff | U_SIGNAL
//	[byte   bits >> 8]                     iff U_MOREBITS
//	short   entityNum                      iff U_LONGENTITY else byte
//	[byte   Model]                         iff U_MODEL
//	[byte   Frame]                         iff U_FRAME
//	[byte   ColorMap]                      iff U_COLORMAP
//	[byte   Skin]                          iff U_SKIN
//	[byte   Effects]                       iff U_EFFECTS
//	[coord  Origin[0]]                     iff U_ORIGIN1
//	[angle  Angles[0]]                     iff U_ANGLE1
//	[coord  Origin[1]]                     iff U_ORIGIN2
//	[angle  Angles[1]]                     iff U_ANGLE2
//	[coord  Origin[2]]                     iff U_ORIGIN3
//	[angle  Angles[2]]                     iff U_ANGLE3
//
// Per-axis origin and angle are interleaved (origin[i] then
// angles[i]) -- this matches the tyrquake call order, not the
// "all origins then all angles" shape that would feel more natural.
//
// The caller is responsible for computing Bits correctly (including
// folding in U_MOREBITS when any high-byte bit is set and
// U_LONGENTITY when entityNum > 255). The encoder validates neither
// -- it just packs whatever Bits says. U_SIGNAL is always OR'd into
// the first byte so callers may omit it.
//
// FITZ extensions (U_FITZ_EXTEND1/2/ALPHA/FRAME2/MODEL2/LERPFINISH)
// and the BJP/FITZ-large model/frame widenings are a follow-up
// FITZ pass; this commit implements the vanilla NQ shape only. The
// EntityUpdate.Alpha field is kept for forward-compatibility but
// the encoder ignores it.
//
// Returns:
//
//	ErrNilBuf                       if buf is nil
//	ErrEntityNumRange               if entityNum < 0 or > 0xffff
//	(propagated msg.Write* errors)  on overflow
func EncodeUpdate(buf *sizebuf.Buffer, entityNum int, update EntityUpdate) error {
	if buf == nil {
		return ErrNilBuf
	}
	if entityNum < 0 || entityNum > 0xffff {
		return ErrEntityNumRange
	}

	bits := update.Bits

	// First bits byte: low 8 bits OR'd with the svc_update tag.
	if err := msg.WriteByte(buf, (bits&0xff)|protocol.USignal); err != nil {
		return err
	}
	// High bits byte iff U_MOREBITS.
	if bits&protocol.UMoreBits != 0 {
		if err := msg.WriteByte(buf, (bits>>8)&0xff); err != nil {
			return err
		}
	}

	// Entity index: short on U_LONGENTITY, byte otherwise.
	if bits&protocol.ULongEntity != 0 {
		if err := msg.WriteShort(buf, entityNum); err != nil {
			return err
		}
	} else {
		if err := msg.WriteByte(buf, entityNum); err != nil {
			return err
		}
	}

	// Optional single-byte fields, in the upstream call order.
	if bits&protocol.UModel != 0 {
		if err := msg.WriteByte(buf, update.Model); err != nil {
			return err
		}
	}
	if bits&protocol.UFrame != 0 {
		if err := msg.WriteByte(buf, update.Frame); err != nil {
			return err
		}
	}
	if bits&protocol.UColorMap != 0 {
		if err := msg.WriteByte(buf, update.ColorMap); err != nil {
			return err
		}
	}
	if bits&protocol.USkin != 0 {
		if err := msg.WriteByte(buf, update.Skin); err != nil {
			return err
		}
	}
	if bits&protocol.UEffects != 0 {
		if err := msg.WriteByte(buf, update.Effects); err != nil {
			return err
		}
	}

	// Origin / angles interleaved per axis -- matches tyrquake's
	// call order (origin1, angle1, origin2, angle2, origin3, angle3).
	if bits&protocol.UOrigin1 != 0 {
		if err := msg.WriteCoord(buf, update.Origin[0]); err != nil {
			return err
		}
	}
	if bits&protocol.UAngle1 != 0 {
		if err := msg.WriteAngle(buf, update.Angles[0]); err != nil {
			return err
		}
	}
	if bits&protocol.UOrigin2 != 0 {
		if err := msg.WriteCoord(buf, update.Origin[1]); err != nil {
			return err
		}
	}
	if bits&protocol.UAngle2 != 0 {
		if err := msg.WriteAngle(buf, update.Angles[1]); err != nil {
			return err
		}
	}
	if bits&protocol.UOrigin3 != 0 {
		if err := msg.WriteCoord(buf, update.Origin[2]); err != nil {
			return err
		}
	}
	if bits&protocol.UAngle3 != 0 {
		if err := msg.WriteAngle(buf, update.Angles[2]); err != nil {
			return err
		}
	}

	return nil
}
