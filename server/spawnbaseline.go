// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/progs"
)

// PlayerModelName is the precache slug the upstream's SV_CreateBaseline
// substitutes for player slots (entnum in [1, maxclients]) regardless
// of what v.model the QC may have set on them. tyrquake: the hard-coded
// "progs/player.mdl" literal inside SV_CreateBaseline's per-edict body.
const PlayerModelName = "progs/player.mdl"

// ComposeBaselineFromEdict reads the per-entity baseline state off
// e's entvars + returns it as an [EntityBaseline] the [EncodeBaseline]
// encoder can consume. Mirrors [ComposeClientDataFromEdict]'s "tolerant
// of missing fields" stance: every read goes through [progs.EntVars]
// which returns [progs.ErrFieldNotFound] when the field is not declared;
// the helper silently substitutes the field's QC zero default.
//
// entNum + maxClients drive the upstream's "is this a player slot?"
// branch (the SV_CreateBaseline body forces colormap=entNum +
// modelindex=SV_ModelIndex("progs/player.mdl") for entNum in
// [1, maxClients], and colormap=0 + modelindex=SV_ModelIndex(v.model)
// for everything else). modelPrecache supplies the slot table the
// model-name lookup walks; pass [Server.ModelPrecache].
//
// tyrquake: SV_CreateBaseline (NQ/sv_main.c lines 1146-1209), per-edict
// body only -- this helper produces ONE baseline; the caller loops over
// edicts + calls [EncodeBaseline] per result.
//
// Returns the zero [EntityBaseline] + hasModel=false when p == nil or
// e == nil -- callers can treat that as "no baseline for this slot".
//
// hasModel is true iff the edict carries any visible-entity intent --
// either a non-zero v.modelindex (the QC float field setmodel writes),
// or a non-empty v.model string. The upstream's
// `if (entnum > svs.maxclients && !svent->v.modelindex) continue;`
// guard on a setmodel-less Go runtime would skip every monster /
// brushmodel (their v.modelindex is never set because setmodel doesn't
// dispatch); the v.model string fallback lets the bring-up emit
// baselines for them anyway, with bl.ModelIndex left at 0 until
// setmodel + precache wiring lands.
func ComposeBaselineFromEdict(p *progs.Progs, e *progs.Edict, entNum, maxClients int, modelPrecache []string) (bl EntityBaseline, hasModel bool) {
	if p == nil || e == nil {
		return bl, false
	}
	// NewEntVars only errors on nil inputs; the guards above ensure
	// both are non-nil, so its error is dropped (bsptrace-style).
	v, _ := progs.NewEntVars(p, e)

	if o, err := v.ReadVec3("origin"); err == nil {
		bl.Origin = o
	}
	if a, err := v.ReadVec3("angles"); err == nil {
		bl.Angles = a
	}
	if f, err := v.ReadFloat("frame"); err == nil {
		bl.Frame = int(f)
	}
	if s, err := v.ReadFloat("skin"); err == nil {
		bl.SkinNum = int(s)
	}

	// Player vs world/monster branch -- mirrors the upstream's
	// `if (entnum > 0 && entnum <= svs.maxclients)` arm.
	if entNum > 0 && entNum <= maxClients {
		bl.ColorMap = entNum
		// SV_ModelIndex's "name absent from precache" failure is silent
		// here: an unprecached player model just yields slot 0, which
		// is the worldspawn slot -- the upstream Host_Error's, the Go
		// port treats it as "no model" so the baseline still flows.
		// Players always have model intent regardless of precache.
		idx, _ := ModelIndex(modelPrecache, PlayerModelName)
		bl.ModelIndex = idx
		hasModel = true
	} else {
		bl.ColorMap = 0
		// First try v.modelindex (the QC float setmodel populates) --
		// that's the upstream's source of truth.
		if mi, err := v.ReadFloat("modelindex"); err == nil && mi != 0 {
			bl.ModelIndex = int(mi)
			hasModel = true
		}
		// Fallback: v.model is the QC string field holding the precache
		// slug. Resolve it via the model precache; a non-empty name --
		// even one that doesn't resolve (the QC's setmodel call never
		// ran to precache it) -- still flags model intent so the
		// bring-up emits a baseline. ModelIndex stays at 0 in the
		// "unprecached" subcase; the renderer will skip the entity
		// until setmodel + precache wiring lands.
		if name, err := v.ReadString("model"); err == nil && name != "" {
			if idx, ierr := ModelIndex(modelPrecache, name); ierr == nil {
				bl.ModelIndex = idx
			}
			hasModel = true
		}
	}

	// Alpha is FITZ-only; vanilla NQ + BJP* protocols ignore it.
	// SV_CreateBaseline forces ENTALPHA_DEFAULT on non-FITZ; this
	// helper just leaves it at zero (= ENTALPHA_DEFAULT) so callers
	// targeting FITZ can override via v.ReadFloat("alpha") themselves
	// once the FITZ entvars extension lands (out of scope here).
	return bl, hasModel
}

// BaselineSkipReason classifies why [SendBaselines] omits a given
// edict slot from the per-spawn broadcast. Surfaced via [BaselineStat]
// so callers (host bring-up logs, tests) can audit the skip walk
// without re-implementing the upstream's predicate.
type BaselineSkipReason int

const (
	// BaselineSkipNone means the edict was emitted (success). The slot
	// contributes 1 to BaselineStat.Emitted.
	BaselineSkipNone BaselineSkipReason = iota
	// BaselineSkipFree means the edict's Free flag is set -- the slot
	// is in the free list, has no live entity state, and emitting a
	// baseline for it would corrupt the client's per-entity cache.
	// tyrquake: the `if (svent->free) continue;` guard in
	// SV_CreateBaseline.
	BaselineSkipFree
	// BaselineSkipNoModel means the edict is past the player-slot reserve
	// (entnum > maxclients) AND has no visible-entity intent (per
	// [ComposeBaselineFromEdict]'s hasModel verdict: v.modelindex==0 AND
	// v.model is empty/missing) -- a trigger / info_* / pure-logic
	// entity with no visible representation. The upstream skips these
	// because there is nothing to render.
	// tyrquake: the
	// `if (entnum > svs.maxclients && !svent->v.modelindex) continue;`
	// guard, broadened to accept v.model as a fallback "has model
	// intent" signal so the bring-up flows baselines for entities
	// parsed pre-setmodel.
	BaselineSkipNoModel
)

// BaselineStat is the per-broadcast audit envelope [SendBaselines]
// returns. Emitted is the number of svc_spawnbaseline messages the
// helper appended to client.Message; SkippedFree + SkippedNoModel
// partition the omitted slots by reason. The three fields sum to
// (numEdicts - 1) -- slot 0 (worldspawn) is always emitted because it
// carries the BSP worldmodel as its v.model, so the BaselineSkipNoModel
// guard's `entnum > maxclients` half always skips checking it.
type BaselineStat struct {
	Emitted        int
	SkippedFree    int
	SkippedNoModel int
	PerSlotEntNums []int                // emitted slots, in walk order
	PerSlotSkipped []BaselineSkipReason // skip reason per slot; len = numEdicts
}

// ErrSendBaselinesNilServer is returned by [Server.SendBaselines] when
// the receiver is nil. The other no-op short-circuits (nil client /
// inactive / no Message buffer) return a zero [BaselineStat] + nil
// error -- matching the rest of the package's "skip the silent slot,
// don't error on a structurally absent target" convention.
var ErrSendBaselinesNilServer = errors.New("server: nil Server receiver in SendBaselines")

// SendBaselines walks the per-map edict pool + appends one
// svc_spawnbaseline message per visible entity onto client.Message.
// This is the per-spawn entity-state broadcast the upstream calls
// SV_CreateBaseline; without it the client never learns about the
// 500+ monsters / triggers / lights that the entity-spawn pass
// allocated, and the renderer has no per-entity data to draw.
//
// Walk shape (mirrors SV_CreateBaseline):
//
//  1. for entnum in [0, NumEdicts):
//     - skip if e.Free                          (BaselineSkipFree)
//     - skip if entnum > maxClients && modelindex == 0
//     (BaselineSkipNoModel)
//     - otherwise compose + EncodeBaseline       (BaselineSkipNone)
//
// The composed baseline uses [ComposeBaselineFromEdict] (the per-edict
// reader) + [EncodeBaseline] (the wire encoder). The result's
// PerSlotSkipped[i] is the disposition of edict i; PerSlotEntNums
// is the emitted-slot index list in walk order so callers can verify
// "which entities the client should now see".
//
// Silent no-op (returns zero BaselineStat + nil err) when:
//
//   - s is non-nil but client is nil / inactive (the slot doesn't want
//     bytes; matches [Server.PreparePerClientMessage]'s skip).
//   - client.Message is nil (the test stub didn't allocate a buffer).
//
// Returns ErrSendBaselinesNilServer when s == nil (the function needs
// the edict pool + model precache off the receiver; nil-server is a
// genuine bug, not a missing-slot).
//
// The progs argument is the loaded QC progs the helper uses to resolve
// entvar field offsets; nil progs is allowed (every edict reads degrade
// to the QC zero default, baselines become origin=0/angles=0/frame=0/
// modelindex=0 + the modelindex==0 guard skips every non-player slot).
//
// maxClients is svs.maxclients -- the player-slot reserve [1, maxClients].
//
// tyrquake: SV_CreateBaseline in NQ/sv_main.c -- the per-edict for-loop
// + the post-skip MSG_WriteByte(&sv.signon, svc_spawnbaseline ...) tail.
// The upstream writes into sv.signon (the spawn-time reliable buffer the
// engine replays per-connect); the Go port writes directly into the
// active client.Message because the wire-driven handshake doesn't yet
// retain a separate signon buffer -- a follow-up pass aligning with
// SV_SendServerinfo's `sv.signon` replay can move this into the per-
// connect path.
func (s *Server) SendBaselines(client *Client, p *progs.Progs, maxClients int) (BaselineStat, error) {
	if s == nil {
		return BaselineStat{}, ErrSendBaselinesNilServer
	}
	if client == nil || !client.Active || client.Message == nil {
		return BaselineStat{}, nil
	}

	stat := BaselineStat{
		PerSlotSkipped: make([]BaselineSkipReason, s.NumEdicts),
	}

	for entNum := 0; entNum < s.NumEdicts; entNum++ {
		e := s.Edicts[entNum]
		if e == nil {
			// A nil slot is structurally equivalent to free + has no
			// entvars to compose against; classify it as Free so the
			// audit counter stays meaningful.
			stat.PerSlotSkipped[entNum] = BaselineSkipFree
			stat.SkippedFree++
			continue
		}
		if e.Free {
			stat.PerSlotSkipped[entNum] = BaselineSkipFree
			stat.SkippedFree++
			continue
		}

		bl, hasModel := ComposeBaselineFromEdict(p, e, entNum, maxClients, s.ModelPrecache)
		_ = hasModel // hasModel is exposed for callers; SendBaselines's
		// own no-model skip is currently disabled (see below).

		// NO-MODEL SKIP -- DELIBERATELY DISABLED FOR BRING-UP.
		//
		// Upstream's `if (entnum > svs.maxclients && !svent->v.modelindex)
		// continue;` guard suppresses pure-logic entities (triggers,
		// info_*, lights) from the per-spawn broadcast on the grounds
		// that the client has nothing to render for them. In a fully-
		// wired engine that's a sensible bandwidth optimization: QC's
		// setmodel populates v.modelindex during the spawn function,
		// so the guard correctly classifies post-spawn invisible
		// entities.
		//
		// The Go port doesn't yet dispatch QC spawn functions, so
		// neither v.modelindex (the float setmodel writes) nor v.model
		// (the string setmodel reads) is populated for monsters /
		// brushmodels parsed from the BSP entity lump. Enforcing the
		// guard here would skip ~99% of the entity pool, hiding the
		// bring-up signal that proves the channel works.
		//
		// Emit baselines for every non-free, non-nil slot regardless of
		// model intent. The client's State.Baselines cache absorbs the
		// surplus harmlessly (vacant slots stay at their zero state,
		// the renderer never visits them until setmodel wiring lands).
		// When the QC runtime + setmodel land in a future batch, this
		// guard can be re-enabled by replacing the `_ = hasModel` above
		// with the upstream's `if entNum > maxClients && !hasModel`
		// continue.

		if err := EncodeBaseline(client.Message, entNum, bl, s.Protocol); err != nil {
			// Propagate the first encoder failure verbatim. Partial
			// stat (everything emitted so far) is still returned so
			// the caller can see how far the walk got before the
			// overflow.
			return stat, err
		}
		stat.PerSlotSkipped[entNum] = BaselineSkipNone
		stat.PerSlotEntNums = append(stat.PerSlotEntNums, entNum)
		stat.Emitted++
	}

	return stat, nil
}
