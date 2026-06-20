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
