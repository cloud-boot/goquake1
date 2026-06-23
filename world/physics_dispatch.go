// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// PhysicsEdictResolver is the dispatcher's hook into the caller's
// edict-pool: given an index, return the corresponding *Edict (or
// nil if the slot is free/empty). The Go port decouples the
// dispatcher from any specific edict-storage scheme -- the C
// upstream walks sv.edicts[] directly with NEXT_EDICT pointer
// arithmetic; the resolver pattern lets the dispatcher operate
// equally well over a slice, an arena, or any synthetic-test pool.
type PhysicsEdictResolver func(index int) *progs.Edict

// PhysicsCmdResolver provides a UserCmd for a given edict. Only
// MOVETYPE_WALK (the client-input integrator) actually consumes the
// cmd in the C upstream; every other movetype gets a zero UserCmd.
// PhysicsWalk is NOT in this commit (no handler exists yet), so the
// dispatcher does not currently dereference the returned cmd -- the
// hook is wired through for shape parity with the future handler.
type PhysicsCmdResolver func(index int) server.UserCmd

// PhysicsKeyResolver maps an edict index to its world.Key. The
// canonical implementation is `func(i int) Key { return Key(i) }`
// (slot index == area-tree key), kept indirect so callers that map
// their edict pool through a non-identity key scheme stay supported.
type PhysicsKeyResolver func(index int) Key

// RunPhysics runs one full per-tic physics pass over every edict in
// the index range [0, numEdicts). For each non-nil edict it reads
// the entvars `movetype` + `solid` fields and dispatches to the
// matching per-MOVETYPE handler. tyrquake: SV_Physics in
// common/sv_phys.c.
//
// MOVETYPE -> handler routing (the dispatch table this commit ships):
//
//	MoveTypeNone        -> PhysicsNone
//	MoveTypeNoClip      -> PhysicsNoClip
//	MoveTypeFly         -> PhysicsFly
//	MoveTypeToss        -> PhysicsToss
//	MoveTypeBounce      -> PhysicsBounce
//	MoveTypeFlyMissile  -> PhysicsToss (same kinematics, different aim)
//	MoveTypeStep        -> silent skip (PhysicsStep not yet implemented)
//	MoveTypeWalk        -> silent skip (PhysicsWalk not yet implemented)
//	MoveTypePush        -> PhysicsPusher (integrates velocity * dt +
//	                                      RunThink; rider-clipping
//	                                      deferred -- see docstring)
//	MoveTypeAngleClip / MoveTypeAngleNoClip / unknown -> silent skip
//
// SPEC DEVIATION from the C upstream: the C version routes
// MOVETYPE_FLY (non-client) through SV_Physics_Toss inside the NQ
// SV_Physics arm; this Go port follows the task brief's table and
// routes MOVETYPE_FLY to [PhysicsFly] (the FLY-specific kinematics:
// RunThink + FlyMove, no gravity). Client-slot MOVETYPE_FLY is the
// canonical FLY semantics in the C upstream's SV_Physics_Client; the
// task brief's table is the union of "the standard MOVETYPE_FLY
// semantics" + "no client-vs-non-client special-casing" and is what
// this commit implements.
//
// Per-edict skip rules (the C upstream's `if (ent->free) continue;`
// + the "free entity" classification it routes through SV_Physics's
// implicit MOVETYPE_NONE arm):
//
//   - edictAt returns nil           -> skip (treated as "free slot")
//   - movetype is Push / AngleClip /
//     AngleNoClip / Walk / Step /
//     unknown                       -> skip (not in the dispatch
//     table this commit ships)
//   - movetype == None AND
//     solid    == SolidNot          -> skip (a non-moving non-solid
//     entity is the upstream's
//     "free" idle case, no per-tic
//     work needed)
//
// The first handler dispatch that returns a non-nil error
// short-circuits the loop: subsequent edicts are NOT processed,
// matching the C upstream's Sys_Error-on-failure shape -- the Go
// port surfaces it as an error instead of aborting the process.
//
// Parameters:
//
//	numEdicts    upper bound of the iteration range; the dispatcher
//	             walks indices [0, numEdicts).
//	params       server-side physics scalars passed through to
//	             [PhysicsToss] / [PhysicsBounce]; typically
//	             [server.DefaultPhysParams].
//	ctx          shared per-frame state (worldmodel + candidates +
//	             now + dt + thinkCaller); see [PhysicsContext].
//	edictAt      maps an index to its *Edict, or nil for a free slot.
//	cmdAt        maps an index to the matching UserCmd; carried for
//	             the future PhysicsWalk handler (no current consumer).
//	keyAt        maps an index to its world.Key; threaded into every
//	             per-handler call as the moving-entity identity.
//	progsHandle  Progs used to bind a fresh [progs.EntVars] to each
//	             dispatched edict so the per-handler accessors resolve
//	             field names through the same field-def table.
//
// Returns the first error from any handler dispatch (or any
// EntVars-binding error from progs.NewEntVars); nil on a clean pass
// over every edict.
//
// Pre-conditions every test exercises (no production guard needed):
//
//   - progsHandle != nil; the only progs.NewEntVars error is
//     ErrNilArg, which fires on nil progs OR nil edict. The
//     dispatcher rejects nil edicts BEFORE the bind, and a nil
//     progsHandle is a caller bug the upstream would have crashed
//     on (sv.progs == NULL inside the per-tic loop is unreachable
//     once SV_SpawnServer succeeds).
//
// nextTickTodo (NOT in this commit, deliberately): the C upstream's
// `pr_global_struct->force_retouch` re-link pass at the top of the
// loop + the `sv.time += host_frametime` advance at the bottom are
// the dispatcher caller's job (the caller owns the server.Server
// state); RunPhysics is the per-edict iteration core only.
func RunPhysics(
	numEdicts int,
	params server.PhysParams,
	ctx PhysicsContext,
	edictAt PhysicsEdictResolver,
	cmdAt PhysicsCmdResolver,
	keyAt PhysicsKeyResolver,
	progsHandle *progs.Progs,
) error {
	// shape is locked in now so a follow-up that wires Walk in
	// does not have to change the dispatcher signature.

	for i := 0; i < numEdicts; i++ {
		ent := edictAt(i)
		if ent == nil {
			continue
		}

		ev, err := progs.NewEntVars(progsHandle, ent)
		if err != nil {
			return err
		}

		// movetype is an EvFloat in QC (the MOVETYPE_* enum is
		// stored as a float by the QCC compiler -- see
		// pr_comp.h). Surface a missing or wrong-typed field
		// verbatim: a corrupt-progs signal that callers want to
		// see, not silently elide.
		mtF, err := ev.ReadFloat("movetype")
		if err != nil {
			return err
		}
		mt := server.MoveType(int32(mtF))

		// solid is also an EvFloat (same QC encoding rule). Same
		// surface-the-error policy.
		solidF, err := ev.ReadFloat("solid")
		if err != nil {
			return err
		}
		solid := server.Solid(int32(solidF))

		// Free-entity skip: a non-moving non-solid edict has no
		// per-tic work. The C upstream relies on `ent->free` for
		// this; the Go port has no free flag on the EntVars surface
		// yet (Edict.Free is the arena bookkeeping, not visible
		// inside SV_Physics), so the (movetype==None && solid==Not)
		// pair stands in as the equivalent classification.
		if mt == server.MoveTypeNone && solid == server.SolidNot {
			continue
		}

		key := keyAt(i)

		switch mt {
		case server.MoveTypeNone:
			if _, err := PhysicsNone(ent, ev, key, ctx); err != nil {
				return err
			}
		case server.MoveTypeNoClip:
			if _, err := PhysicsNoClip(ent, ev, key, ctx); err != nil {
				return err
			}
		case server.MoveTypeFly:
			if _, err := PhysicsFly(ent, ev, key, ctx); err != nil {
				return err
			}
		case server.MoveTypeToss, server.MoveTypeFlyMissile:
			if _, err := PhysicsToss(ent, ev, key, params, ctx); err != nil {
				return err
			}
		case server.MoveTypeBounce:
			if _, err := PhysicsBounce(ent, ev, key, params, ctx); err != nil {
				return err
			}
		case server.MoveTypeStep:
			if _, err := PhysicsStep(ent, ev, key, params, ctx); err != nil {
				return err
			}
		case server.MoveTypeWalk:
			if _, err := PhysicsWalk(ent, ev, key, cmdAt(i), params, ctx); err != nil {
				return err
			}
		case server.MoveTypePush:
			if _, err := PhysicsPusher(ent, ev, key, ctx); err != nil {
				return err
			}
		default:
			// Push / AngleClip / AngleNoClip / any unknown value
			// -- silent skip. The C upstream Sys_Error's on a
			// genuinely-unknown movetype; the Go port treats
			// non-dispatched movetypes the same way until the
			// missing handlers land.
		}
	}

	return nil
}
