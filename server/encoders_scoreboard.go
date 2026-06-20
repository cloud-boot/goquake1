// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"fmt"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// ErrSlotRange is returned by the per-client scoreboard encoders
// (EncodeUpdateName, EncodeUpdateColors, EncodeUpdateFrags) when the
// slot index does not fit the wire's one-byte unsigned client-slot
// field. The C upstream silently truncates via MSG_WriteByte's
// (byte)cast; the Go port validates explicitly to surface caller
// bugs (a 256-slot scoreboard would otherwise alias slot 0).
var ErrSlotRange = errors.New("server: client slot outside [0, 255]")

// ErrFragsRange is returned by EncodeUpdateFrags when the frag count
// does not fit a signed 16-bit short. The protocol's svc_updatefrags
// field is a signed short (so negatives are legal -- self-kills push
// the count below zero); values outside [-32768, 32767] would wrap on
// the wire and corrupt the client's scoreboard.
var ErrFragsRange = errors.New("server: frags outside int16 range [-32768, 32767]")

// ErrStatRange is returned by EncodeUpdateStat when the stat index
// does not fit the wire's one-byte unsigned stat-index field. The
// protocol caps STAT_* at MAX_CL_STATS = 32 in tyrquake, but the
// wire slot itself is a full byte; the Go port enforces the wire
// bound only so engine-private stat extensions remain valid.
var ErrStatRange = errors.New("server: stat index outside [0, 255]")

// EncodeUpdateName writes svc_updatename + slot byte + NUL-terminated
// name string. Sent when a client's display name changes (e.g. via
// the "name" console command). tyrquake: emitted inline by
// SV_ExtractFromUserinfo / by the name-change branch in host_cmd.c.
//
// slot must be in [0, 255] (one wire byte); returns [ErrSlotRange]
// otherwise with no bytes written.
func EncodeUpdateName(buf *sizebuf.Buffer, slot int, name string) error {
	if buf == nil {
		return ErrNilBuf
	}
	if slot < 0 || slot > 0xff {
		return fmt.Errorf("%w: %d", ErrSlotRange, slot)
	}
	if err := msg.WriteByte(buf, protocol.SvcUpdateName); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, slot); err != nil {
		return err
	}
	return msg.WriteString(buf, name)
}

// EncodeUpdateColors writes svc_updatecolors + slot byte + colors
// byte. colors is the packed shirt+pants palette (4 bits each;
// the value occupies a full [0, 255] wire byte). tyrquake: emitted
// by the "color" console command handler.
//
// slot and colors must each be in [0, 255]; returns [ErrSlotRange]
// when slot is out of range (no bytes written). colors out of range
// is reported via the same sentinel since both are single-byte
// scoreboard fields with identical semantics.
func EncodeUpdateColors(buf *sizebuf.Buffer, slot int, colors int) error {
	if buf == nil {
		return ErrNilBuf
	}
	if slot < 0 || slot > 0xff {
		return fmt.Errorf("%w: %d", ErrSlotRange, slot)
	}
	if colors < 0 || colors > 0xff {
		return fmt.Errorf("%w: colors=%d", ErrSlotRange, colors)
	}
	if err := msg.WriteByte(buf, protocol.SvcUpdateColors); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, slot); err != nil {
		return err
	}
	return msg.WriteByte(buf, colors)
}

// EncodeUpdateFrags writes svc_updatefrags + slot byte + frags short.
// Sent once per scoreboard refresh per player whose frag count
// changed since the last update. tyrquake: SV_UpdateToReliableMessages
// emits one per dirty client.
//
// slot must be in [0, 255]; frags must be in [-32768, 32767].
// Returns [ErrSlotRange] or [ErrFragsRange] respectively (no bytes
// written on either rejection).
func EncodeUpdateFrags(buf *sizebuf.Buffer, slot int, frags int) error {
	if buf == nil {
		return ErrNilBuf
	}
	if slot < 0 || slot > 0xff {
		return fmt.Errorf("%w: %d", ErrSlotRange, slot)
	}
	if frags < -0x8000 || frags > 0x7fff {
		return fmt.Errorf("%w: %d", ErrFragsRange, frags)
	}
	if err := msg.WriteByte(buf, protocol.SvcUpdateFrags); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, slot); err != nil {
		return err
	}
	return msg.WriteShort(buf, frags)
}

// EncodeUpdateStat writes svc_updatestat + stat byte + value long.
// Used to push per-player stat fields (HP, ammo, items, ...) to a
// client outside the normal svc_clientdata frame -- typically when
// a stat changes mid-frame and needs an immediate sync. tyrquake:
// PF_Stat builtin + SV_WriteClientdataToMessage's fallback path.
//
// stat must be in [0, 255]; returns [ErrStatRange] otherwise (no
// bytes written). value covers the full int32 range on the wire.
func EncodeUpdateStat(buf *sizebuf.Buffer, stat int, value int32) error {
	if buf == nil {
		return ErrNilBuf
	}
	if stat < 0 || stat > 0xff {
		return fmt.Errorf("%w: %d", ErrStatRange, stat)
	}
	if err := msg.WriteByte(buf, protocol.SvcUpdateStat); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, stat); err != nil {
		return err
	}
	return msg.WriteLong(buf, value)
}

// EncodeKilledMonster writes a single svc_killedmonster byte.
// Triggers the client's "n/m" monsters-killed HUD counter increment.
// tyrquake: PF_killed_monster builtin.
func EncodeKilledMonster(buf *sizebuf.Buffer) error {
	if buf == nil {
		return ErrNilBuf
	}
	return msg.WriteByte(buf, protocol.SvcKilledMonster)
}

// EncodeFoundSecret writes a single svc_foundsecret byte. Triggers
// the "secret found!" announcement + HUD secret counter increment.
// tyrquake: PF_found_secret builtin.
func EncodeFoundSecret(buf *sizebuf.Buffer) error {
	if buf == nil {
		return ErrNilBuf
	}
	return msg.WriteByte(buf, protocol.SvcFoundSecret)
}

// EncodeSellScreen writes a single svc_sellscreen byte. Used by
// shareware Quake to swap the renderer to the "buy the full game"
// screen at end-of-episode. tyrquake: PF_sell_screen builtin.
func EncodeSellScreen(buf *sizebuf.Buffer) error {
	if buf == nil {
		return ErrNilBuf
	}
	return msg.WriteByte(buf, protocol.SvcSellScreen)
}
