// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/world"
)

// PushEntityIn is the input snapshot the caller builds from an
// edict's entvars BEFORE the push: where the entity currently is,
// its size, its movetype, and the engine-side flags that select
// the trace mode.
//
// EntityKey is the moving entity's own world.Key -- it is NOT used
// by PushEntity itself (the caller passes a pre-filtered candidate
// list), but the snapshot carries it so the caller has one place
// to record "which edict am I pushing" alongside its bounds.
type PushEntityIn struct {
	Origin    [3]float32 // current entity position
	Mins      [3]float32 // bounding-box mins
	Maxs      [3]float32 // bounding-box maxs
	Push      [3]float32 // delta to apply this tick (velocity * dt OR a per-frame nudge)
	MoveType  MoveType   // selects MoveMode (NoClip -> no clipping, others -> normal)
	Solid     Solid      // SOLID_NOT skips entirely
	EntityKey world.Key  // this entity's world.Key (caller uses it to filter AreaQuery results)
}

// PushEntityOut is the result snapshot. The caller writes
// NewOrigin back into the edict's entvars and uses the trace +
// HitEntity for any follow-up touch dispatch.
type PushEntityOut struct {
	NewOrigin [3]float32     // final position (= Origin + Push * fraction)
	Trace     bsptrace.Trace // collision result; .Plane is valid iff impact
	HitEntity int            // -1 if no entity hit; else index into the candidates slice the caller passed
	HitWorld  bool           // true iff the world brushmodel clipped the move
}

// PushEntity slides an entity from in.Origin along in.Push,
// clipping against the world + the supplied candidate entities,
// and returns the final position + the trace result. tyrquake:
// SV_PushEntity in common/sv_phys.c.
//
// The function:
//
//  1. If in.Solid == SolidNot OR in.MoveType == MoveTypeNoClip,
//     no collision: returns NewOrigin = in.Origin + in.Push,
//     Trace = default (Fraction=1, AllSolid=false, EndPos already
//     set to the new origin), HitEntity = -1, HitWorld = false.
//  2. Picks the trace mode:
//     MoveType == MoveTypeFlyMissile -> MoveModeMissile
//     otherwise                       -> MoveModeNormal
//  3. Calls world.TraceMove with start=Origin, end=Origin+Push,
//     mins/maxs, the worldmodel + candidates.
//  4. Sets NewOrigin = trace.EndPos (= Origin + Push * trace.Fraction).
//  5. Returns the trace + the impact target (HitEntity from
//     result.EntityIdx, HitWorld from result.WorldClipped).
//
// SIMPLIFICATION vs C upstream: the C SV_PushEntity always calls
// SV_TraceMove (even for SOLID_NOT) with passedict set to skip the
// entity. The Go port short-circuits SolidNot AND MoveTypeNoClip
// up front -- the trace would be a no-op against a filtered-out
// list anyway -- so no TraceMove call happens on those paths. The
// returned Trace defaults to Fraction=1 / AllSolid=false / EndPos=
// (Origin+Push), matching what a clean upstream trace would yield.
//
// Callers (sv_phys's per-MOVETYPE handlers) follow up with:
//   - Linking the entity at NewOrigin (world.LinkBounds)
//   - Calling QC touch handlers iff trace.Fraction < 1
//   - Per-MOVETYPE state mutation (bounce velocity, etc.)
//
// worldmodel is the world brushmodel; candidates is the filtered
// list of entities to clip against (built from world.AreaQuery's
// keys, converted to world.Target by the caller, with the moving
// entity's own key excluded).
func PushEntity(in PushEntityIn, worldmodel *model.BrushModel, candidates []world.Target) (PushEntityOut, error) {
	end := [3]float32{
		in.Origin[0] + in.Push[0],
		in.Origin[1] + in.Push[1],
		in.Origin[2] + in.Push[2],
	}

	// Short-circuit: SOLID_NOT entities don't interact with anything,
	// and MOVETYPE_NOCLIP skips all collision (the C upstream's noclip
	// branch in SV_Physics_Noclip). Synthesize a clean trace at end.
	if in.Solid == SolidNot || in.MoveType == MoveTypeNoClip {
		return PushEntityOut{
			NewOrigin: end,
			Trace: bsptrace.Trace{
				Fraction: 1.0,
				EndPos:   end,
			},
			HitEntity: -1,
			HitWorld:  false,
		}, nil
	}

	// Pick the trace mode. The C upstream uses MOVE_MISSILE for
	// MOVETYPE_FLYMISSILE (so monster bounds widen to +-15) and
	// MOVE_NORMAL for everything else that reaches this point.
	// world.TraceMove does not take a MoveMode parameter -- the
	// upstream's MOVE_MISSILE behaviour (widening monster bounds to
	// +-15 so missiles have a uniform impact silhouette) is applied
	// here, on the candidates slice, so callers don't have to do it
	// per push site. Non-missile movetypes leave candidates as-is.
	clipCandidates := candidates
	if in.MoveType == MoveTypeFlyMissile {
		widened := make([]world.Target, len(candidates))
		for i, c := range candidates {
			c.Mins = world.MissileMonsterBounds.Mins
			c.Maxs = world.MissileMonsterBounds.Maxs
			widened[i] = c
		}
		clipCandidates = widened
	}

	res, err := world.TraceMove(worldmodel, clipCandidates, in.Origin, in.Mins, in.Maxs, end)
	if err != nil {
		return PushEntityOut{}, err
	}

	out := PushEntityOut{
		NewOrigin: res.Trace.EndPos,
		Trace:     res.Trace,
		HitEntity: res.EntityIdx,
		HitWorld:  res.WorldClipped,
	}
	return out, nil
}
