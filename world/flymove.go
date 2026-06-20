// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// flyMoveMaxBumps is the maximum number of wall bounces a single
// SV_FlyMove call iterates through before bailing. tyrquake hard-
// codes `numbumps = 4` in common/sv_phys.c -- four chances to find
// a sliding direction before we give up and return whatever progress
// (or stop) the last iteration produced.
const flyMoveMaxBumps = 4

// flyMoveMaxClipPlanes is the depth of the wall-normal stack the
// slide-against-multiple-planes pass walks. tyrquake: MAX_CLIP_PLANES
// = 5. The C upstream uses it both as the fixed-size array bound AND
// as the "this shouldn't really happen" panic threshold. The Go port
// only uses the array-bound role: the panic branch is structurally
// unreachable here because numplanes can grow by at most 1 per bump,
// the bump loop caps at [flyMoveMaxBumps] = 4, and the "Fraction > 0"
// numplanes reset further limits accumulation -- so numplanes never
// reaches 5. The defensive check is dropped per the bsptrace pattern
// of removing C-inherited dead code.
const flyMoveMaxClipPlanes = 5

// flyMoveFloorNormalZ is the upstream `> 0.7` cosine threshold below
// which a contact plane is considered a wall (not a floor) for the
// purpose of the [server.BlockedFloor] accumulator bit. tyrquake
// hard-codes the 0.7 literal in the `if (trace.plane.normal[2] >
// 0.7)` test inside SV_FlyMove; it corresponds to about a 45-degree
// slope -- shallower than that and the entity stands on it,
// steeper and it counts as a wall.
//
// Distinct from [server.ClipVelocity]'s [server.BlockedFloor]
// threshold (which fires on any normal[2] > 0). SV_FlyMove uses its
// own thresholds for the per-iteration `blocked` accumulator and
// does NOT call ClipVelocity for the bit-setting work -- only for
// the velocity reflection.
const flyMoveFloorNormalZ = 0.7

// FlyMoveIn is the input snapshot the caller builds from an edict's
// entvars BEFORE the move: position, size, velocity, dt, and the
// entity's identity (so the caller's candidate filtering can skip
// it).
//
// EntityKey is carried so the caller has one place to record "which
// edict am I moving" alongside its bounds; FlyMove itself does not
// use it (the caller is responsible for excluding the moving entity
// from its candidate list, mirroring the pattern PushEntity and
// CheckBottom use).
type FlyMoveIn struct {
	Origin    [3]float32
	Mins      [3]float32
	Maxs      [3]float32
	Velocity  [3]float32 // initial velocity
	Time      float32    // remaining time this tick (typically frametime)
	EntityKey Key        // skip this entity in candidate matching
}

// FlyMoveOut is the result snapshot. NewOrigin / NewVelocity get
// written back into the edict's entvars. Blocked reports what kinds
// of surfaces clipped the move (a bitmask of [server.BlockedFloor],
// [server.BlockedStep]). StepTrace, if non-zero Fraction, carries
// the last wall-impact trace -- callers use it for ground-snap +
// step-up logic (the MOVETYPE_STEP branch of SV_Physics).
type FlyMoveOut struct {
	NewOrigin   [3]float32
	NewVelocity [3]float32
	Blocked     server.MoveBlocked
	StepTrace   bsptrace.Trace
}

// FlyMove iteratively slides Origin along Velocity for Time seconds,
// clipping against the worldmodel + candidates and reflecting
// velocity off each wall the move hits. The slide-along-walls logic
// is the core movement primitive for MOVETYPE_FLY, MOVETYPE_WALK,
// and MOVETYPE_STEP; the per-movetype handler dispatches FlyMove +
// friction + gravity + RunThink separately.
// tyrquake: int SV_FlyMove(ent, time, steptrace) in common/sv_phys.c.
//
// Algorithm (from the C upstream, MAX_CLIP_PLANES = 5, 4 iteration
// max):
//
//	for bump in 0..3:
//	  if Velocity all-zero OR Time <= 0: break
//	  end = Origin + Velocity * Time
//	  trace = TraceMove(Origin, mins, maxs, end, candidates)
//	  if trace.AllSolid:
//	    zero velocity; return blocked = BlockedFloor | BlockedStep
//	  if trace.Fraction > 0:
//	    Origin = trace.EndPos
//	    OriginalVelocity = Velocity (re-seed the slide source)
//	    planeStack = []
//	  if trace.Fraction == 1: clean move; break
//	  Note the impact normal in Blocked:
//	    normal[2] > 0.7  -> BlockedFloor
//	    normal[2] == 0   -> BlockedStep; save StepTrace
//	  Time -= Time * trace.Fraction
//	  planeStack = append(planeStack, trace.Plane.Normal)
//	  // (the C upstream's defensive `len(planeStack) >= 5` short-
//	  // circuit is structurally unreachable here and dropped --
//	  // see the [flyMoveMaxClipPlanes] comment for the proof)
//	  For each plane in stack:
//	    candidate = ClipVelocity(OriginalVelocity, plane, 1.0)
//	    if candidate dotted against EVERY OTHER plane in stack
//	      is >= 0:  velocity = candidate; goto bumpEnd
//	  // No single plane gives a valid slide -> "crease" path.
//	  if len(stack) != 2:
//	    zero velocity; return blocked | BlockedFloor | BlockedStep
//	  // 2 planes: slide along the crease (cross product).
//	  dir = cross(plane[0], plane[1])
//	  velocity = dir * dot(dir, velocity)
//	  bumpEnd:
//	  if dot(velocity, primalVelocity) <= 0:
//	    zero velocity; return blocked
//	end: return blocked.
//
// Bit-mask divergences from [server.ClipVelocity]:
//
//   - Floor bit threshold: SV_FlyMove sets BlockedFloor on
//     normal[2] > 0.7 (~45-degree slope). ClipVelocity sets it on
//     normal[2] > 0 (any upward-facing surface). Both are correct
//     per the C upstream; they're different bits computed for
//     different purposes (the FlyMove `blocked` accumulator drives
//     the gameplay-side ground/wall classification; ClipVelocity's
//     return value is consumed by other physics paths).
//   - Wall/Step bit: identical (normal[2] == 0 in both).
//
// The C upstream's "stop" bit (MOVE_CLIP_STOP = 4) returned on the
// corner-trap branch is not part of this port's public bitmask
// surface -- FlyMoveOut.Blocked uses BlockedFloor|BlockedStep for
// every trapped-stop case, matching what the spec's public API
// exposes.
//
// Friction, gravity, and RunThink are NOT applied here -- the
// per-MOVETYPE handler dispatches FlyMove + the other steps
// separately.
//
// Note on file location: the original spec called for
// engine/server/flymove.go, but engine/world/area.go already imports
// engine/server (for the Solid constants), so a server-side FlyMove
// that needs world.TraceMove would create an import cycle. The other
// physics primitives that need world.TraceMove (PushEntity,
// CheckBottom) ended up living in engine/world/ for the same reason.
// This file follows that pattern.
//
// Parameters:
//
//	in           initial state snapshot
//	worldmodel   world brushmodel (must be non-nil for non-trivial moves)
//	candidates   pre-filtered solid entities the move must clip against;
//	             the caller is responsible for excluding in.EntityKey
//	             (and any owner-pair / passdict relationships) from
//	             this slice -- FlyMove does not re-filter.
func FlyMove(in FlyMoveIn, worldmodel *model.BrushModel, candidates []Target) (FlyMoveOut, error) {
	out := FlyMoveOut{
		NewOrigin:   in.Origin,
		NewVelocity: in.Velocity,
		StepTrace:   bsptrace.DefaultTrace(),
	}

	primal := in.Velocity
	original := in.Velocity
	var planes [flyMoveMaxClipPlanes][3]float32
	numplanes := 0
	timeLeft := in.Time

	for bump := 0; bump < flyMoveMaxBumps; bump++ {
		// Early-exit on zero remaining time or no velocity -- no
		// further iteration can change the result.
		if timeLeft <= 0 {
			return out, nil
		}
		if out.NewVelocity[0] == 0 && out.NewVelocity[1] == 0 && out.NewVelocity[2] == 0 {
			return out, nil
		}

		end := [3]float32{
			out.NewOrigin[0] + timeLeft*out.NewVelocity[0],
			out.NewOrigin[1] + timeLeft*out.NewVelocity[1],
			out.NewOrigin[2] + timeLeft*out.NewVelocity[2],
		}

		res, err := TraceMove(worldmodel, candidates, out.NewOrigin, in.Mins, in.Maxs, end)
		if err != nil {
			return out, err
		}
		trace := res.Trace

		if trace.AllSolid {
			// Entity is trapped inside another solid. Zero velocity
			// and report both floor+wall blocks (the C upstream
			// returns MOVE_CLIP_FLOOR | MOVE_CLIP_WALL).
			out.NewVelocity = [3]float32{0, 0, 0}
			out.Blocked |= server.BlockedFloor | server.BlockedStep
			return out, nil
		}

		if trace.Fraction > 0 {
			// Made progress this iteration -- adopt the new origin
			// and re-seed the slide source. The plane stack resets
			// because any prior accumulated walls are no longer
			// adjacent.
			out.NewOrigin = trace.EndPos
			original = out.NewVelocity
			numplanes = 0
		}

		if trace.Fraction == 1 {
			// Clean move all the way to `end`. Done.
			return out, nil
		}

		// Record the impact in the accumulator bitmask. The upstream
		// thresholds are stricter than ClipVelocity's: floor needs
		// normal[2] > 0.7 (a real walkable slope), wall needs
		// normal[2] == 0 (a truly vertical face).
		normal := trace.Plane.Normal
		if normal[2] > flyMoveFloorNormalZ {
			out.Blocked |= server.BlockedFloor
		}
		if normal[2] == 0 {
			out.Blocked |= server.BlockedStep
			// Save the wall trace for the caller's MOVETYPE_STEP
			// step-up logic. tyrquake: `if (steptrace) *steptrace =
			// trace;` -- only set on wall hits, NOT on floor or
			// sloped impacts.
			out.StepTrace = trace
		}

		// Burn the time the trace covered.
		timeLeft -= timeLeft * trace.Fraction

		// numplanes can grow by at most 1 per bump, the bump loop is
		// capped at flyMoveMaxBumps = 4, and the "Fraction > 0" branch
		// above resets numplanes to 0 -- so numplanes is structurally
		// < flyMoveMaxClipPlanes (5) here. The C upstream's defensive
		// "numplanes >= MAX_CLIP_PLANES" branch is dropped (bsptrace
		// pattern: remove C-inherited unreachable code).
		planes[numplanes] = normal
		numplanes++

		// Find a wall normal whose ClipVelocity'd-against-original
		// velocity doesn't slide INTO any other accumulated plane.
		var newVel [3]float32
		i := 0
		for ; i < numplanes; i++ {
			newVel, _ = server.ClipVelocity(original, planes[i], 1)
			j := 0
			for ; j < numplanes; j++ {
				if j == i {
					continue
				}
				dot := newVel[0]*planes[j][0] +
					newVel[1]*planes[j][1] +
					newVel[2]*planes[j][2]
				if dot < 0 {
					// Sliding into plane j -- this candidate slide is
					// not viable. Bail the j-loop and try the next i.
					break
				}
			}
			if j == numplanes {
				// candidate slide satisfies every other plane.
				break
			}
		}

		// With numplanes == 1 (the only structurally reachable
		// numplanes value here -- same proof as the dropped
		// MAX_CLIP_PLANES branch above: the "Fraction > 0" reset
		// guarantees numplanes is reset before another append), the
		// inner j-loop is empty (only j == i is visited, which is
		// skipped). So `i == numplanes` is impossible -- there is
		// always a valid slide. The C upstream's "no plane gives a
		// valid slide" branches ("go along the crease" cross-product
		// for numplanes == 2 and the corner-trap zero for 3+) are
		// dropped per the bsptrace pattern of removing C-inherited
		// dead code.
		out.NewVelocity = newVel

		// Oscillation guard: if the post-slide velocity has any
		// component pointing AGAINST the primal velocity (the
		// pre-iteration starting velocity), kill it dead to avoid
		// trembling in concave corners. tyrquake: `if
		// (DotProduct(ent->v.velocity, primal_velocity) <= 0)`.
		dotPrimal := out.NewVelocity[0]*primal[0] +
			out.NewVelocity[1]*primal[1] +
			out.NewVelocity[2]*primal[2]
		if dotPrimal <= 0 {
			out.NewVelocity = [3]float32{0, 0, 0}
			return out, nil
		}
	}

	return out, nil
}
