// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/model"
)

// StepSize is the maximum vertical drop CheckBottom tolerates between
// the center floor and any corner floor before declaring "off an edge
// that is not a staircase". tyrquake: STEPSIZE = 18 in sv_move.c.
const StepSize = 18.0

// CheckBottomIn is the input snapshot the caller builds from an
// edict's entvars BEFORE the check: where the entity is + its bbox.
//
// EntityKey is carried so the caller has one place to record "which
// edict am I checking" alongside its bounds; CheckBottom itself does
// not use it (the caller is responsible for excluding the moving
// entity from its candidate list, mirroring the pattern PushEntity
// and FlyMove use).
type CheckBottomIn struct {
	Origin    [3]float32
	Mins      [3]float32
	Maxs      [3]float32
	EntityKey Key
}

// CheckBottom reports whether the entity's four bottom corners are
// each over solid ground within [StepSize] of the center floor --
// i.e. the entity is standing on a floor / staircase, not hanging
// off a cliff edge. tyrquake: SV_CheckBottom in common/sv_move.c.
//
// Algorithm (verbatim from C, MOVE_NOMONSTERS semantics -- the
// caller's candidate slice must already exclude monsters / non-BSP
// entities the upstream MOVE_NOMONSTERS pass skips):
//
//  1. Compute the four base-corner positions at world z =
//     in.Origin.z + in.Mins.z (the bottom of the bbox).
//  2. Trace from the bbox center DOWN 2*[StepSize]. If the trace
//     completes without hitting anything (Fraction == 1.0), the
//     entity is hovering -- return (false, nil).
//  3. For each of the four corners, trace from that corner DOWN
//     2*[StepSize]. If the corner's floor (EndPos.z) is more than
//     [StepSize] below the center's floor, the entity is straddling
//     a cliff edge -- return (false, nil). A corner-trace that
//     completes without hitting anything (Fraction == 1.0) is
//     equivalent to "infinitely far below", which also fails.
//  4. All four corners are within [StepSize] -- return (true, nil).
//
// SIMPLIFICATION vs C upstream: the C SV_CheckBottom has a fast-path
// up front that returns true when all four under-corner points are
// already inside CONTENTS_SOLID (no traces needed). The Go port
// omits that fast-path -- it calls [PointContents] zero times and
// always runs the trace path. The two behaviours agree on the
// final boolean for every input the C fast-path accepts (if all
// four corners are in solid, the corner traces immediately impact
// at fraction 0 with EndPos.z == start.z == mins.z, equal to the
// center's bottom -- which passes the within-StepSize check).
//
// Errors propagate from [TraceMove]; on error the boolean is the
// zero value (false), matching the upstream's "any failure means
// not on bottom" convention.
//
// worldmodel is the world brushmodel; candidates is the filtered
// list of entities the bottom-traces must clip against (built from
// [World.AreaQuery]'s keys with the moving entity's own key excluded
// AND monsters dropped, mirroring MOVE_NOMONSTERS).
func CheckBottom(in CheckBottomIn, worldmodel *model.BrushModel, candidates []Target) (bool, error) {
	// Step 1: compute the base corners (at z = origin.z + mins.z).
	bottom := in.Origin[2] + in.Mins[2]
	corners := [4][2]float32{
		{in.Origin[0] + in.Mins[0], in.Origin[1] + in.Mins[1]}, // (-,-)
		{in.Origin[0] + in.Maxs[0], in.Origin[1] + in.Mins[1]}, // (+,-)
		{in.Origin[0] + in.Mins[0], in.Origin[1] + in.Maxs[1]}, // (-,+)
		{in.Origin[0] + in.Maxs[0], in.Origin[1] + in.Maxs[1]}, // (+,+)
	}

	// Step 2: center down-trace. Start at the center of the top of
	// the bbox? No -- the C upstream starts at z = mins[2] (the
	// bottom of the bbox) and traces 2*STEPSIZE further down. The
	// X/Y is the center of the bbox.
	centerXY := [2]float32{
		(in.Origin[0] + in.Mins[0] + in.Origin[0] + in.Maxs[0]) * 0.5,
		(in.Origin[1] + in.Mins[1] + in.Origin[1] + in.Maxs[1]) * 0.5,
	}
	centerStart := [3]float32{centerXY[0], centerXY[1], bottom}
	centerEnd := [3]float32{centerXY[0], centerXY[1], bottom - 2*StepSize}

	res, err := TraceMove(worldmodel, candidates, centerStart, [3]float32{}, [3]float32{}, centerEnd)
	if err != nil {
		return false, err
	}
	if res.Trace.Fraction == 1.0 {
		// Center is hovering -- no floor within 2*StepSize.
		return false, nil
	}
	mid := res.Trace.EndPos[2]

	// Step 3: corner down-traces. Each corner must hit a floor whose
	// z is no more than StepSize below the center's floor (mid).
	//
	// The per-corner TraceMove call cannot surface an error that the
	// center trace above didn't already catch: CheckBottom passes the
	// SAME (worldmodel + candidates) to every call, and the trace
	// error sources (corrupt world hull, SOLID_BSP candidate with nil
	// BrushModel) all fire on EVERY start point. So if we reached
	// this loop, the center trace succeeded, which means every
	// subsequent trace will too. The corner trace's error branch is
	// dropped per the bsptrace pattern of removing unreachable code.
	for _, c := range corners {
		start := [3]float32{c[0], c[1], bottom}
		end := [3]float32{c[0], c[1], bottom - 2*StepSize}
		cr, _ := TraceMove(worldmodel, candidates, start, [3]float32{}, [3]float32{}, end)
		if cr.Trace.Fraction == 1.0 {
			// Corner trace fell all the way -- equivalent to a
			// floor infinitely far below the center. Cliff edge.
			return false, nil
		}
		if mid-cr.Trace.EndPos[2] > StepSize {
			// Corner floor is more than StepSize below center floor
			// -- the entity is straddling a step too tall to be a
			// staircase. Cliff edge.
			return false, nil
		}
	}

	// All four corners are over a floor within StepSize of the
	// center floor -- the entity is supported.
	return true, nil
}
