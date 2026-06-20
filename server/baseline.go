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

// ErrEntityRange fires when the caller passes an entityNum that
// won't fit in the 16-bit slot the wire format reserves for it.
// The C upstream silently truncates via MSG_WriteShort's int->short
// cast; the Go port surfaces the condition so callers can route the
// bug rather than ship a corrupted snapshot. tyrquake:
// SV_CreateBaseline's MSG_WriteShort(&sv.signon, entnum).
var ErrEntityRange = errors.New("server: entityNum outside [0, 0xffff]")

// ErrBaselineNeedsFitz fires when a baseline carries a modelIndex
// or frame >= 256 but the active protocol is plain NQ (which can
// only emit a single byte for either field). BJP* protocols always
// widen modelIndex to a short and never tripped this branch; only
// NQ does. tyrquake: silently truncates -- the Go port refuses.
var ErrBaselineNeedsFitz = errors.New("server: baseline modelIndex/frame >= 256 only encodable on FITZ protocol")

// EntityBaseline is the per-edict snapshot the server writes at
// SV_SpawnServer time so clients can delta-encode subsequent
// entity updates against a known starting state. tyrquake:
// entity_state_t (the v.baseline field on edict_t).
type EntityBaseline struct {
	ModelIndex int // sv.model_precache slot
	Frame      int // alias frame number
	ColorMap   int // player slot (0 = world / non-player)
	SkinNum    int // alias skin selector
	Origin     [3]float32
	Angles     [3]float32 // pitch / yaw / roll (degrees, 1/256-circle quantisation)
	Alpha      int        // FITZ-only; 0 = ENTALPHA_DEFAULT (omitted from wire)
}

// EncodeBaseline writes one svc_spawnbaseline (or its FITZ
// svc_fitz_spawnbaseline2 variant when wide-field bits are needed)
// for entityNum + baseline into buf. tyrquake: the body of the
// per-edict for-loop inside SV_CreateBaseline (NQ/sv_main.c lines
// 1146-1209).
//
// Wire layout (vanilla svc_spawnbaseline):
//
//	byte    svc_spawnbaseline           OR  byte svc_fitz_spawnbaseline2
//	short   entityNum                       short entityNum
//	                                        byte  fitz-bits
//	byte    modelIndex                  OR  short modelIndex (B_FITZ_LARGEMODEL)
//	byte    frame                       OR  short frame      (B_FITZ_LARGEFRAME)
//	byte    colormap
//	byte    skinnum
//	for axis in 0..2:
//	  coord   origin[axis]
//	  angle   angles[axis]
//	[byte alpha]                            iff B_FITZ_ALPHA
//
// FITZ-bit derivation (only on protocol.VersionFitz):
//
//   - B_FITZ_LARGEMODEL iff (modelIndex & 0xff00) != 0 (idx >= 256)
//   - B_FITZ_LARGEFRAME iff (frame & 0xff00) != 0      (frame >= 256)
//   - B_FITZ_ALPHA      iff alpha != ENTALPHA_DEFAULT
//
// For non-FITZ protocols the function refuses to silently clamp:
//
//   - modelIndex >= 256 on NQ: returns ErrBaselineNeedsFitz
//   - frame >= 256 on any non-FITZ:      returns ErrBaselineNeedsFitz
//   - alpha is ignored (the wire format doesn't carry it).
//
// BJP / BJP2 / BJP3 write modelIndex as a short regardless of size
// (their SV_WriteModelIndex branch), so they tolerate any
// modelIndex in [0, 0xffff].
func EncodeBaseline(buf *sizebuf.Buffer, entityNum int, baseline EntityBaseline, version int) error {
	if buf == nil {
		return errors.New("server: nil sizebuf")
	}
	if entityNum < 0 || entityNum > 0xffff {
		return ErrEntityRange
	}

	// Derive FITZ bits + reject would-be silent truncations on the
	// non-FITZ protocols.
	bits := 0
	if version == protocol.VersionFitz {
		if baseline.ModelIndex&0xff00 != 0 {
			bits |= protocol.BFitzLargeModel
		}
		if baseline.Frame&0xff00 != 0 {
			bits |= protocol.BFitzLargeFrame
		}
		if baseline.Alpha != protocol.EntAlphaDefault {
			bits |= protocol.BFitzAlpha
		}
	} else {
		// modelIndex widening: NQ writes a byte and would silently
		// truncate; BJP* always write a short and tolerate >=256.
		if baseline.ModelIndex&0xff00 != 0 && version == protocol.VersionNQ {
			return ErrBaselineNeedsFitz
		}
		// Frame on any non-FITZ protocol is a single byte: refuse.
		if baseline.Frame&0xff00 != 0 {
			return ErrBaselineNeedsFitz
		}
	}

	// Opcode + entityNum (+ bits on FITZ).
	if bits != 0 {
		if err := msg.WriteByte(buf, protocol.SvcFitzSpawnBaseline2); err != nil {
			return err
		}
		if err := msg.WriteShort(buf, entityNum); err != nil {
			return err
		}
		if err := msg.WriteByte(buf, bits); err != nil {
			return err
		}
	} else {
		if err := msg.WriteByte(buf, protocol.SvcSpawnBaseline); err != nil {
			return err
		}
		if err := msg.WriteShort(buf, entityNum); err != nil {
			return err
		}
	}

	// modelIndex: NQ -> byte; BJP* -> short; FITZ -> short iff
	// LARGEMODEL else byte (mirrors SV_WriteModelIndex).
	if err := writeBaselineModelIndex(buf, baseline.ModelIndex, bits, version); err != nil {
		return err
	}

	// frame: short on FITZ+LARGEFRAME, byte otherwise.
	if bits&protocol.BFitzLargeFrame != 0 {
		if err := msg.WriteShort(buf, baseline.Frame); err != nil {
			return err
		}
	} else {
		if err := msg.WriteByte(buf, baseline.Frame); err != nil {
			return err
		}
	}

	if err := msg.WriteByte(buf, baseline.ColorMap); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, baseline.SkinNum); err != nil {
		return err
	}

	for axis := 0; axis < 3; axis++ {
		if err := msg.WriteCoord(buf, baseline.Origin[axis]); err != nil {
			return err
		}
		if err := msg.WriteAngle(buf, baseline.Angles[axis]); err != nil {
			return err
		}
	}

	if bits&protocol.BFitzAlpha != 0 {
		if err := msg.WriteByte(buf, baseline.Alpha); err != nil {
			return err
		}
	}

	return nil
}

// writeBaselineModelIndex matches SV_WriteModelIndex's per-protocol
// width dispatch for the msgtype_baseline call site. NQ -> byte
// (any &0xff00 was already rejected upstream); BJP* -> short; FITZ
// -> short iff B_FITZ_LARGEMODEL else byte.
func writeBaselineModelIndex(buf *sizebuf.Buffer, idx, bits, version int) error {
	switch version {
	case protocol.VersionBJP, protocol.VersionBJP2, protocol.VersionBJP3:
		return msg.WriteShort(buf, idx)
	case protocol.VersionFitz:
		if bits&protocol.BFitzLargeModel != 0 {
			return msg.WriteShort(buf, idx)
		}
		return msg.WriteByte(buf, idx)
	}
	// VersionNQ (the only remaining branch the caller-side guards
	// admit; unknown versions degrade to NQ's single byte, matching
	// upstream's "default" Host_Error -- the Go port has no equivalent
	// of Host_Error here so the byte width is the safest fallback).
	return msg.WriteByte(buf, idx)
}
