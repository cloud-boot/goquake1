// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// SweptBounds computes the world-space AABB enclosing every point
// the (mins..maxs) test object occupies as it sweeps from start to
// end. The +-1 epsilon matches the C upstream so movement clipped
// an epsilon away from an edge still includes the bounding box.
// tyrquake: SV_MoveBounds.
//
// The result is the bounding box callers should hand to AreaQuery
// to enumerate the entities the trace must clip against.
func SweptBounds(start, end, mins, maxs [3]float32) (smin, smax [3]float32) {
	for i := 0; i < 3; i++ {
		if end[i] > start[i] {
			smin[i] = start[i] + mins[i] - 1
			smax[i] = end[i] + maxs[i] + 1
		} else {
			smin[i] = end[i] + mins[i] - 1
			smax[i] = start[i] + maxs[i] + 1
		}
	}
	return
}

// MissileMonsterBounds is the +-15 cube the C upstream substitutes
// for the trace's monster bounds when MoveMode == MoveModeMissile,
// so missiles get a uniform impact silhouette regardless of the
// firing entity's actual size. tyrquake: the clip.monster
// (-15..15) assignment block inside SV_TraceMove.
var MissileMonsterBounds = struct {
	Mins, Maxs [3]float32
}{
	Mins: [3]float32{-15, -15, -15},
	Maxs: [3]float32{15, 15, 15},
}

// ClipToTarget runs a single swept-trace of the (mins..maxs) test
// object from start to end against target's clipping hull, picked
// by [HullForBounds]. Returns the resulting [bsptrace.Trace] (with
// EndPos in world coordinates, not hull-local).
// tyrquake: SV_ClipToEntity.
//
// The trace starts with [bsptrace.DefaultTrace] (Fraction = 1.0,
// AllSolid = true). It is then run on the bsptrace.Hull from
// HullForBounds; when an impact lands (Fraction < 1.0), the
// hull-local EndPos is translated back to world coordinates by
// adding the per-target offset.
func ClipToTarget(target Target, start, mins, maxs, end [3]float32) (bsptrace.Trace, error) {
	tr := bsptrace.DefaultTrace()
	tr.EndPos = end

	hull, offset, err := HullForBounds(mins, maxs, target)
	if err != nil {
		return tr, err
	}

	startL := [3]float32{
		start[0] - offset[0],
		start[1] - offset[1],
		start[2] - offset[2],
	}
	endL := [3]float32{
		end[0] - offset[0],
		end[1] - offset[1],
		end[2] - offset[2],
	}

	if _, err := bsptrace.TraceHull(&hull, hull.FirstClipNode, startL, endL, &tr); err != nil {
		return tr, err
	}

	if tr.Fraction != 1.0 {
		tr.EndPos[0] += offset[0]
		tr.EndPos[1] += offset[1]
		tr.EndPos[2] += offset[2]
	}
	return tr, nil
}

// TraceResult is what [TraceMove] returns: the combined trace plus
// what (if anything) clipped the move. WorldClipped is true iff the
// world brushmodel was the (or a) clipper. EntityIdx is the index
// into the caller's candidates slice that produced the final impact,
// or -1 if no candidate clipped (i.e. only world clipped, or the
// move was clean).
//
// The C upstream returns a single clipent pointer: nil when nothing
// clipped, sv.edicts when only the world clipped, the touching
// entity otherwise. The Go port unpacks the same info into two
// fields so callers can distinguish "no clip" from "world-only clip"
// without juggling a sentinel pointer.
type TraceResult struct {
	Trace        bsptrace.Trace
	WorldClipped bool
	EntityIdx    int
}

// TraceMove runs a swept-trace through the world brushmodel and a
// caller-supplied list of candidate targets, combining the per-
// target traces into one final trace.
// tyrquake: SV_TraceMove.
//
// The caller is responsible for the candidate list: typically
//
//  1. Compute the swept AABB via [SweptBounds] (using mins/maxs,
//     widened to [MissileMonsterBounds] when mode == MoveModeMissile).
//  2. Query the area tree via [World.AreaQuery] with [QuerySolidsOnly].
//  3. Filter the returned Keys by the per-edict rules world doesn't
//     know about (passedict, owner ownership, FL_MONSTER bounds
//     dispatch, MoveModeNoMonsters non-BSP skip, SOLID_NOT/TRIGGER
//     defensive skips, point-entities-never-interact rule).
//  4. Build a []Target from the survivors with the correct mins/maxs
//     per-candidate (mons-bounds for missiles, normal otherwise).
//
// TraceMove itself just combines the world clip + per-target clips
// per tyrquake's running rule:
//
//   - If a per-target trace is "interesting" (allsolid, startsolid,
//     or its fraction is shorter than the running trace), replace
//     the running trace with that target's trace; preserve the
//     running StartSolid bit if it was already set.
//   - Else if the per-target trace only set startsolid, just merge
//     the startsolid bit into the running trace.
//
// Early-exit: the iteration stops as soon as trace.AllSolid is true
// (the C upstream's "if (trace->allsolid) return clipent" inside
// SV_ClipToLinks_r). Callers that want strict iteration even on
// AllSolid should split the call up.
func TraceMove(worldmodel *model.BrushModel, candidates []Target, start, mins, maxs, end [3]float32) (TraceResult, error) {
	result := TraceResult{EntityIdx: -1}

	worldTarget := Target{
		Origin:     [3]float32{0, 0, 0},
		Solid:      server.SolidBSP,
		BrushModel: worldmodel,
	}
	tr, err := ClipToTarget(worldTarget, start, mins, maxs, end)
	if err != nil {
		return result, err
	}
	result.Trace = tr
	result.WorldClipped = tr.Fraction < 1.0 || tr.StartSolid

	for i, target := range candidates {
		if result.Trace.AllSolid {
			return result, nil
		}
		stack, err := ClipToTarget(target, start, mins, maxs, end)
		if err != nil {
			return result, err
		}
		// The C upstream's combine-trace rule is:
		//   if (stack.allsolid || stack.startsolid || stack.fraction < trace.fraction)
		//       <replace trace; preserve startsolid bit>
		//   else if (stack.startsolid)
		//       trace.startsolid = true
		// The else-if branch is structurally unreachable: stack.startsolid
		// is already an OR in the first predicate, so any stack.startsolid
		// case takes the first branch. Dropped here, matching the bsptrace
		// pattern of removing C-inherited dead code.
		if stack.AllSolid || stack.StartSolid || stack.Fraction < result.Trace.Fraction {
			prevStartSolid := result.Trace.StartSolid
			result.Trace = stack
			if prevStartSolid {
				result.Trace.StartSolid = true
			}
			result.EntityIdx = i
		}
	}
	return result, nil
}
