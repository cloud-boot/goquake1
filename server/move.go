// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import "github.com/go-quake1/engine/mathlib"

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

// WallFriction returns the post-friction velocity for a player
// brushing against a wall, matching the C upstream verbatim.
// tyrquake: SV_WallFriction in common/sv_phys.c (NOT NQ/sv_user.c
// -- the SV_ prefix and the older spec docs misplace it).
//
// Shape:
//
//	forward, _, _ = AngleVectors(viewangles)
//	d = dot(normal, forward) + 0.5
//	if d >= 0:    return velocity unchanged
//	              (player isn't facing INTO the wall enough; the
//	              wall normal points AWAY from the wall surface,
//	              so a player walking into the wall has a negative
//	              forward.normal; the +0.5 sets the activation
//	              threshold at ~120deg of facing)
//	i = dot(normal, velocity)
//	side = velocity - normal*i            (the wall-tangential part)
//	velocity[0] = side[0] * (1 + d)
//	velocity[1] = side[1] * (1 + d)
//	velocity[2] stays as-is               (C only writes [0]/[1];
//	                                       gravity component is
//	                                       preserved verbatim)
//
// The (1+d) factor is in [0, 0.5] when the early-return doesn't
// fire (d in [-1, -0.5]), so the tangential XY velocity is
// scaled DOWN -- this is the "stick to the wall" feel when the
// player runs face-first into a surface.
//
// Divergence from this port's earlier prompt-spec: the C takes
// no dt and no friction scalar; the only inputs are the player's
// facing angles, the wall normal, and the player's velocity. The
// formula is a pure geometric rescaling, not a per-tick exponential
// decay. We follow the C; the prompt's documented version was a
// generic friction shape that didn't exist upstream.
//
// Parameters:
//
//	velocity    current world velocity ([vx, vy, vz])
//	normal      outward wall normal from the collision trace
//	viewangles  player's facing angles (pitch, yaw, roll) in degrees
func WallFriction(velocity, normal, viewangles [3]float32) [3]float32 {
	forward, _, _ := mathlib.AngleVectors(mathlib.Vec3(viewangles))

	d := normal[0]*forward[0] + normal[1]*forward[1] + normal[2]*forward[2]
	d += 0.5
	if d >= 0 {
		return velocity
	}

	// cut the tangential velocity
	i := normal[0]*velocity[0] + normal[1]*velocity[1] + normal[2]*velocity[2]
	sideX := velocity[0] - normal[0]*i
	sideY := velocity[1] - normal[1]*i

	scale := 1 + d
	return [3]float32{
		sideX * scale,
		sideY * scale,
		velocity[2], // upstream leaves v[2] untouched
	}
}
