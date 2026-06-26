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

// ErrDatagramFull fires when EncodeParticle is asked to write into
// a datagram that's nearly full -- the C upstream's silent drop
// path ("Drop silently if there is no room"). The Go port returns
// it so callers can decide between silent-drop and log-and-continue
// rather than swallowing the failure unconditionally.
var ErrDatagramFull = errors.New("server: datagram would overflow on particle write")

// particleReserve is the byte budget EncodeParticle reserves at the
// END of the datagram to avoid mid-message truncation. The C upstream
// uses MAX_DATAGRAM - 16 as its bail threshold (svc_particle uses
// 12 bytes: 1 cmd + 6 coords + 3 dir + 1 count + 1 color); the Go
// port rounds up to 16 to match the upstream margin verbatim.
const particleReserve = 16

// EncodeParticle writes one svc_particle temp-entity event into buf:
// a particle burst at org with direction dir, palette color (0..255),
// and count (0..255) particles. tyrquake: the message-writing tail of
// SV_StartParticle in NQ/sv_main.c (the sv.datagram bail + the
// svc_particle byte sequence).
//
// Wire shape (12 bytes):
//
//	byte    svc_particle           (cmd)
//	coord   org[0]                 (2 bytes, 1/8 fixed-point)
//	coord   org[1]                 (2 bytes)
//	coord   org[2]                 (2 bytes)
//	char    clamp(dir[i]*16)       (3 bytes, one per axis, [-128, 127])
//	byte    count
//	byte    color
//
// Returns [ErrDatagramFull] without writing anything if buf is
// within particleReserve bytes of its capacity (matches the
// upstream's "if cursize > MAX_DATAGRAM - 16" early-return).
func EncodeParticle(buf *sizebuf.Buffer, org, dir [3]float32, color, count int) error {
	if buf == nil {
		return errors.New("server: nil sizebuf")
	}
	if buf.Len() > MaxDatagram-particleReserve {
		return ErrDatagramFull
	}
	if err := msg.WriteByte(buf, protocol.SvcParticle); err != nil {
		return err
	}
	for axis := 0; axis < 3; axis++ {
		if err := msg.WriteCoord(buf, org[axis]); err != nil {
			return err
		}
	}
	for axis := 0; axis < 3; axis++ {
		v := dir[axis] * 16
		// Clamp to int8 range without relying on float->int wrap.
		switch {
		case v > 127:
			v = 127
		case v < -128:
			v = -128
		}
		if err := msg.WriteChar(buf, int(v)); err != nil {
			return err
		}
	}
	if err := msg.WriteByte(buf, count); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, color); err != nil {
		return err
	}
	return nil
}

// ErrLightningKind is returned by [EncodeLightning] when kind is not
// one of the four canonical lightning/beam sub-types (TE_LIGHTNING1 /
// 2 / 3 / TE_BEAM). The upstream Sys_Errors on a bad sub-type byte;
// the Go port surfaces a recoverable error so caller mistakes don't
// kill the whole tick.
var ErrLightningKind = errors.New("server: EncodeLightning: kind not in {TELightning1, TELightning2, TELightning3, TEBeam}")

// lightningReserve is the byte budget [EncodeLightning] reserves at
// the END of the datagram. Wire size = 2 (svc + kind) + 2 (entity
// short) + 6 (start coord triple) + 6 (end coord triple) = 16 bytes;
// the upstream's bail margin is `MAX_DATAGRAM - 16` (same as
// particleReserve). Rounded to 24 here so the same threshold protects
// any svc_temp_entity body whose payload extends past the canonical
// 16-byte shape.
const lightningReserve = 24

// EncodeLightning writes one svc_temp_entity LIGHTNING/BEAM event into
// buf: the lightning sub-type byte (TE_LIGHTNING1 / 2 / 3 / TE_BEAM),
// the owning entity index, and the (start, end) coord triples
// describing the traceline the bolt was fired along. tyrquake: the
// `MSG_WriteByte(&sv.datagram, svc_temp_entity); MSG_WriteByte(...,
// TE_LIGHTNING2); MSG_WriteShort(..., NUM_FOR_EDICT(self));
// MSG_WriteCoord(..., start[i]); MSG_WriteCoord(..., end[i]);` body
// inside PF_WriteCoord-bearing builtins -- the same shape the
// client's [client.SvcReader.decodeTELightning] reads back.
//
// Wire shape (16 bytes):
//
//	byte    svc_temp_entity        (cmd)
//	byte    TE_LIGHTNING[1|2|3] |
//	        TE_BEAM                (sub-type)
//	short   owning entity index
//	coord*3 start                  (6 bytes)
//	coord*3 end                    (6 bytes)
//
// Returns [ErrLightningKind] when kind isn't recognized;
// [ErrDatagramFull] when buf is within lightningReserve bytes of its
// capacity; any propagated msg.Write* error otherwise.
func EncodeLightning(buf *sizebuf.Buffer, kind, ent int, start, end [3]float32) error {
	if buf == nil {
		return errors.New("server: nil sizebuf")
	}
	switch kind {
	case protocol.TELightning1, protocol.TELightning2, protocol.TELightning3, protocol.TEBeam:
	default:
		return ErrLightningKind
	}
	if buf.Len() > MaxDatagram-lightningReserve {
		return ErrDatagramFull
	}
	if err := msg.WriteByte(buf, protocol.SvcTempEntity); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, kind); err != nil {
		return err
	}
	if err := msg.WriteShort(buf, ent); err != nil {
		return err
	}
	for axis := 0; axis < 3; axis++ {
		if err := msg.WriteCoord(buf, start[axis]); err != nil {
			return err
		}
	}
	for axis := 0; axis < 3; axis++ {
		if err := msg.WriteCoord(buf, end[axis]); err != nil {
			return err
		}
	}
	return nil
}
