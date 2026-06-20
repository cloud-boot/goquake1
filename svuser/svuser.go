// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package svuser

import (
	"math"

	"github.com/go-quake1/engine/mathlib"
)

// CalcWishVel returns the world-space wishvel for one client input
// frame: project the player's forward/side/up move scalars through
// the world-relative forward/right basis that AngleVectors derives
// from viewangles. The Z component is the raw upMove scalar (the
// caller decides whether to zero it for MOVETYPE_WALK or layer the
// SV_WaterMove drift on top).
//
// tyrquake: the
//
//	AngleVectors(angles, forward, right, up)
//	for (i = 0; i < 3; i++)
//	    wishvel[i] = forward[i] * fmove + right[i] * smove;
//	wishvel[2] = upmove;
//
// block at the top of SV_AirMove / SV_WaterMove in NQ/sv_user.c.
func CalcWishVel(forwardMove, sideMove, upMove float32, viewangles [3]float32) [3]float32 {
	forward, right, _ := mathlib.AngleVectors(viewangles)
	return [3]float32{
		forward[0]*forwardMove + right[0]*sideMove,
		forward[1]*forwardMove + right[1]*sideMove,
		forward[2]*forwardMove + right[2]*sideMove + upMove,
	}
}

// SplitWishVel decomposes wishvel into a unit direction and its
// scalar magnitude. Zero input returns (zero vector, 0) -- the
// caller MUST check wishspeed before dividing by it.
//
// tyrquake: the inline
//
//	VectorCopy(wishvel, wishdir);
//	wishspeed = VectorNormalize(wishdir);
//
// pair in SV_AirMove. VectorNormalize itself short-circuits to a
// zero output when length is zero; we preserve that.
func SplitWishVel(wishvel [3]float32) (wishdir [3]float32, wishspeed float32) {
	length := float32(math.Sqrt(float64(
		wishvel[0]*wishvel[0] +
			wishvel[1]*wishvel[1] +
			wishvel[2]*wishvel[2])))
	if length == 0 {
		return [3]float32{0, 0, 0}, 0
	}
	inv := 1.0 / length
	return [3]float32{wishvel[0] * inv, wishvel[1] * inv, wishvel[2] * inv}, length
}

// ClampWishSpeed returns wishvel scaled so its magnitude is at most
// maxSpeed. Inputs already at or below the cap pass through bit-
// identical (no scale, no recomputation of the length). Zero input
// is a no-op.
//
// tyrquake: the
//
//	if (wishspeed > sv_maxspeed.value) {
//	    VectorScale(wishvel, sv_maxspeed.value / wishspeed, wishvel);
//	    wishspeed = sv_maxspeed.value;
//	}
//
// branch inlined in SV_AirMove and SV_WaterMove.
func ClampWishSpeed(wishvel [3]float32, maxSpeed float32) [3]float32 {
	speed := float32(math.Sqrt(float64(
		wishvel[0]*wishvel[0] +
			wishvel[1]*wishvel[1] +
			wishvel[2]*wishvel[2])))
	if speed <= maxSpeed {
		return wishvel
	}
	scale := maxSpeed / speed
	return [3]float32{wishvel[0] * scale, wishvel[1] * scale, wishvel[2] * scale}
}

// Accelerate returns v after one ground-acceleration step toward
// wishdir at wishspeed. The accel step is capped at (wishspeed -
// currentSpeedAlongWishdir) so a single tick can never overshoot
// the wishspeed projection: if the player's velocity along wishdir
// already meets or exceeds wishspeed the function returns v
// unchanged (no negative acceleration -- the integrator only ever
// pushes toward wishdir, never away).
//
// tyrquake: SV_Accelerate in NQ/sv_user.c. The cvar sv_accelerate
// and the per-frame host_frametime are passed in as plain arguments
// (accelerate, dt) so this package stays state-free.
func Accelerate(v, wishdir [3]float32, wishspeed, accelerate, dt float32) [3]float32 {
	currentspeed := v[0]*wishdir[0] + v[1]*wishdir[1] + v[2]*wishdir[2]
	addspeed := wishspeed - currentspeed
	if addspeed <= 0 {
		return v
	}
	accelspeed := accelerate * dt * wishspeed
	if accelspeed > addspeed {
		accelspeed = addspeed
	}
	return [3]float32{
		v[0] + accelspeed*wishdir[0],
		v[1] + accelspeed*wishdir[1],
		v[2] + accelspeed*wishdir[2],
	}
}
