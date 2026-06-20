// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// PushMoveRider is the snapshot of one entity that's potentially
// affected by a MOVETYPE_PUSH pusher's move. The server glue layer
// extracts it from the edict's entvars BEFORE calling [PushMove],
// then writes the post-move data back from [PushMoveOut] after.
//
// Origin / Mins / Maxs follow the rest of the world package's
// convention: Origin is world-space, Mins/Maxs are LOCAL bounds
// (relative to Origin). The "absmin/absmax" the C upstream uses
// inline is reconstructed as Origin + Mins / Origin + Maxs.
//
// GroundKey is the rider's groundentity (the surface it's standing
// on). When GroundKey equals the pusher's own [Key], the rider is
// "riding" the pusher and is moved unconditionally (the C upstream's
// FL_ONGROUND + groundentity == pusher predicate). For non-riders,
// GroundKey is irrelevant and the bounds-overlap test decides.
type PushMoveRider struct {
	Key       Key
	Origin    [3]float32      // PRE-move origin
	Mins      [3]float32      // LOCAL bounds (Origin + Mins = absmin)
	Maxs      [3]float32      // LOCAL bounds
	Solid     server.Solid    // SolidNot skips entirely
	MoveType  server.MoveType // None / NoClip / Push skip entirely
	Flags     int32           // entvars.flags (FL_* bits); carried for parity with the C upstream
	GroundKey Key             // the rider's ground-entity Key; == pusherKey means riding
}

// PushMoveOut is the result of one [PushMove] call. NewRiderOrigins
// parallels the input riders slice (same length, same order): index
// i is the post-move origin of riders[i]. Skipped riders (SolidNot,
// MoveType None/NoClip/Push, neither riding nor overlapping) get
// their pre-move Origin copied through, so the caller can blindly
// write NewRiderOrigins[i] back into riders[i]'s edict without an
// "is this slot meaningful" check.
//
// When Blocked is true, BlockedRider is the index of the rider that
// the move couldn't accommodate. The caller is then responsible for
// rolling back the pusher (write pusherOrigin back into its edict,
// don't advance ltime) AND the moved riders (write each rider's
// pre-move Origin back into its edict). Following the C upstream's
// SV_Push, the QC "blocked" callback for the pusher fires AFTER the
// roll-back -- that's the caller's job too; PushMove just reports
// which entity blocked.
//
// BlockedRider is -1 when Blocked is false.
type PushMoveOut struct {
	NewPusherOrigin [3]float32
	NewRiderOrigins [][3]float32
	Blocked         bool
	BlockedRider    int
}

// PushMove tries to move a MOVETYPE_PUSH pusher (doors, lifts, plats,
// trains) by pushMove and re-positions every entity standing on it
// OR overlapping its new bounds. tyrquake: SV_Push in common/sv_phys.c
// (the geometric work -- the QC "blocked" callback + per-touch impact
// dispatch + the upstream's SOLID_TRIGGER corpse-bounds-zero corner
// case stay in the caller's lap).
//
// Algorithm:
//
//  1. Compute the post-move pusher absmin/absmax (= pusherOrigin +
//     pushMove + pusherMins / pusherMaxs).
//
//  2. Move the pusher to its final position (NewPusherOrigin =
//     pusherOrigin + pushMove). The pusher is a SOLID_BSP brushmodel;
//     its own collision is delegated to the per-rider PushEntity
//     calls (no self-trace).
//
//  3. For each rider i in riders, in order:
//
//     - skip if Solid == [server.SolidNot] (corpse / non-interacting),
//     - skip if MoveType is [server.MoveTypeNone] / [server.MoveTypeNoClip]
//     / [server.MoveTypePush] (doesn't ride pushers),
//     - skip if the rider is NEITHER standing on the pusher (GroundKey
//     == pusherKey AND FlagOnGround set) NOR overlapping the
//     pusher's NEW absbounds (strict AABB overlap, mirroring the C
//     upstream's >= / <= disjoint-axis test),
//     - else call [PushEntity] with push = pushMove and the OTHER
//     riders as candidates (so each rider clips against its
//     neighbours + the world).
//
//  4. Record NewRiderOrigins[i] = the rider's resulting origin. When
//     PushEntity reports a non-clean trace (Fraction < 1 OR StartSolid
//     OR AllSolid), the rider is blocked: set Blocked=true,
//     BlockedRider=i, return EARLY (later riders keep their pre-move
//     Origin in NewRiderOrigins; the caller rolls back 0..i).
//
// The candidates passed to per-rider PushEntity calls are: every
// OTHER rider in the riders list (built as [Target]s from each
// rider's Origin + Mins + Maxs + Solid; SolidNot riders are dropped)
// + the world brushmodel (PushEntity handles that internally).
//
// SIMPLIFICATIONS vs the C upstream's SV_Push:
//
//   - SOLID_NOT riders are skipped at the top of the loop. The C
//     loops over them but [PushEntity] would short-circuit anyway.
//   - The SOLID_TRIGGER "corpse: zero its bounds and let it stay"
//     branch is dropped. It mutates rider bounds in-place, which the
//     PushMoveOut shape doesn't expose; a future server-glue caller
//     can layer that mutation on top.
//   - The point-entity (mins[0] == maxs[0]) "never blocks" branch is
//     dropped for the same reason -- a zero-volume rider here will
//     trivially have a clean PushEntity trace and not block, which
//     produces the same observable outcome.
//   - The "blocked" QC callback is NOT invoked here. The caller has
//     the pusher's Key + the blocking rider's index; it fires the
//     callback after rolling back.
//   - No SV_LinkEdict / re-link of moved entities. The caller is
//     expected to relink the pusher + every moved rider once it
//     writes the new origins back.
func PushMove(pusherKey Key, pusherOrigin, pusherMins, pusherMaxs, pushMove [3]float32, riders []PushMoveRider, worldmodel *model.BrushModel) (PushMoveOut, error) {
	newPusherOrigin := [3]float32{
		pusherOrigin[0] + pushMove[0],
		pusherOrigin[1] + pushMove[1],
		pusherOrigin[2] + pushMove[2],
	}

	// Post-move pusher absbounds. The C upstream computes
	//   mins[i] = pusher->v.absmin[i] + move[i];
	// using the entvars absmin/absmax (already world-space). The Go
	// port keeps Mins/Maxs as LOCAL bounds (consistent with the rest
	// of the world package), so absmin = newPusherOrigin + pusherMins.
	pusherAbsMin := [3]float32{
		newPusherOrigin[0] + pusherMins[0],
		newPusherOrigin[1] + pusherMins[1],
		newPusherOrigin[2] + pusherMins[2],
	}
	pusherAbsMax := [3]float32{
		newPusherOrigin[0] + pusherMaxs[0],
		newPusherOrigin[1] + pusherMaxs[1],
		newPusherOrigin[2] + pusherMaxs[2],
	}

	// Preallocate the per-rider result slice with each rider's
	// PRE-move origin, so a "skipped" rider's slot is already correct
	// (= unchanged) and the early-return-on-blocked path leaves
	// later riders' slots at their pre-move origins by default.
	newRiderOrigins := make([][3]float32, len(riders))
	for i := range riders {
		newRiderOrigins[i] = riders[i].Origin
	}

	out := PushMoveOut{
		NewPusherOrigin: newPusherOrigin,
		NewRiderOrigins: newRiderOrigins,
		Blocked:         false,
		BlockedRider:    -1,
	}

	for i := range riders {
		rider := &riders[i]

		// Top-of-loop filters: SolidNot is the world-package's "never
		// participates" rule; the three MoveType skips mirror the C
		// upstream's MOVETYPE_PUSH / NONE / NOCLIP guard inside SV_Push.
		if rider.Solid == server.SolidNot {
			continue
		}
		if rider.MoveType == server.MoveTypeNone ||
			rider.MoveType == server.MoveTypeNoClip ||
			rider.MoveType == server.MoveTypePush {
			continue
		}

		// "Standing on the pusher" predicate: the C upstream checks
		// (flags & FL_ONGROUND) && groundentity == pusher. A rider
		// satisfying this is moved UNCONDITIONALLY (no bounds test).
		riding := rider.GroundKey == pusherKey &&
			(rider.Flags&int32(server.FlagOnGround)) != 0

		if !riding {
			// Bounds-overlap test against the pusher's NEW absbounds.
			// Inlined here (not [boundsOverlap]) to preserve the C
			// upstream's STRICT >= / <= disjoint-axis predicate:
			// touching-edges count as non-overlap, matching SV_Push's
			//   if (check->v.absmin[0] >= maxs[0] || ... )
			//       continue;
			riderAbsMin := [3]float32{
				rider.Origin[0] + rider.Mins[0],
				rider.Origin[1] + rider.Mins[1],
				rider.Origin[2] + rider.Mins[2],
			}
			riderAbsMax := [3]float32{
				rider.Origin[0] + rider.Maxs[0],
				rider.Origin[1] + rider.Maxs[1],
				rider.Origin[2] + rider.Maxs[2],
			}
			if riderAbsMin[0] >= pusherAbsMax[0] ||
				riderAbsMin[1] >= pusherAbsMax[1] ||
				riderAbsMin[2] >= pusherAbsMax[2] ||
				riderAbsMax[0] <= pusherAbsMin[0] ||
				riderAbsMax[1] <= pusherAbsMin[1] ||
				riderAbsMax[2] <= pusherAbsMin[2] {
				continue
			}
		}

		// Build the candidate list: every OTHER rider, as a Target.
		// SolidNot riders are dropped (they don't clip anything --
		// PushEntity would short-circuit on them anyway, but they
		// never need to appear as a clip target either).
		candidates := buildRiderCandidates(riders, i)

		pin := PushEntityIn{
			Origin:    rider.Origin,
			Mins:      rider.Mins,
			Maxs:      rider.Maxs,
			Push:      pushMove,
			MoveType:  rider.MoveType,
			Solid:     rider.Solid,
			EntityKey: rider.Key,
		}
		pout, err := PushEntity(pin, worldmodel, candidates)
		if err != nil {
			return PushMoveOut{}, err
		}

		newRiderOrigins[i] = pout.NewOrigin

		// "Blocked" = the rider's swept push couldn't complete. A
		// clean PushEntity reports Fraction == 1 with no startsolid
		// / allsolid; anything else means the rider hit a wall or
		// another rider on the way and is now jammed.
		if pout.Trace.Fraction < 1.0 || pout.Trace.StartSolid || pout.Trace.AllSolid {
			out.NewRiderOrigins = newRiderOrigins
			out.Blocked = true
			out.BlockedRider = i
			return out, nil
		}
	}

	out.NewRiderOrigins = newRiderOrigins
	return out, nil
}

// buildRiderCandidates returns a Target slice with every rider EXCEPT
// the one at skipIdx, dropping SolidNot riders. The skipIdx rider is
// the one currently being pushed; it must not clip against itself.
// SolidNot riders are dropped because they never participate in
// collision (the C upstream's SOLID_NOT-can't-block invariant).
func buildRiderCandidates(riders []PushMoveRider, skipIdx int) []Target {
	if len(riders) <= 1 {
		return nil
	}
	out := make([]Target, 0, len(riders)-1)
	for j := range riders {
		if j == skipIdx {
			continue
		}
		r := &riders[j]
		if r.Solid == server.SolidNot {
			continue
		}
		out = append(out, Target{
			Origin: r.Origin,
			Mins:   r.Mins,
			Maxs:   r.Maxs,
			Solid:  r.Solid,
		})
	}
	return out
}
