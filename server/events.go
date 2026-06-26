// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
)

// ErrNilServer is returned by the *Server-bound wrappers when called
// on a nil receiver. The C upstream segfaults; the Go port surfaces
// the failure so a buggy caller doesn't take the whole server tick
// down.
var ErrNilServer = errors.New("server: nil *Server receiver")

// ErrNilDatagram is returned when a *Server-bound wrapper is called
// on a Server whose Datagram buffer hasn't been allocated. Constructed
// servers built via [NewServer] always have one; this guards the
// hand-constructed Server{} path the tests use.
var ErrNilDatagram = errors.New("server: nil Server.Datagram")

// StartParticle is the *Server-bound wrapper around [EncodeParticle].
// Writes the svc_particle event into the per-tick datagram so every
// active client picks it up on the next SV_SendClientMessages flush.
// tyrquake: SV_StartParticle in NQ/sv_main.c.
//
// Silent-drop semantics on a near-full datagram are preserved by the
// underlying encoder: the upstream's "if (sv.datagram.cursize >
// MAX_DATAGRAM - 16) return" lives in [EncodeParticle], and the
// [ErrDatagramFull] sentinel is propagated verbatim. Particle events
// are best-effort: callers typically swallow the error.
//
// Returns nil iff the encoder succeeded; propagates [ErrDatagramFull]
// or any propagated msg.Write* error.
func (s *Server) StartParticle(org, dir [3]float32, color, count int) error {
	if s == nil {
		return ErrNilServer
	}
	if s.Datagram == nil {
		return ErrNilDatagram
	}
	return EncodeParticle(s.Datagram, org, dir, color, count)
}

// StartSound is the *Server-bound wrapper around [EncodeSound]. Looks
// up the sound's precache index, computes the entity-center origin
// from (entityOrigin + 0.5*(entityMins + entityMaxs)), and writes
// svc_sound into the per-tick datagram. tyrquake: SV_StartSound in
// NQ/sv_main.c (the precache walk + the
// `coord += 0.5 * (entity->v.mins[i] + entity->v.maxs[i])` loop +
// the wire emit, all in one upstream function).
//
// Parameters:
//
//	entityIdx      the entity slot making the sound
//	channel        0..7 (8+ requires FITZ protocol)
//	entityOrigin   the entity's world position
//	entityMins,
//	entityMaxs     the entity's bbox, used for the center offset
//	sample         precached sound name; ErrNotPrecached on miss
//	volume         0..255
//	attenuation    0..4
//
// Returns the [SoundIndex] / [EncodeSound] errors verbatim;
// [ErrDatagramFull] when the datagram is near-full;
// [ErrNotPrecached] when sample isn't in [Server.SoundPrecache].
func (s *Server) StartSound(entityIdx, channel int, entityOrigin, entityMins, entityMaxs [3]float32, sample string, volume int, attenuation float32) error {
	if s == nil {
		return ErrNilServer
	}
	if s.Datagram == nil {
		return ErrNilDatagram
	}
	soundNum, err := SoundIndex(s.SoundPrecache, sample)
	if err != nil {
		return err
	}
	origin := [3]float32{
		entityOrigin[0] + 0.5*(entityMins[0]+entityMaxs[0]),
		entityOrigin[1] + 0.5*(entityMins[1]+entityMaxs[1]),
		entityOrigin[2] + 0.5*(entityMins[2]+entityMaxs[2]),
	}
	return EncodeSound(s.Datagram, entityIdx, channel, soundNum, origin, volume, attenuation, s.Protocol)
}

// FireLightning is the *Server-bound wrapper around [EncodeLightning].
// Writes one svc_temp_entity TE_LIGHTNING* / TE_BEAM event into the
// per-tick datagram so every active client picks it up on the next
// SV_SendClientMessages flush. tyrquake: the per-tick path inside
// W_FireLightning (id1's weapons.qc), which traces from the player's
// muzzle along v_forward for 600 units, calls LightningDamage along
// the resulting line, then emits svc_temp_entity TE_LIGHTNING2 with
// the (entity, start, end) triple.
//
// Parameters mirror [EncodeLightning]:
//
//	kind   one of protocol.TELightning1 / 2 / 3 / TEBeam
//	ent    the owning entity slot (the player firing the bolt, or
//	       the boss for TELightning1 / TELightning3)
//	start  traceline source (the entity's muzzle)
//	end    traceline endpoint (the impact point or max-range terminus)
//
// Returns the [EncodeLightning] errors verbatim: [ErrNilServer] /
// [ErrNilDatagram] / [ErrLightningKind] / [ErrDatagramFull].
func (s *Server) FireLightning(kind, ent int, start, end [3]float32) error {
	if s == nil {
		return ErrNilServer
	}
	if s.Datagram == nil {
		return ErrNilDatagram
	}
	return EncodeLightning(s.Datagram, kind, ent, start, end)
}

// BroadcastNop appends a single svc_nop byte to the per-client
// reliable Message buffer of every Active + Spawned client. Used to
// keep slow connections alive when nothing else is going out.
// tyrquake: the broadcast variant of SV_SendNop's per-client wire
// write (the upstream code lives inline in SV_SendClientMessages).
//
// Returns the first per-client write error encountered; subsequent
// clients still get their copy (partial-broadcast > no-broadcast,
// matching [BroadcastPrint]'s iteration semantics).
//
// Safe to call with a nil Static (silent no-op).
func (s *Server) BroadcastNop(static *Static) error {
	if static == nil {
		return nil
	}
	var firstErr error
	for _, c := range static.Clients {
		if c == nil || !c.Active || !c.Spawned {
			continue
		}
		if err := msg.WriteByte(c.Message, protocol.SvcNop); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	return firstErr
}

// ClearDatagram resets the per-tick unreliable buffer. Called once
// per SV_SendClientMessages tick before the per-frame writes begin.
// tyrquake: SV_ClearDatagram (a one-liner in NQ/sv_main.c).
//
// Safe to call on a nil receiver or a Server with no Datagram
// (silent no-op).
func (s *Server) ClearDatagram() {
	if s == nil || s.Datagram == nil {
		return
	}
	s.Datagram.Clear()
}
