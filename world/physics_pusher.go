// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// PhysicsPusher implements the MOVETYPE_PUSH handler -- doors, lifts,
// plats, trains. tyrquake: SV_Physics_Pusher in common/sv_phys.c.
//
// MOVETYPE_PUSH movers are the rare entities whose origin is driven
// by their `velocity` field (not by per-tic gravity / FlyMove): the
// QC code sets ent.velocity + ent.nextthink via SUB_CalcMove, the
// engine integrates origin += velocity*dt every tic, and when the
// scheduled nextthink fires the QC clears velocity to stop the move.
// Doors auto-open via SUB_CalcMove from door_go_up + auto-close via
// SUB_CalcMove from door_go_down; trigger volumes never invoke this
// handler (triggers are SOLID_TRIGGER, not MOVETYPE_PUSH).
//
// Algorithm (simplified-but-correct shape for bring-up):
//
//  1. Read velocity + avelocity. If BOTH are zero, the pusher is
//     parked -- jump straight to RunThink (matches the C upstream's
//     "if (!ent->v.velocity && !ent->v.avelocity) goto runthink"
//     fast-path).
//  2. Otherwise: integrate origin += velocity*dt and angles += avelocity*dt
//     in-place. Write the new origin + angles back.
//  3. RunThink. A SUB_CalcMove'd door's nextthink fires when the move
//     completes, and the QC clears velocity there -- so the next tic's
//     PhysicsPusher hits the fast-path again.
//
// SIMPLIFICATIONS vs the C upstream's SV_Physics_Pusher (deferred,
// not load-bearing for door progression in an empty room):
//
//   - Rider collection + PushMove dispatch. The C upstream walks every
//     edict in the world to build the "standing on me OR overlapping
//     my new absbounds" rider list, then calls SV_Push to integrate
//     the pusher's move WHILE clipping every rider. The Go port's
//     [PushMove] primitive is wired (see world/pushmove.go) but the
//     dispatcher has no current way to enumerate riders from inside
//     the per-edict handler -- the rider walk is the dispatcher's job
//     and ships in a follow-up commit. For doors in an empty room
//     (= the bring-up scenario this commit unblocks) the direct
//     integration is observably identical to the rider-clipped path.
//
//   - The "blocked" QC callback dispatch. PushMove surfaces the blocked
//     rider's index; firing the QC ent.blocked function is the
//     dispatcher's job in the same follow-up.
//
//   - SV_LinkEdict after the move. The dispatcher owns the World
//     handle + the bbox-from-entvars synthesis, so the per-handler
//     stays free of the area-tree relink (matches the pattern the
//     other PhysicsX handlers use).
//
// Returns:
//
//	true,  nil  -- happy path (integrated + think dispatched, or
//	               fast-path parked + think dispatched).
//	false, err  -- EntVars read error or RunThink dispatch error.
func PhysicsPusher(ent *progs.Edict, ev *progs.EntVars, key Key, ctx PhysicsContext) (bool, error) {
	_ = key

	velocity, err := ev.ReadVec3("velocity")
	if err != nil {
		return false, err
	}
	avelocity, err := ev.ReadVec3("avelocity")
	if err != nil {
		// avelocity is rarely declared on early-stub progs; treat its
		// absence as a zero rotation rather than aborting.
		avelocity = [3]float32{}
	}

	// Fast-path: parked mover (velocity == 0 && avelocity == 0). The
	// integration is a no-op; only the think dispatch matters.
	moving := velocity[0] != 0 || velocity[1] != 0 || velocity[2] != 0 ||
		avelocity[0] != 0 || avelocity[1] != 0 || avelocity[2] != 0

	if moving {
		origin, err := ev.ReadVec3("origin")
		if err != nil {
			return false, err
		}
		angles, err := ev.ReadVec3("angles")
		if err != nil {
			// Same forgiveness as avelocity: angles may be absent on
			// stripped test progs. Default to identity.
			angles = [3]float32{}
		}

		for i := 0; i < 3; i++ {
			origin[i] += ctx.Dt * velocity[i]
			angles[i] += ctx.Dt * avelocity[i]
		}

		// WriteVec3 for origin / angles after their reads succeeded
		// cannot fail (same offset + type + range). The angles write
		// is conditional on the prior read having succeeded -- the
		// `angles == zero` fallback above set it from a fresh zero, so
		// the write target may not exist; ignore that error too.
		_ = ev.WriteVec3("origin", origin)
		_ = ev.WriteVec3("angles", angles)
	}

	// Think dispatch (post-move): the SUB_CalcMove-scheduled nextthink
	// fires here, the QC clears velocity, the next tic hits the
	// fast-path.
	if _, err := server.RunThink(ent, ev, ctx.Now, ctx.Dt, ctx.ThinkCaller); err != nil {
		return false, err
	}
	return true, nil
}
