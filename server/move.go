// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import "math"

// StopEpsilon is the magnitude below which post-clip velocity
// components are snapped to zero -- prevents trembling along a
// wall when the residual sliding velocity is sub-pixel.
// tyrquake: STOP_EPSILON in common/sv_phys.c.
const StopEpsilon = 0.1

// MoveBlocked bits describe what direction(s) a clip prevented
// movement in. Set by [ClipVelocity] from the wall normal. The
// SV_FlyMove iteration consumes these to decide whether the
// entity is on the floor / stepped up / hit a moving brush.
// tyrquake: the integer bits SV_ClipVelocity returns.
type MoveBlocked int

const (
	// BlockedFloor flags a clip against a surface whose normal has
	// a positive z component -- i.e. a floor or upward-facing slope.
	// tyrquake: the `blocked |= 1` branch in SV_ClipVelocity.
	BlockedFloor MoveBlocked = 1

	// BlockedStep flags a clip against a surface whose normal has
	// exactly zero z component -- i.e. a vertical wall or step face.
	// tyrquake: the `blocked |= 2` branch in SV_ClipVelocity
	// (literal `!normal[2]`, so any nonzero z misses this bit,
	// including small negative values from downward-facing slopes).
	BlockedStep MoveBlocked = 2
)

// ClipVelocity reflects velocity off a wall whose outward normal
// is normal, with an overbounce factor of overbounce (1.0 = stop
// against wall; > 1.0 = bounce; < 1.0 = absorb). Returns the
// post-clip velocity + a bitmask of what was hit (BlockedFloor
// and/or BlockedStep).
//
// tyrquake: int SV_ClipVelocity(in, normal, out, overbounce) in
// common/sv_phys.c.
//
// The C upstream uses the literal `!normal[2]` for the step bit,
// so a downward-facing slope (normal[2] < 0) sets neither floor
// nor step -- the Go port preserves that bit-for-bit.
func ClipVelocity(velocity, normal [3]float32, overbounce float32) ([3]float32, MoveBlocked) {
	var blocked MoveBlocked
	if normal[2] > 0 {
		blocked |= BlockedFloor
	}
	if normal[2] == 0 {
		blocked |= BlockedStep
	}

	backoff := (velocity[0]*normal[0] + velocity[1]*normal[1] + velocity[2]*normal[2]) * overbounce

	var out [3]float32
	for i := 0; i < 3; i++ {
		change := normal[i] * backoff
		out[i] = velocity[i] - change
		if out[i] > -StopEpsilon && out[i] < StopEpsilon {
			out[i] = 0
		}
	}
	return out, blocked
}

// WallFriction reduces velocity by the component perpendicular to
// the wall normal (i.e. the part pushing INTO the wall). Used by
// SV_UserFriction when the player is brushing against a vertical
// surface to keep wallrunning slow. tyrquake: SV_WallFriction in
// NQ/sv_user.c (NOT in common/sv_phys.c despite the helper's
// generality -- the only caller is the user-input integrator).
//
// The early-return when the unit-velocity dot is non-positive
// covers both "already moving away from the wall" (dot < 0) and
// "zero velocity" (the normalize step produces a zero vector,
// dot == 0). dt == 0 or friction == 0 also produces no change
// because the scale factor collapses to zero -- but the formula
// still runs unconditionally past the dot-sign guard.
func WallFriction(velocity, normal [3]float32, friction, dt float32) [3]float32 {
	speed := float32(math.Sqrt(float64(velocity[0]*velocity[0] + velocity[1]*velocity[1] + velocity[2]*velocity[2])))
	if speed == 0 {
		return velocity
	}
	invSpeed := 1 / speed
	d := velocity[0]*normal[0]*invSpeed + velocity[1]*normal[1]*invSpeed + velocity[2]*normal[2]*invSpeed
	if d <= 0 {
		return velocity
	}

	scale := d * dt * friction
	return [3]float32{
		velocity[0] - normal[0]*scale,
		velocity[1] - normal[1]*scale,
		velocity[2] - normal[2]*scale,
	}
}
