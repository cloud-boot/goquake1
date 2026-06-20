// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package svuser is the server-side player-input math: the pure
// transforms that turn a client's per-frame UserCmd (forwardmove /
// sidemove / upmove scalars + viewangles) into the world-space
// wishvel/wishdir/wishspeed inputs the physics integrator advances
// the player velocity against.
//
// The package is deliberately state-free -- it owns no cvars, no
// edicts, no frame-time globals. The caller (server.SV_ClientThink
// once it lands in the Go port) is expected to thread sv_maxspeed
// and host_frametime through as plain arguments. That makes the
// math trivially testable and lets the integrator stage compose it
// the same way for SV_AirMove and SV_WaterMove without re-implementing
// either.
//
// Mapping vs tyrquake@6531579:
//
//   - [CalcWishVel] / [SplitWishVel] / [ClampWishSpeed] -- the
//     wishvel-construction block at the top of SV_AirMove and
//     SV_WaterMove (NQ/sv_user.c). The C inlines all three; the Go
//     port factors them apart so the water-vs-air callers can share
//     the projection-and-clamp half and differ only in the up-axis
//     handling (water drift + 0.7x speed cap) they layer on top.
//
//   - [Accelerate] -- SV_Accelerate (NQ/sv_user.c). The
//     "accel scaled by frame time, capped so a single tick can't
//     overshoot wishspeed" loop the ground integrator runs after
//     [SV_UserFriction] (forthcoming, lives in the server package).
//
// SV_UserFriction and SV_AirAccelerate are not ported here: the
// former depends on a TraceLine into the world geometry (lives in
// the world package), the latter is a near-clone of [Accelerate]
// with a 30-unit wishspeed cap and will land alongside the air-move
// integrator that needs it.
package svuser
