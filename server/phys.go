// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import "math"

// SvPhysDefaults is the default value table for the sv_* physics
// cvars the C upstream registers via SV_RegisterVariables. The
// engine reads these via the cvar system; the Go port exposes
// them as a struct callers compose freely + as drift detectors
// for the named defaults.
//
// tyrquake: NQ/sv_main.c cvar registrations (sv_maxvelocity,
// sv_gravity, sv_friction, sv_edgefriction, sv_stopspeed,
// sv_maxspeed, sv_accelerate).
type PhysParams struct {
	MaxVelocity  float32 // default 2000
	Gravity      float32 // default 800
	Friction     float32 // default 4
	EdgeFriction float32 // default 2
	StopSpeed    float32 // default 100
	MaxSpeed     float32 // default 320
	Accelerate   float32 // default 10
}

// DefaultPhysParams returns the C upstream defaults verbatim --
// callers that want tyrquake-parity behavior start with this and
// mutate as needed. tyrquake: the cvar_t literals in
// common/sv_phys.c (sv_friction "4", sv_gravity "800",
// sv_stopspeed "100", sv_maxvelocity "2000", sv_maxspeed "320",
// sv_accelerate "10") + NQ/sv_user.c (sv_edgefriction "2").
func DefaultPhysParams() PhysParams {
	return PhysParams{
		MaxVelocity:  2000,
		Gravity:      800,
		Friction:     4,
		EdgeFriction: 2,
		StopSpeed:    100,
		MaxSpeed:     320,
		Accelerate:   10,
	}
}

// ClampVelocity per-axis-clamps v to ±maxVelocity. NaN inputs
// become 0 (matches the C SV_CheckVelocity NaN guard:
// "if (IS_NAN(ent->v.velocity[i])) { Con_Printf(...); v=0; }").
// tyrquake: SV_CheckVelocity in common/sv_phys.c.
func ClampVelocity(v [3]float32, maxVelocity float32) [3]float32 {
	var out [3]float32
	for i := 0; i < 3; i++ {
		x := v[i]
		if math.IsNaN(float64(x)) {
			out[i] = 0
			continue
		}
		if x > maxVelocity {
			out[i] = maxVelocity
		} else if x < -maxVelocity {
			out[i] = -maxVelocity
		} else {
			out[i] = x
		}
	}
	return out
}

// ApplyGravity returns velocity with v[2] decremented by
// gravityFactor * gravity * dt. gravityFactor is the entity's
// own gravity multiplier (entvars.gravity, defaults to 1.0 when
// the QC doesn't set it). tyrquake: SV_AddGravity in
// common/sv_phys.c.
//
// If gravityFactor == 0, treat as 1.0 (matches the upstream's
// `val = GetEdictFieldValue(ent, "gravity"); if (!val ||
// !val->_float) scale = 1.0`).
func ApplyGravity(v [3]float32, gravityFactor, gravity, dt float32) [3]float32 {
	scale := gravityFactor
	if scale == 0 {
		scale = 1
	}
	v[2] -= scale * gravity * dt
	return v
}

// ApplyFriction returns velocity reduced per the ground-friction
// model: subtract dt * friction * speed (with a stop-speed floor)
// from velocity, clamping the magnitude reduction to current speed.
// onEdge=true uses friction*edgeFriction instead of plain friction
// (matches the SV_UserFriction "if the leading edge is over a
// dropoff, increase friction" branch).
//
// If speed <= stopSpeed, the friction factor uses stopSpeed as the
// control value (a fixed-stop ramp); else it uses speed
// (proportional). tyrquake: SV_UserFriction in NQ/sv_user.c (the
// trace-classify step that decides onEdge is the caller's
// responsibility -- this just applies the chosen friction).
func ApplyFriction(v [3]float32, params PhysParams, onEdge bool, dt float32) [3]float32 {
	// Horizontal speed only -- the C upstream computes speed from
	// velocity[0..1] (z is preserved across friction).
	speed := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1])))
	if speed == 0 {
		return v
	}

	friction := params.Friction
	if onEdge {
		friction = params.Friction * params.EdgeFriction
	}

	control := speed
	if speed < params.StopSpeed {
		control = params.StopSpeed
	}

	newspeed := speed - dt*control*friction
	if newspeed < 0 {
		newspeed = 0
	}
	// Scale velocity by newspeed/speed. Because newspeed is clamped
	// at 0 above, the ratio is in [0, 1] -- no sign flip is possible,
	// so the C upstream's implicit "magnitude reduction can't exceed
	// current speed" invariant holds verbatim.
	scale := newspeed / speed
	v[0] *= scale
	v[1] *= scale
	v[2] *= scale
	return v
}
