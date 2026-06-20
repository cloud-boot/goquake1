// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// tossOnGroundNormalZ is the upstream `> 0.7` cosine threshold above
// which a contact plane is treated as a floor for the purposes of
// stopping a tossed projectile. Below this threshold the surface is
// "too steep" and the projectile keeps sliding (after the
// [server.ClipVelocity] reflection) without latching onto it as
// ground.
//
// tyrquake hard-codes the 0.7 literal in the
//
//	if (trace.plane.normal[2] > 0.7)
//
// guard inside SV_Physics_Toss. Identical to [flyMoveFloorNormalZ];
// the two are kept distinct because the C upstream applies the same
// numeric to two different decisions (sliding-floor vs stop-on-floor)
// and a future re-tune could split them.
const tossOnGroundNormalZ = 0.7

// tossStopVelocityZ is the upstream `velocity[2] < 60` threshold the
// MOVETYPE_BOUNCE arm uses to decide a bouncing projectile has
// settled enough to come to rest. Below 60 units/second of post-clip
// vertical velocity AND a near-horizontal floor normal, the entity
// latches to ground; above, it keeps bouncing. tyrquake: literal 60
// in the
//
//	if (ent->v.velocity[2] < 60 || ent->v.movetype != MOVETYPE_BOUNCE)
//
// guard inside SV_Physics_Toss. MOVETYPE_TOSS bypasses the threshold
// (the `!= MOVETYPE_BOUNCE` clause is always true for it) -- the C
// upstream merges both movetypes through one function with that
// inline check; the Go port splits Toss and Bounce into separate
// handlers (see [PhysicsBounce]) for cleaner per-handler dispatch.
const tossStopVelocityZ = 60

// tossDefaultGravity is the gravity scale fallback used when an
// edict's `gravity` QC field is either missing entirely or set to
// 0. tyrquake: the
//
//	val = GetEdictFieldValue(ent, "gravity");
//	if (!val || !val->_float)
//	    scale = 1.0;
//
// branch inside SV_AddGravity. The standard Q1 entvars_t does NOT
// carry a `gravity` field; mods add it. The Go port accepts both
// "field absent" and "field present but 0" as the upstream-equivalent
// "no per-entity scaling -> use 1.0" path; the absent-field branch
// is the one this constant covers (the zero branch is handled inside
// [server.ApplyGravity]).
const tossDefaultGravity = 1.0

// PhysicsToss implements the MOVETYPE_TOSS handler -- the gravity-
// driven projectile path used by rockets, grenades, gibs, and any
// other thrown entity that doesn't bounce. tyrquake: SV_Physics_Toss
// in common/sv_phys.c (the non-MOVETYPE_BOUNCE arm).
//
// Algorithm (the C upstream verbatim, modulo the documented Go-port
// omissions):
//
//  1. RunThink. The think may rewrite velocity / nextthink / etc;
//     PhysicsToss reads them AFTER the dispatch. The C upstream's
//     `if (ent->free) return;` post-think bail is dropped here, like
//     it is in [PhysicsNoClip] and [PhysicsFly]: ent.free has no
//     equivalent on the current Edict surface, so the alive=false
//     branch is structurally unreachable on a properly-shaped Edict
//     and removed bsptrace-style. Surface only the err.
//  2. If [server.FlagOnGround] is set in entvars.flags, return without
//     moving -- a resting projectile stays put. The C upstream
//     short-circuits at the same point.
//  3. CheckVelocity: per-axis clamp velocity to +-params.MaxVelocity
//     via [server.ClampVelocity]. The C upstream's SV_CheckVelocity
//     also nukes NaN components, which the Go [server.ClampVelocity]
//     preserves bit-for-bit.
//  4. AddGravity via [server.ApplyGravity]. The C upstream skips this
//     step for MOVETYPE_FLY / MOVETYPE_FLYMISSILE; PhysicsToss handles
//     only MOVETYPE_TOSS / MOVETYPE_BOUNCE / MOVETYPE_GIB callers (the
//     dispatcher routes the no-gravity movetypes to [PhysicsFly]), so
//     gravity is always applied here.
//  5. Advance angles by `dt * avelocity` (the C upstream's
//     VectorMA(ent->v.angles, host_frametime, ent->v.avelocity,
//     ent->v.angles)).
//  6. [PushEntity] by `velocity * dt` against the caller-supplied
//     world brushmodel + candidates.
//  7. If the push completed clean (trace.Fraction == 1), write the
//     new origin + velocity back and return (true, nil) -- the
//     projectile flew unimpeded through the tick.
//  8. Otherwise reflect velocity off the impact plane via
//     [server.ClipVelocity] with overbounce = 1.0 (the
//     MOVETYPE_TOSS-arm constant; [PhysicsBounce] uses 1.5).
//  9. If the impact surface is mostly horizontal (normal[2] >
//     [tossOnGroundNormalZ]), latch to ground: set FL_ONGROUND, zero
//     velocity, zero avelocity. The C upstream also writes
//     `groundentity = EDICT_TO_PROG(ground)`; the Go port does NOT
//     touch the groundentity field (the dispatcher owns the
//     Key <-> edict-index translation and writes it after the call,
//     once it has the [PushEntityOut.HitEntity] index).
//  10. CheckWaterTransition is OMITTED in this commit -- water
//     bookkeeping (FL_INWATER toggling, water-touch sound effects,
//     entvars.waterlevel updates) lands in a future PR alongside the
//     [server.PointContents] surface. The omission is observable as
//     "a tossed projectile that splashes into water does not get its
//     FL_INWATER bit flipped"; the gravity/bounce/stop semantics this
//     handler owns are unaffected.
//
// Writes back to entvars on the happy path:
//
//   - "origin"     post-push position
//   - "velocity"   post-clip / post-gravity / post-clamp velocity
//   - "angles"     advanced by dt * avelocity
//   - "avelocity"  zeroed iff FL_ONGROUND was newly set (matches the
//     C upstream's VectorCopy(vec3_origin, ...))
//   - "flags"      FL_ONGROUND set iff a near-horizontal impact
//     occurred (Toss arm: any horizontal hit; Bounce arm:
//     only when post-clip velocity[2] < 60 too)
//
// Parameters:
//
//	ent      the moving edict (passed through to [server.RunThink]).
//	ev       EntVars bound to ent (reads/writes the listed fields).
//	key      this entity's [Key]; not consumed by PhysicsToss itself
//	         (the candidate list is pre-filtered by the dispatcher)
//	         but threaded through for signature parity with the other
//	         per-MOVETYPE handlers + as [PushEntityIn.EntityKey].
//	params   server-side physics scalars (sv_gravity, sv_maxvelocity,
//	         etc.); typically [server.DefaultPhysParams]. Taken as a
//	         separate parameter (rather than carried on
//	         [PhysicsContext]) so this commit stays self-contained --
//	         a future refactor may fold it into [PhysicsContext] when
//	         the rest of the per-MOVETYPE handlers want it too.
//	ctx      shared per-frame state (worldmodel + candidates + now +
//	         dt + thinkCaller); see [PhysicsContext].
//
// Returns:
//
//	true,  nil  -- happy path (the entity is alive after the tick),
//	               OR the FL_ONGROUND short-circuit fired (resting
//	               projectile, no work done).
//	false, err  -- EntVars read error, RunThink dispatch error
//	               (incl. the ErrNoThinkCaller / nil-arg sentinels),
//	               or PushEntity trace error.
func PhysicsToss(ent *progs.Edict, ev *progs.EntVars, key Key, params server.PhysParams, ctx PhysicsContext) (bool, error) {
	return physicsTossOrBounce(ent, ev, key, params, ctx, 1.0, false)
}

// PhysicsBounce implements the MOVETYPE_BOUNCE handler -- identical
// to [PhysicsToss] except:
//
//   - the [server.ClipVelocity] overbounce factor is 1.5 (vs 1.0 for
//     Toss), so the entity reflects with extra speed rather than
//     sliding to a stop, and
//   - the FL_ONGROUND latch on a horizontal hit fires only when the
//     post-clip velocity[2] is below [tossStopVelocityZ] (= 60 units/
//     sec), so a fast-moving grenade keeps bouncing across the floor
//     instead of latching to it.
//
// tyrquake: SV_Physics_Toss in common/sv_phys.c, with the inline
// `ent->v.movetype == MOVETYPE_BOUNCE` checks. The C upstream merges
// Toss + Bounce into the same function with two inline movetype
// switches (overbounce selection + stop-on-ground refinement); the
// Go port splits them for cleaner per-handler dispatch (the upcoming
// SV_Physics MOVETYPE table has one entry per handler, not one entry
// per movetype-pair).
//
// Used by: grenades (mid-air bouncing), gibs in the Bounce mode some
// mods select, dropped weapons that the QC marks BOUNCE.
//
// Signature and return contract are identical to [PhysicsToss]; see
// its docstring for the per-step algorithm + the writeback list +
// the CheckWaterTransition omission note.
func PhysicsBounce(ent *progs.Edict, ev *progs.EntVars, key Key, params server.PhysParams, ctx PhysicsContext) (bool, error) {
	return physicsTossOrBounce(ent, ev, key, params, ctx, 1.5, true)
}

// physicsTossOrBounce is the shared body of [PhysicsToss] and
// [PhysicsBounce]. The two arms differ in only two scalars:
//
//   - overbounce       the [server.ClipVelocity] factor (1.0 for Toss,
//     1.5 for Bounce).
//   - bounceStopCheck  when true (Bounce arm), the FL_ONGROUND latch
//     additionally requires post-clip velocity[2] <
//     [tossStopVelocityZ]; when false (Toss arm), any
//     near-horizontal impact stops the entity.
//
// Kept as one function with two scalar knobs (rather than two
// near-identical copies) because the Toss/Bounce split is purely a
// dispatch-table concern -- the geometry is bit-for-bit the same up
// to those two knobs in the C upstream's inline movetype checks.
func physicsTossOrBounce(ent *progs.Edict, ev *progs.EntVars, key Key, params server.PhysParams, ctx PhysicsContext, overbounce float32, bounceStopCheck bool) (bool, error) {
	// RunThink can return (false, nil) ONLY via the nil-arg sentinels
	// (ErrNilEntity / ErrNilEntVars / ErrNoThinkCaller), all of which
	// also surface a non-nil err. The "(false, nil) entity-died" path
	// of the C upstream depended on ent->free, which has no equivalent
	// in the current Edict surface -- so the !alive-without-err branch
	// is structurally unreachable here and dropped, bsptrace-style.
	// The alive bool is discarded; only err drives early-return.
	if _, err := server.RunThink(ent, ev, ctx.Now, ctx.Dt, ctx.ThinkCaller); err != nil {
		return false, err
	}

	// FL_ONGROUND short-circuit: a resting projectile stays put. The
	// C upstream's `if ((int)ent->v.flags & FL_ONGROUND) return;`
	// covers this. flags is an EvFloat in QC (despite being a bitfield);
	// read as a float and bit-test the rounded int value.
	flagsF, err := ev.ReadFloat("flags")
	if err != nil {
		return false, err
	}
	flags := server.EntityFlag(int32(flagsF))
	if (flags & server.FlagOnGround) != 0 {
		return true, nil
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
	angles, err := ev.ReadVec3("angles")
	if err != nil {
		return false, err
	}
	avelocity, err := ev.ReadVec3("avelocity")
	if err != nil {
		return false, err
	}
	_ = avelocity // consumed via VectorMA below; zeroed on FL_ONGROUND latch.

	// Per-axis ±MaxVelocity clamp + NaN-nuke. Matches SV_CheckVelocity.
	velocity = server.ClampVelocity(velocity, params.MaxVelocity)

	// Gravity. The per-entity gravityFactor is the QC `gravity` field
	// (a mod-added float, not a stock entvars field). When the field
	// is absent or 0, [server.ApplyGravity] internally treats the
	// factor as 1.0; PhysicsToss surfaces an absent-field as 1.0 too
	// so the same fallback applies. Other read errors (a field-type
	// mismatch) are surfaced -- a real corrupt-progs signal.
	gravityFactor, err := readGravityFactor(ev)
	if err != nil {
		return false, err
	}
	velocity = server.ApplyGravity(velocity, gravityFactor, params.Gravity, ctx.Dt)

	// Advance angles by dt * avelocity. tyrquake:
	//   VectorMA(ent->v.angles, host_frametime, ent->v.avelocity,
	//            ent->v.angles);
	for i := 0; i < 3; i++ {
		angles[i] += ctx.Dt * avelocity[i]
	}

	// Build the push vector and call into PushEntity. The candidate
	// list comes from the dispatcher's PhysicsContext -- it has
	// already filtered out the moving entity (via the EntityKey hook
	// in PushEntityIn) and any SOLID_NOT participants.
	pin := PushEntityIn{
		Origin:    origin,
		Mins:      mins,
		Maxs:      maxs,
		Push:      [3]float32{velocity[0] * ctx.Dt, velocity[1] * ctx.Dt, velocity[2] * ctx.Dt},
		MoveType:  server.MoveTypeToss,
		Solid:     server.SolidBBox,
		EntityKey: key,
	}
	pout, err := PushEntity(pin, ctx.Worldmodel, ctx.Candidates)
	if err != nil {
		return false, err
	}

	// Write the post-push origin + post-tick angles back regardless
	// of impact -- PushEntity's NewOrigin is the clamped post-trace
	// position (= origin + push for a clean trace, = origin + fraction
	// * push for a clipped trace).
	//
	// WriteVec3 only fails when the field is absent or the wrong type.
	// The matching ReadVec3 calls above proved both fields are present
	// as EvVector, so the WriteVec3 error branches are unreachable
	// here and the returns are dropped, bsptrace-style. Same reasoning
	// applies to every subsequent WriteVec3/WriteFloat in this
	// function: each one targets a field whose existence + type were
	// validated by the read pass at the top.
	_ = ev.WriteVec3("origin", pout.NewOrigin)
	_ = ev.WriteVec3("angles", angles)

	// Clean trace: no impact, just write the (gravity-modified)
	// velocity back and we're done.
	if pout.Trace.Fraction == 1.0 {
		_ = ev.WriteVec3("velocity", velocity)
		return true, nil
	}

	// Impact: reflect velocity off the trace plane. overbounce is the
	// Toss/Bounce dispatch knob (1.0 / 1.5).
	normal := [3]float32{
		pout.Trace.Plane.Normal[0],
		pout.Trace.Plane.Normal[1],
		pout.Trace.Plane.Normal[2],
	}
	velocity, _ = server.ClipVelocity(velocity, normal, overbounce)

	// Stop-on-floor check. Toss arm always latches on a near-horizontal
	// hit; Bounce arm additionally requires post-clip velocity[2] < 60.
	// The latch sets FL_ONGROUND and zeros velocity + avelocity.
	if normal[2] > tossOnGroundNormalZ && (!bounceStopCheck || velocity[2] < tossStopVelocityZ) {
		velocity = [3]float32{0, 0, 0}
		flags |= server.FlagOnGround
		_ = ev.WriteFloat("flags", float32(int32(flags)))
		_ = ev.WriteVec3("avelocity", [3]float32{0, 0, 0})
	}

	_ = ev.WriteVec3("velocity", velocity)
	return true, nil
}

// readGravityFactor returns the per-entity gravity scale from the QC
// `gravity` field, or [tossDefaultGravity] (= 1.0) when the field is
// absent. The C upstream's SV_AddGravity does the same lookup via
// GetEdictFieldValue + NULL-guard. [server.ApplyGravity] internally
// substitutes 1.0 for a 0 input, so the absent-and-zero paths share
// the same downstream behaviour. Type mismatches (the field exists
// but is not EvFloat -- a corrupt-progs signal) are surfaced verbatim
// so the caller can distinguish them from "mod doesn't add the field".
func readGravityFactor(ev *progs.EntVars) (float32, error) {
	g, err := ev.ReadFloat("gravity")
	if err != nil {
		if errors.Is(err, progs.ErrFieldNotFound) {
			return tossDefaultGravity, nil
		}
		return 0, err
	}
	return g, nil
}
