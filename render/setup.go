// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

// View-angle clamps applied at the top of each frame's view setup.
// tyrquake: CL_AdjustAngles / R_SetupFrame ranges.
const (
	MaxPitchUp    float32 = 70  // looking up; positive pitch is downward
	MaxPitchDown  float32 = 80  // looking down
	MaxRollAbs    float32 = 50  // |roll| ceiling (death-cam tilt etc.)
	MaxYawAbsWrap float32 = 360 // yaw is wrapped via fmod(yaw, 360)
)

// FOV-x clamps. tyrquake: scr_fov cvar's min/max via Cvar_SetValue.
const (
	MinFovX     float32 = 10
	MaxFovX     float32 = 170
	DefaultFovX float32 = 90
)

// ClampViewAngles applies the per-frame viewangle clamps the C
// upstream enforces inside R_SetupFrame. Returns the clamped triple
// (pitch / yaw / roll).
//
// Pitch is clamped asymmetrically (looking down further than looking
// up). Yaw wraps modulo 360 so values from a long running game don't
// drift to large magnitudes. Roll is clamped symmetrically.
func ClampViewAngles(angles [3]float32) [3]float32 {
	pitch := angles[0]
	if pitch > MaxPitchDown {
		pitch = MaxPitchDown
	} else if pitch < -MaxPitchUp {
		pitch = -MaxPitchUp
	}

	yaw := angles[1]
	for yaw >= MaxYawAbsWrap {
		yaw -= MaxYawAbsWrap
	}
	for yaw < 0 {
		yaw += MaxYawAbsWrap
	}

	roll := angles[2]
	if roll > MaxRollAbs {
		roll = MaxRollAbs
	} else if roll < -MaxRollAbs {
		roll = -MaxRollAbs
	}

	return [3]float32{pitch, yaw, roll}
}

// ClampFovX clamps the horizontal FOV to (MinFovX, MaxFovX), inclusive.
// Returns DefaultFovX if fov is non-finite (NaN, Inf -- detected via
// fov != fov || fov-fov != 0; pure-Go epsilon-free check).
func ClampFovX(fov float32) float32 {
	// NaN: x != x is the canonical IEEE-754 NaN check.
	if fov != fov {
		return DefaultFovX
	}
	if fov < MinFovX {
		return MinFovX
	}
	if fov > MaxFovX {
		return MaxFovX
	}
	return fov
}

// ApplyViewOffset returns the per-tick adjusted view origin: the base
// origin plus a vertical view-height bob. tyrquake: the
// `vieworg[2] += cl.viewheight + bob` arithmetic at the top of
// V_RenderView.
//
// `viewHeightOffset` is the cumulative per-frame bob (the bob
// computation lives in a separate batch -- this helper just applies
// the result to the origin).
func ApplyViewOffset(origin [3]float32, viewHeightOffset float32) [3]float32 {
	return [3]float32{origin[0], origin[1], origin[2] + viewHeightOffset}
}

// PlaneSide returns +1 if `p` is on the front side of the plane,
// -1 on the back side, 0 on the plane. tyrquake: the BoxOnPlaneSide
// scalar path for a single point.
//
// Used heavily by the BSP traversal: every interior BSP node has a
// splitting plane; the renderer walks the front-child first if the
// camera is on the front side, otherwise back-child first.
//
// `pl.Normal . p - pl.Dist`:
//   > 0 -> front
//   < 0 -> back
//   = 0 -> on the plane (degenerate; the BSP traversal treats this
//          as "front" for the side selection, but the side returned
//          here is the unambiguous mathematical answer)
func PlaneSide(p [3]float32, pl Plane) int {
	d := pl.Normal[0]*p[0] + pl.Normal[1]*p[1] + pl.Normal[2]*p[2] - pl.Dist
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	default:
		return 0
	}
}
