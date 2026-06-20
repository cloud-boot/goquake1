// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package svuser

// onEpsilon mirrors the C ON_EPSILON (NQ/quakedef.h:64). SV_SetIdealPitch
// uses it as the per-step "is this height change noise?" threshold.
const onEpsilon = 0.1

// punchDecayRate is the per-second per-axis decay magnitude the C
// DropPunchAngle (NQ/sv_user.c) shrinks the punch angle by. The
// upstream literal is `10 * host_frametime`.
const punchDecayRate = 10.0

// DropPunchAngle decays the player's punch angle toward zero by
// dt seconds at the upstream rate (10 units per second per axis,
// capped so it never crosses zero). tyrquake: SV_DropPunchAngle in
// NQ/sv_user.c.
//
// Reads the current punch vector + dt, returns the decayed vector.
// The decay is per-axis linear: each axis shrinks by min(|axis|,
// 10*dt) in magnitude toward zero. If the axis is < 10*dt in
// magnitude it lands at exactly 0 (no sign flip).
//
// Deviation from C: the upstream decays the vector *magnitude* via
// VectorNormalize-then-rescale, so off-axis components shrink in
// proportion to the largest axis. The Go port decays each axis
// independently. The two agree for single-axis input (the only
// shape actual gameplay produces, since punchangle is only ever
// nudged on the pitch axis by T_Damage in the QC) but diverge for
// synthetic multi-axis input.
//
// The C upstream couples this with the player edict's
// entvars.punchangle write; the Go port returns the new vector
// so callers store it back into the edict explicitly.
func DropPunchAngle(punch [3]float32, dt float32) [3]float32 {
	step := float32(punchDecayRate) * dt
	var out [3]float32
	for i := 0; i < 3; i++ {
		v := punch[i]
		if v > 0 {
			if v <= step {
				out[i] = 0
			} else {
				out[i] = v - step
			}
		} else if v < 0 {
			if -v <= step {
				out[i] = 0
			} else {
				out[i] = v + step
			}
		}
	}
	return out
}

// PitchSampler is the callback IdealPitch uses to query the
// world geometry: given a forward-axis offset from the player's
// origin (in world units), return the floor height at that point
// and whether the trace hit. SV_SetIdealPitch in the C upstream
// uses SV_TraceLine straight down from
// (origin + cos(yaw)*offset, origin + sin(yaw)*offset,
// origin.z + view_ofs.z) to .z - 160; the Go port abstracts that
// out so callers can plug in any heights-fetcher (real-trace,
// stub, mock) without dragging the trace package into svuser.
//
// The hit flag mirrors the C's "trace.allsolid OR trace.fraction
// == 1" early-out: any miss (looking at a wall, or at a dropoff)
// aborts the whole computation and IdealPitch returns 0.
type PitchSampler func(forwardOffset float32) (floorZ float32, hit bool)

// maxForward is the C MAX_FORWARD: SV_SetIdealPitch fires this
// many downward traces in front of the player.
const maxForward = 6

// IdealPitch computes the auto-look-at-floor angle the player's
// HUD should face when walking on uneven terrain. tyrquake:
// SV_SetIdealPitch in NQ/sv_user.c.
//
// The algorithm samples the floor at 6 fixed forward distances
// (36, 48, 60, 72, 84, 96 units -- the C `(i+3)*12` for i in
// 0..MAX_FORWARD-1), notes their height changes, and returns the
// last consistent step direction (in units) scaled by
// idealPitchScale. The upstream formula is:
//
//	idealpitch = -dir * sv_idealpitchscale.value
//
// where dir is the signed per-step height change in units. There
// is no atan2 -- the upstream treats the unit-valued step as a
// degree-valued pitch directly (small-angle approximation since
// each step is ~12 forward units).
//
// Returns 0 in any of the C's "leave the current ideal alone"
// cases: any sampler miss (wall or dropoff), perfectly flat floor
// (all steps under onEpsilon), mixed step directions, or fewer
// than 2 non-flat steps. This collapses the C's "early return
// without writing" into "return 0" so the caller can unconditionally
// assign the result; the upstream's separate "don't touch" vs
// "explicitly zero" branches are not distinguishable at the Go
// API surface.
//
// The 6 samples are taken via the supplied PitchSampler; the
// caller is responsible for the TraceLine-equivalent that maps
// forwardOffset -> (floorZ, hit). idealPitchScale is the
// sv_idealpitchscale cvar value (default 0.8) which scales the
// computed pitch in the final return.
func IdealPitch(sampler PitchSampler, idealPitchScale float32) float32 {
	var z [maxForward]float32
	for i := 0; i < maxForward; i++ {
		offset := float32(i+3) * 12
		floorZ, hit := sampler(offset)
		if !hit {
			return 0
		}
		z[i] = floorZ
	}

	var dir float32
	steps := 0
	for j := 1; j < maxForward; j++ {
		step := z[j] - z[j-1]
		if step > -onEpsilon && step < onEpsilon {
			continue
		}
		if dir != 0 && (step-dir > onEpsilon || step-dir < -onEpsilon) {
			return 0 // mixed step directions
		}
		steps++
		dir = step
	}

	if dir == 0 {
		return 0
	}
	if steps < 2 {
		return 0
	}
	return -dir * idealPitchScale
}
