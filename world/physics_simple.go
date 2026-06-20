// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// PhysicsContext bundles the per-frame state every per-MOVETYPE
// handler reads + the dispatch hooks. The caller (the future
// SV_Physics dispatcher) builds it once per frame and passes the
// same value to every per-edict Physics* call.
//
//   - Worldmodel  is the world brushmodel handed to [FlyMove] /
//     [TraceMove]; nil for the trivial handlers that never trace
//     (PhysicsNone, PhysicsNoClip).
//   - Candidates  is the pre-filtered solid-edicts slice [FlyMove]
//     and friends clip against. The caller is responsible for
//     excluding the moving entity (see [PushEntity] /
//     [PushEntityIn.EntityKey]); the per-MOVETYPE handlers do not
//     re-filter.
//   - Now         is sv.time in the C upstream -- the current server
//     simulation time, used by [server.RunThink] to decide whether a
//     scheduled nextthink falls inside this tick.
//   - Dt          is sv.frametime / host_frametime -- the tick
//     interval in seconds, used to advance origin/angles in
//     [PhysicsNoClip] and as [FlyMove]'s Time budget.
//   - ThinkCaller is the QC dispatch hook passed through to
//     [server.RunThink]; see its docstring for the contract.
type PhysicsContext struct {
	Worldmodel  *model.BrushModel
	Candidates  []Target
	Now         float32
	Dt          float32
	ThinkCaller server.ThinkCaller
}

// PhysicsNone implements the MOVETYPE_NONE handler: the entity does
// not move at all; the only per-tick work is dispatching any
// scheduled think. tyrquake: SV_Physics_None in common/sv_phys.c.
//
// key is unused here (PhysicsNone never traces or links) but is
// carried across the per-MOVETYPE handler surface so the dispatcher
// has a uniform signature.
//
// Returns:
//
//	true,  nil  -- think fired, or no think was scheduled this tick.
//	false, err  -- think dispatch (or its EntVars prereads) failed.
//
// The C upstream returns void; the Go port surfaces RunThink's
// (alive, err) tuple directly so the dispatcher can short-circuit on
// the entity-died / dispatch-failed cases without a second lookup.
func PhysicsNone(ent *progs.Edict, ev *progs.EntVars, key Key, ctx PhysicsContext) (bool, error) {
	_ = key
	return server.RunThink(ent, ev, ctx.Now, ctx.Dt, ctx.ThinkCaller)
}

// PhysicsNoClip implements the MOVETYPE_NOCLIP handler: dispatch any
// scheduled think, then advance origin and angles in straight lines
// at the entity's current velocity + avelocity. No collision is
// applied -- that is the point of NOCLIP. tyrquake: SV_Physics_Noclip
// in common/sv_phys.c.
//
// Algorithm (matches the C upstream verbatim):
//
//  1. RunThink. If it returns an error, surface it without touching
//     position.
//  2. Read origin, velocity, angles, avelocity from EntVars.
//  3. origin[i] += dt * velocity[i] and angles[i] += dt * avelocity[i]
//     for each axis.
//  4. Write origin + angles back.
//
// The C upstream calls SV_LinkEdict after the move so the area tree
// reflects the new bounds. The Go port deliberately omits that call
// here: the LinkEdict half of the port lives in [World.LinkBounds]
// and is the dispatcher's job (it owns the World handle and the
// bbox-from-entvars synthesis), not the per-MOVETYPE handler's.
//
// key is unused (NoClip never traces) and carried for signature
// uniformity with the other handlers.
//
// EntVars read/write errors are surfaced verbatim -- the C upstream
// silently corrupts on missing-field paths (the field offsets are
// hard-coded), but the Go port routes through the typed accessor and
// reports a real error.
func PhysicsNoClip(ent *progs.Edict, ev *progs.EntVars, key Key, ctx PhysicsContext) (bool, error) {
	_ = key
	// RunThink can return (false, nil) ONLY via the nil-arg sentinels
	// (ErrNilEntity / ErrNilEntVars / ErrNoThinkCaller), all of which
	// surface as (false, err). The "(false, nil) entity-died" path of
	// the C upstream depended on ent->free, which has no equivalent
	// in the current Edict surface -- so the !alive-without-err branch
	// is structurally unreachable here and dropped, bsptrace-style.
	if _, err := server.RunThink(ent, ev, ctx.Now, ctx.Dt, ctx.ThinkCaller); err != nil {
		return false, err
	}

	origin, err := ev.ReadVec3("origin")
	if err != nil {
		return false, err
	}
	velocity, err := ev.ReadVec3("velocity")
	if err != nil {
		return false, err
	}
	angles, err := ev.ReadVec3("angles")
	if err != nil {
		return false, err
	}
	avelocity, err := ev.ReadVec3("avelocity")
	if err != nil {
		return false, err
	}

	for i := 0; i < 3; i++ {
		origin[i] += ctx.Dt * velocity[i]
		angles[i] += ctx.Dt * avelocity[i]
	}

	// WriteVec3 for a field name that ReadVec3 just succeeded for
	// cannot fail: both routes resolve the SAME (offset, type) pair
	// through the SAME Progs.FindField + Edict.FieldSetVector range
	// check -- if the read found a valid 3-float slot at the offset,
	// the write hits the same slot. The error branches are structurally
	// unreachable and dropped per the bsptrace pattern of removing
	// C-inherited dead code.
	_ = ev.WriteVec3("origin", origin)
	_ = ev.WriteVec3("angles", angles)
	return true, nil
}

// PhysicsFly implements the MOVETYPE_FLY handler: dispatch any
// scheduled think (which may have rewritten velocity), then run a
// full [FlyMove] slide-along-walls integration over the tick
// interval. Writes the resulting origin + velocity back to the
// edict. tyrquake: the `case MOVETYPE_FLY:` arm of SV_Physics_Client
// in common/sv_phys.c (RunThink + SV_FlyMove(player, host_frametime,
// NULL)). The NQ-side non-client SV_RunEntity routes MOVETYPE_FLY to
// SV_Physics_Toss instead; the spec for this port is the Client
// handler, which is the canonical "Fly" semantics (think + slide
// without bounce / gravity / water transitions).
//
// SPEC DEVIATION: the task brief described the algorithm as
// "WaterMove + RunThink + FlyMove". The C upstream does NOT call
// SV_CheckWater (the WaterMove primitive) inside the MOVETYPE_FLY
// arm -- water classification is exclusive to MOVETYPE_WALK
// (SV_Physics_Client / case MOVETYPE_WALK) and to SV_Physics_Toss's
// SV_CheckWaterTransition step. Implementing the brief's WaterMove
// step here would diverge from upstream; the port follows the C
// source verbatim (RunThink then FlyMove).
//
// Algorithm:
//
//  1. RunThink. If it errors, bail without integrating.
//  2. Read origin, velocity, mins, maxs from EntVars (post-think,
//     so a think that rewrote velocity is honoured by the integrator).
//  3. Call [FlyMove] with the post-think state. The Time budget is
//     the full tick (ctx.Dt); the caller's Candidates slice is
//     handed through verbatim (already filtered to exclude this
//     entity).
//  4. Write the integrated origin + velocity back. The Blocked /
//     StepTrace fields on the FlyMove output are dropped here: FLY
//     does not need step-up logic (it's the WALK/STEP path that
//     consumes StepTrace) and the dispatcher doesn't surface
//     Blocked to the QC layer for MOVETYPE_FLY in upstream.
//
// key is forwarded to [FlyMove] as its EntityKey so the integrator's
// internal candidate filtering still has the moving-entity identity
// in hand (the spec calls this out as the substitute for the C
// upstream's NUM_FOR_EDICT(ent) pointer-subtraction trick, which the
// Go arena-of-Edict layout cannot replicate cheaply).
//
// EntVars read/write errors are surfaced verbatim; FlyMove errors
// (only happens on a corrupt brushmodel) are surfaced verbatim too.
func PhysicsFly(ent *progs.Edict, ev *progs.EntVars, key Key, ctx PhysicsContext) (bool, error) {
	// As in [PhysicsNoClip], the !alive-without-err branch of the C
	// upstream's "entity died inside think" path is structurally
	// unreachable here (ent->free has no Go equivalent yet); only the
	// (false, err) cascade is real and is forwarded.
	if _, err := server.RunThink(ent, ev, ctx.Now, ctx.Dt, ctx.ThinkCaller); err != nil {
		return false, err
	}

	velocity, err := ev.ReadVec3("velocity")
	if err != nil {
		return false, err
	}
	origin, err := ev.ReadVec3("origin")
	if err != nil {
		return false, err
	}
	mins, err := ev.ReadVec3("mins")
	if err != nil {
		return false, err
	}
	maxs, err := ev.ReadVec3("maxs")
	if err != nil {
		return false, err
	}

	out, err := FlyMove(FlyMoveIn{
		Origin:    origin,
		Mins:      mins,
		Maxs:      maxs,
		Velocity:  velocity,
		Time:      ctx.Dt,
		EntityKey: key,
	}, ctx.Worldmodel, ctx.Candidates)
	if err != nil {
		return false, err
	}

	// WriteVec3 for origin / velocity cannot fail after their reads
	// succeeded (same offset + type, same range check). Error branches
	// dropped per the bsptrace pattern.
	_ = ev.WriteVec3("origin", out.NewOrigin)
	_ = ev.WriteVec3("velocity", out.NewVelocity)
	return true, nil
}
