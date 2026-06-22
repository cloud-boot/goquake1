// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
)

// EntityUpdateStat is the per-tic audit envelope [Server.SendEntityUpdates]
// returns. Emitted is the number of svc_update messages the helper
// appended to client.Message; Skipped is the count of edict slots the
// walk visited but did not emit (free / nil slots). The two fields
// sum to (NumEdicts - 1) on a fully-populated map: slot 0 (worldspawn)
// is always emitted because it carries the BSP worldmodel and has no
// "free" flag in the spawn path.
//
// PerSlotEmitted is the entNum list (in walk order) the helper actually
// emitted -- mirrors [BaselineStat.PerSlotEntNums] so callers can audit
// "which entities just landed on the wire".
type EntityUpdateStat struct {
	Emitted        int
	Skipped        int
	PerSlotEmitted []int
}

// ErrSendEntityUpdatesNilServer is returned by
// [Server.SendEntityUpdates] when the receiver is nil. Mirrors the
// other "structurally absent receiver = bug, not skip" sentinels in
// the package ([ErrSendBaselinesNilServer], ErrNilBuf...).
var ErrSendEntityUpdatesNilServer = errors.New("server: nil Server receiver in SendEntityUpdates")

// fullUpdateBits is the per-tic update mask the bring-up emits for
// every visible entity: all 3 origin axes + all 3 angle axes. The
// non-axis bits (U_MODEL, U_FRAME, U_COLORMAP, U_SKIN, U_EFFECTS) are
// gated separately when the per-edict reader yields non-default values.
//
// USignal is OR'd in by [EncodeUpdate] itself -- callers must NOT
// pre-fold it into Bits.
const fullUpdateBits = protocol.UOrigin1 | protocol.UOrigin2 | protocol.UOrigin3 |
	protocol.UAngle1 | protocol.UAngle2 | protocol.UAngle3

// SendEntityUpdates walks the per-map edict pool + appends one
// svc_update message per visible entity onto client.Message. This is
// the per-tic equivalent of the signon-time [Server.SendBaselines]
// broadcast -- without it the client only sees the spawn-time
// baselines and never learns that monsters are walking around,
// animation frames are advancing, or projectiles are moving.
//
// Walk shape (mirrors the inner body of SV_WriteEntitiesToClient,
// minus PVS culling + delta bit derivation):
//
//  1. for entnum in [1, NumEdicts):   (skip worldspawn at slot 0)
//     - skip if e == nil or e.Free                 (EntityUpdateStat.Skipped++)
//     - otherwise compose + EncodeUpdate            (EntityUpdateStat.Emitted++)
//
// Bring-up scope:
//
//   - Bits is always fullUpdateBits (all 3 origins + all 3 angles)
//     OR'd with U_MODEL/U_FRAME/U_COLORMAP/U_SKIN/U_EFFECTS when the
//     composed value is non-zero. This is intentionally NOT a delta
//     against the baseline -- the wire shape is correct but bandwidth-
//     optimal delta encoding lives in a follow-up batch.
//   - U_MOREBITS is folded in by [EncodeUpdate] iff any high-byte
//     bit is present; the caller-side mask construction below uses
//     [protocol.UMoreBits] explicitly when U_ANGLE1/U_ANGLE3/U_MODEL/
//     U_COLORMAP/U_SKIN/U_EFFECTS are set so the encoder gets a
//     well-formed bitmask. U_LONGENTITY is folded for entnum > 255.
//   - PVS filtering is deliberately disabled: every non-free,
//     non-nil slot is emitted regardless of visibility. The client-
//     side renderer is the next gatekeeper for "what to actually
//     draw"; this helper just gets the bytes onto the wire.
//
// Silent no-op (returns zero EntityUpdateStat + nil err) when:
//
//   - s is non-nil but client is nil / inactive / unspawned (matches
//     [Server.PreparePerClientMessage]'s skip).
//   - client.Message is nil (the test stub didn't allocate a buffer).
//
// Returns ErrSendEntityUpdatesNilServer when s == nil (the function
// needs the edict pool + model precache off the receiver; nil-server
// is a genuine bug, not a missing-slot).
//
// The progs argument is the loaded QC progs the helper uses to resolve
// entvar field offsets; nil progs is allowed (every edict reads degrade
// to the QC zero default, the resulting update carries
// origin=(0,0,0)/angles=(0,0,0)/model=0/frame=0).
//
// maxClients is svs.maxclients -- shapes the player-slot branch of
// [ComposeBaselineFromEdict] (which this helper re-uses as the current-
// state reader; the upstream's per-tic SV_WriteEntitiesToClient builds
// the same fields off the live edict, so the helper is the correct
// shape for "current state" too, not just the spawn-time baseline).
//
// tyrquake: the per-edict body of SV_WriteEntitiesToClient in
// NQ/sv_main.c -- skipping the U_* delta-bit derivation + the
// PVS-set-check guard + the FITZ extend bits.
func (s *Server) SendEntityUpdates(client *Client, p *progs.Progs, maxClients int) (EntityUpdateStat, error) {
	if s == nil {
		return EntityUpdateStat{}, ErrSendEntityUpdatesNilServer
	}
	if client == nil || !client.Active || !client.Spawned || client.Message == nil {
		return EntityUpdateStat{}, nil
	}

	var stat EntityUpdateStat

	// Walk every slot from 1 (skip world at 0; worldspawn never moves
	// per-tic + its baseline already carries the BSP model index).
	for entNum := 1; entNum < s.NumEdicts; entNum++ {
		e := s.Edicts[entNum]
		if e == nil || e.Free {
			stat.Skipped++
			continue
		}

		// Re-use the baseline composer: SV_WriteEntitiesToClient builds
		// the per-tic entity_state_t off the SAME entvars the spawn-time
		// baseline reads (origin / angles / frame / skin / colormap /
		// modelindex), just with the live (post-physics) values rather
		// than the spawn-time snapshot.
		bl, _ := ComposeBaselineFromEdict(p, e, entNum, maxClients, s.ModelPrecache)

		bits := fullUpdateBits
		update := EntityUpdate{
			Origin: bl.Origin,
			Angles: bl.Angles,
		}

		// Optional fields gated on non-default values. The vanilla NQ
		// upstream delta-encodes these against the baseline; here we
		// just emit them whenever the live value is non-zero so the
		// client sees the current state even before delta math lands.
		if bl.ModelIndex != 0 {
			bits |= protocol.UModel
			update.Model = bl.ModelIndex
		}
		if bl.Frame != 0 {
			bits |= protocol.UFrame
			update.Frame = bl.Frame
		}
		if bl.ColorMap != 0 {
			bits |= protocol.UColorMap
			update.ColorMap = bl.ColorMap
		}
		if bl.SkinNum != 0 {
			bits |= protocol.USkin
			update.Skin = bl.SkinNum
		}
		// Effects isn't carried by EntityBaseline; the bring-up never
		// emits it. A follow-up batch can read v.effects directly off
		// the edict and set U_EFFECTS here.

		// U_LONGENTITY for entNum > 255 (the byte-wide entity field
		// can't carry it).
		if entNum > 0xff {
			bits |= protocol.ULongEntity
		}

		// U_MOREBITS iff any high-byte bit is set -- the encoder
		// emits the second bits byte only when the caller flags it.
		if bits&0xff00 != 0 {
			bits |= protocol.UMoreBits
		}

		update.Bits = bits

		if err := EncodeUpdate(client.Message, entNum, update); err != nil {
			// Propagate the first encoder failure verbatim. Partial
			// stat (everything emitted so far) is still returned so
			// the caller can see how far the walk got before the
			// overflow.
			return stat, err
		}
		stat.PerSlotEmitted = append(stat.PerSlotEmitted, entNum)
		stat.Emitted++
	}

	return stat, nil
}
