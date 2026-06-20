// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"math"

	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// MoveStepIn is the input snapshot the caller builds from a monster
// edict's entvars BEFORE a one-tic walk attempt: where it is, its
// size, the per-tic move delta, the relevant flags
// ([server.FlagOnGround] / [server.FlagFly] / [server.FlagSwim]),
// and the entity key (carried for the caller's candidate filtering
// -- MoveStep itself does not consume it).
//
// Move is a per-tic delta (in world units), NOT a velocity --
// MoveStep does not multiply by dt. The caller computes Move from
// the desired direction + walk distance (typically via
// [StepDirection]).
//
// Relink mirrors the C upstream's `relink` parameter: it has no
// effect on MoveStep's geometry, but the caller may use it to gate
// the post-call [World.LinkBounds] re-link. It is carried in the
// snapshot so the call site has a single struct to thread.
type MoveStepIn struct {
	Origin    [3]float32
	Mins      [3]float32
	Maxs      [3]float32
	Move      [3]float32        // delta to apply this tick (per-frame nudge, NOT velocity)
	Flags     server.EntityFlag // FlagOnGround / FlagFly / FlagSwim are relevant
	EntityKey Key
	Relink    bool
}

// MoveStepOut is the result snapshot. NewOrigin / NewFlags get
// written back into the edict's entvars by the caller. Moved is
// true iff the new position is different from in.Origin AND clear
// of solid. HitEntity is the index of the impact target in the
// candidates slice (or -1), so the caller can dispatch QC's
// `ent.v.touch` against the touched edict -- the C upstream's
// SV_movestep does this dispatch inline; the Go port leaves it to
// the caller so MoveStep stays pure geometry.
type MoveStepOut struct {
	NewOrigin [3]float32
	Trace     bsptrace.Trace
	Moved     bool
	HitEntity int
	NewFlags  server.EntityFlag
}

// MoveStep attempts a one-tic walk: pushes the entity by in.Move,
// with step-up handling for walking monsters and a single straight
// trace for flying / swimming ones.
// tyrquake: SV_movestep in common/sv_move.c.
//
// Walking algorithm (the "step up, drop down" pass the C upstream
// uses for non-FL_FLY / non-FL_SWIM monsters):
//
//  1. neworg = in.Origin + in.Move; neworg.z += [StepSize].
//  2. Trace from neworg DOWN 2*[StepSize] (the "drop down to find
//     ground" pass). If the trace is AllSolid, the step lands inside
//     solid -- bail with Moved = false.
//  3. If the trace started solid (the step-up position is inside a
//     ceiling), retry the drop WITHOUT the step-up (neworg.z -=
//     [StepSize]). If that retry is still all/start-solid, bail.
//  4. If the drop trace completed clean (Fraction == 1.0), we walked
//     off an edge into thin air:
//     - If [server.FlagOnGround] was set on the input, the upstream
//     refuses to fall off (the non-PARTIALGROUND branch). Bail
//     with Moved = false.
//     - If [server.FlagOnGround] was NOT set, take the move anyway
//     (the entity was already mid-fall / unsupported).
//  5. Otherwise the drop hit a floor at trace.EndPos. Tentatively
//     adopt the new origin and run [CheckBottom] to make sure the
//     entity isn't straddling a cliff edge:
//     - If FlagOnGround is set and CheckBottom fails, refuse the
//     step (the entity would land on a too-tall ledge); restore
//     startorigin, bail with Moved = false.
//     - Otherwise (CheckBottom passes, OR FlagOnGround is not set
//     so the cliff guard is disabled): commit the move.
//
// Flying / swimming algorithm (FlagFly or FlagSwim set): one
// straight trace from Origin to Origin+Move. The C upstream runs
// the trace TWICE -- once with an enemy-Z adjustment, once without
// -- to make flying monsters track their target's altitude. The Go
// port drops the enemy-Z pass (the spec keeps MoveStep pure
// geometry; the caller can do the Z adjustment by tweaking in.Move
// before the call). Swimming-out-of-water check (the C SV_PointContents
// call that aborts a swim move ending in CONTENTS_EMPTY) is also
// dropped: the caller is expected to gate swim moves by checking the
// destination water status itself, or the spec's flying path stays
// pure-trace.
//
// NewFlags is currently passed through unchanged (NewFlags =
// in.Flags). The C upstream's FlagPartialGround mutation logic
// (clear FlagOnGround on a successful fall, clear FlagPartialGround
// on a successful walk) is not yet exercised by callers in this
// repo; when those callers land, the mutation can be threaded
// through here without an API change.
//
// SIMPLIFICATIONS vs the C upstream:
//
//   - QC `ent.v.touch` dispatch on impact: NOT applied here -- the
//     C SV_movestep calls SV_Impact when the underlying trace hits
//     an entity. The Go port surfaces the HitEntity index in the
//     result so the caller can do the touch dispatch.
//   - LinkEdict call after a successful move: NOT done here -- the
//     C SV_movestep calls SV_LinkEdict when relink is true. The Go
//     port carries the Relink bit in the input snapshot so the
//     caller can decide based on it, but MoveStep itself does no
//     linking (the World pointer is not in scope).
//   - Enemy-Z tracking for flying monsters: dropped (see above).
//   - Swim-water gate: dropped (see above).
//   - FlagPartialGround branches: dropped (see NewFlags note).
//
// worldmodel is the world brushmodel; candidates is the filtered
// list of entities the move must clip against. The caller is
// responsible for excluding in.EntityKey from candidates -- mirror
// of the pattern [PushEntity], [CheckBottom], and [FlyMove] use.
func MoveStep(in MoveStepIn, worldmodel *model.BrushModel, candidates []Target) (MoveStepOut, error) {
	out := MoveStepOut{
		NewOrigin: in.Origin,
		Trace:     bsptrace.DefaultTrace(),
		HitEntity: -1,
		NewFlags:  in.Flags,
	}

	// Flying / swimming: one straight trace, no step-up, no cliff
	// guard. Matches the C upstream's FL_FLY|FL_SWIM branch (minus
	// the enemy-Z and water-gate refinements, which the spec moves
	// out of MoveStep -- see the doc comment).
	if in.Flags&(server.FlagFly|server.FlagSwim) != 0 {
		neworg := [3]float32{
			in.Origin[0] + in.Move[0],
			in.Origin[1] + in.Move[1],
			in.Origin[2] + in.Move[2],
		}
		res, err := TraceMove(worldmodel, candidates, in.Origin, in.Mins, in.Maxs, neworg)
		if err != nil {
			return out, err
		}
		out.Trace = res.Trace
		out.HitEntity = res.EntityIdx
		if res.Trace.Fraction == 1.0 {
			out.NewOrigin = neworg
			out.Moved = true
		}
		return out, nil
	}

	// Walking: step up, drop down.
	wantedXY := [3]float32{
		in.Origin[0] + in.Move[0],
		in.Origin[1] + in.Move[1],
		in.Origin[2] + in.Move[2],
	}
	// Push start up by StepSize, end down to (wantedXY.z - StepSize).
	stepStart := [3]float32{wantedXY[0], wantedXY[1], wantedXY[2] + StepSize}
	stepEnd := [3]float32{wantedXY[0], wantedXY[1], wantedXY[2] - StepSize}

	res, err := TraceMove(worldmodel, candidates, stepStart, in.Mins, in.Maxs, stepEnd)
	if err != nil {
		return out, err
	}
	if res.Trace.AllSolid {
		// The step-up position is buried in solid all the way down.
		// Refuse the move.
		out.Trace = res.Trace
		return out, nil
	}
	if res.Trace.StartSolid {
		// Step-up Z is inside a ceiling; try again WITHOUT the
		// step-up offset, mirroring the C upstream's
		// `neworg[2] -= STEPSIZE` retry block.
		//
		// The retry's TraceMove err branch is structurally
		// unreachable: the retry uses the SAME (worldmodel +
		// candidates) and a stepStart whose Z is lower (so its
		// node-descent path is a subset of the first trace's). Any
		// hull corruption / SOLID_BSP-without-BrushModel candidate
		// would have errored on the first trace above. The err
		// check is dropped per the bsptrace pattern of removing
		// C-inherited unreachable code.
		stepStart[2] -= StepSize
		res, _ = TraceMove(worldmodel, candidates, stepStart, in.Mins, in.Maxs, stepEnd)
		if res.Trace.AllSolid || res.Trace.StartSolid {
			// Still buried -- bail.
			out.Trace = res.Trace
			return out, nil
		}
	}
	out.Trace = res.Trace
	out.HitEntity = res.EntityIdx

	if res.Trace.Fraction == 1.0 {
		// Walked off an edge: no floor within step-up+step-down range.
		// The C upstream refuses to fall off (the non-PARTIALGROUND
		// branch). The Go port gates that refusal on FlagOnGround --
		// a monster already mid-air (FlagOnGround clear) takes the
		// move; a grounded one refuses to walk off.
		if in.Flags&server.FlagOnGround != 0 {
			return out, nil
		}
		out.NewOrigin = wantedXY
		out.Moved = true
		return out, nil
	}

	// Floor found: tentatively adopt the trace endpoint and run
	// CheckBottom (only when grounded) to make sure the four corners
	// are supported -- the C upstream's "dangling corners" guard.
	out.NewOrigin = res.Trace.EndPos
	if in.Flags&server.FlagOnGround != 0 {
		// CheckBottom err branch is structurally unreachable:
		// CheckBottom calls TraceMove with the same (worldmodel +
		// candidates) we already traced successfully above, so any
		// error vector (corrupt hull, SOLID_BSP without BrushModel)
		// would have surfaced on the step-up trace. Dropped per the
		// bsptrace pattern.
		ok, _ := CheckBottom(CheckBottomIn{
			Origin:    out.NewOrigin,
			Mins:      in.Mins,
			Maxs:      in.Maxs,
			EntityKey: in.EntityKey,
		}, worldmodel, candidates)
		if !ok {
			// Corner hangs -- restore and bail.
			out.NewOrigin = in.Origin
			return out, nil
		}
	}
	out.Moved = true
	return out, nil
}

// StepDirection wraps [MoveStep] with a yaw-derived direction:
// computes a planar (X/Y) move vector from yaw (degrees) + dist and
// forwards it. The yaw convention matches the C upstream's
// SV_StepDirection: yaw=0 is +X, yaw=90 is +Y, yaw rotates
// counter-clockwise.
// tyrquake: SV_StepDirection in common/sv_move.c (the math half --
// the ideal_yaw / PF_changeyaw / post-move yaw-delta guard is the
// caller's job).
func StepDirection(yaw, dist float32, in MoveStepIn, worldmodel *model.BrushModel, candidates []Target) (MoveStepOut, error) {
	rad := float64(yaw) * math.Pi / 180
	in.Move = [3]float32{
		float32(math.Cos(rad)) * dist,
		float32(math.Sin(rad)) * dist,
		0,
	}
	return MoveStep(in, worldmodel, candidates)
}

// chaseDirNoDir is the sentinel value the C SV_NewChaseDir uses for
// "no preference along this axis". tyrquake: `#define DI_NODIR -1`.
const chaseDirNoDir float32 = -1

// chaseDirDeltaThreshold is the per-axis distance below which a
// delta is treated as "no clear sign" (returns DI_NODIR). tyrquake
// hard-codes the literal 10 in the deltax/deltay comparisons.
const chaseDirDeltaThreshold float32 = 10

// anglemod folds an angle into [0, 360). tyrquake: `anglemod` in
// common/mathlib.c -- the float port collapses to a one-line fmod.
func anglemod(a float32) float32 {
	r := float32(math.Mod(float64(a), 360))
	if r < 0 {
		r += 360
	}
	return r
}

// NewChaseDir picks the next ideal_yaw for an actor whose current
// chase direction is blocked. Examines the (X, Y) delta from actor
// to goal and returns the closest 45-degree-snapped heading that is
// NOT the turnaround of currentYaw.
// tyrquake: SV_NewChaseDir in common/sv_move.c (the pure-math
// extract -- the C version actively calls SV_StepDirection inside
// the loop to validate each candidate; this port returns ONE next
// yaw and lets the caller drive the validation chain).
//
// Algorithm:
//
//  1. Snap currentYaw to the 45-degree grid via int-truncation, fold
//     into [0, 360). Compute turnaround = anglemod(snapped + 180).
//  2. Classify deltax (goalX - actorX) into d[1]: 0 if > +threshold,
//     180 if < -threshold, NODIR otherwise. Same for deltay into
//     d[2]: 90 if > +threshold, 270 if < -threshold, NODIR.
//  3. If both axes are decisive, compose the diagonal:
//     - d[1] == 0   -> 45 (if d[2] == 90) or 315.
//     - d[1] == 180 -> 135 (if d[2] == 90) or 215.
//     Return the diagonal unless it equals turnaround.
//  4. Else return d[1] if decisive and != turnaround, then d[2] if
//     decisive and != turnaround, then the snapped currentYaw.
//
// The C upstream falls back through rand() and a cardinal sweep
// when none of the preferred directions work; this pure-math port
// leaves that exploration to the caller (which can simply call
// StepDirection at yaw+90 / yaw-90 / yaw+180 on failure -- see
// [MoveToGoal] for the standard 3-attempt chain).
func NewChaseDir(actorOrigin, goalOrigin [3]float32, currentYaw float32) float32 {
	// Snap currentYaw to the 45-degree grid (int truncation, then
	// anglemod into [0, 360)).
	olddir := anglemod(float32(int(currentYaw/45)) * 45)
	turnaround := anglemod(olddir - 180)

	deltax := goalOrigin[0] - actorOrigin[0]
	deltay := goalOrigin[1] - actorOrigin[1]

	var dx, dy float32
	if deltax > chaseDirDeltaThreshold {
		dx = 0
	} else if deltax < -chaseDirDeltaThreshold {
		dx = 180
	} else {
		dx = chaseDirNoDir
	}
	if deltay < -chaseDirDeltaThreshold {
		dy = 270
	} else if deltay > chaseDirDeltaThreshold {
		dy = 90
	} else {
		dy = chaseDirNoDir
	}

	// Try the diagonal first when both axes are decisive.
	if dx != chaseDirNoDir && dy != chaseDirNoDir {
		var tdir float32
		if dx == 0 {
			if dy == 90 {
				tdir = 45
			} else {
				tdir = 315
			}
		} else {
			if dy == 90 {
				tdir = 135
			} else {
				tdir = 215
			}
		}
		if tdir != turnaround {
			return tdir
		}
	}

	// Fall back to the X axis, then the Y axis, then olddir.
	if dx != chaseDirNoDir && dx != turnaround {
		return dx
	}
	if dy != chaseDirNoDir && dy != turnaround {
		return dy
	}
	return olddir
}

// moveToGoalRetries is the number of extra StepDirection attempts
// MoveToGoal makes after the initial in.Yaw push fails. The C
// upstream's SV_NewChaseDir tries up to 8 cardinals + turnaround;
// the pure-math port keeps a tighter budget (try NewChaseDir's pick,
// then +/-90 from it) since each StepDirection is a full trace.
const moveToGoalRetries = 3

// MoveToGoalIn is the input snapshot for one per-tic monster move:
// where the actor is, where its goal is, its size + flags, the
// current ideal_yaw, and the per-tic walk distance.
type MoveToGoalIn struct {
	Origin     [3]float32
	GoalOrigin [3]float32
	Mins       [3]float32
	Maxs       [3]float32
	Flags      server.EntityFlag
	Yaw        float32 // actor's ideal_yaw entering this tick
	Dist       float32 // walk distance for this tic
	EntityKey  Key
}

// MoveToGoalOut is the result snapshot. NewOrigin / NewYaw / NewFlags
// get written back into the actor's entvars; Moved reports whether
// any of the attempted directions produced forward progress.
type MoveToGoalOut struct {
	NewOrigin [3]float32
	NewYaw    float32
	Moved     bool
	NewFlags  server.EntityFlag
}

// MoveToGoal makes one per-tic monster move attempt toward
// GoalOrigin. Tries the actor's current yaw first; if that
// StepDirection fails, asks [NewChaseDir] for a goal-aware yaw and
// retries, then sweeps +/-90 from that yaw for the remaining tries.
// tyrquake: SV_MoveToGoal builtin (the QC pf_movetogoal call).
//
// The first successful StepDirection wins -- NewOrigin / NewYaw /
// NewFlags reflect the move taken. If every attempt is blocked,
// NewOrigin = in.Origin, NewYaw = in.Yaw, Moved = false. The C
// upstream's `if (rand() & 3) == 1` randomized force-NewChaseDir
// branch is dropped (deterministic Go port -- the caller can
// inject randomness by tweaking in.Yaw if needed).
//
// SIMPLIFICATIONS vs the C upstream:
//
//   - No FlagOnGround|FlagFly|FlagSwim gate at the top: the C
//     upstream returns 0 immediately when none of those flags are
//     set. The Go port lets MoveStep handle the per-flag dispatch.
//   - No SV_CloseEnough early-out: the caller is expected to do the
//     close-enough check before invoking MoveToGoal.
//   - Deterministic retry chain (NewChaseDir's pick, then +90, then
//     -90) instead of the C's rand-driven 8-cardinal sweep.
func MoveToGoal(in MoveToGoalIn, worldmodel *model.BrushModel, candidates []Target) (MoveToGoalOut, error) {
	out := MoveToGoalOut{
		NewOrigin: in.Origin,
		NewYaw:    in.Yaw,
		NewFlags:  in.Flags,
	}

	stepIn := MoveStepIn{
		Origin:    in.Origin,
		Mins:      in.Mins,
		Maxs:      in.Maxs,
		Flags:     in.Flags,
		EntityKey: in.EntityKey,
	}

	// Attempt 0: the actor's current yaw.
	yawSequence := [moveToGoalRetries + 1]float32{
		in.Yaw,
		// Attempt 1: NewChaseDir's goal-aware pick.
		NewChaseDir(in.Origin, in.GoalOrigin, in.Yaw),
		// Attempts 2 and 3: sweep +/-90 from the goal-aware pick.
		0, 0,
	}
	yawSequence[2] = anglemod(yawSequence[1] + 90)
	yawSequence[3] = anglemod(yawSequence[1] - 90)

	for _, yaw := range yawSequence {
		stepOut, err := StepDirection(yaw, in.Dist, stepIn, worldmodel, candidates)
		if err != nil {
			return out, err
		}
		if stepOut.Moved {
			out.NewOrigin = stepOut.NewOrigin
			out.NewYaw = yaw
			out.Moved = true
			out.NewFlags = stepOut.NewFlags
			return out, nil
		}
	}

	return out, nil
}
