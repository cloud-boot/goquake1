// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"

	"github.com/go-quake1/engine/mdl"
)

// ErrAliasInterpRange is returned when AliasEntityInterp.Lerp falls
// outside [0, 1].
var ErrAliasInterpRange = errors.New("render: interpolation t out of [0, 1]")

// AliasEntityInterp extends AliasEntity with the destination frame
// index + the interpolation fraction. The parent struct's FrameIdx is
// the "from" frame; FrameIdxNext is the "to" frame; Lerp is the
// blend in [0, 1] (0 = use FrameIdx, 1 = use FrameIdxNext).
type AliasEntityInterp struct {
	AliasEntity          // Origin/AngleYaw/.../FrameIdx (the "from" frame) / SkinIdx
	FrameIdxNext int     // destination frame
	Lerp         float32 // [0, 1] -- 0 = use FrameIdx; 1 = use FrameIdxNext
}

// DrawAliasInterp is DrawAlias with per-vertex linear interpolation
// between two frames. Equivalent to DrawAlias when Lerp == 0 or
// FrameIdxNext == FrameIdx.
//
// Algorithm (mirrors tyrquake R_AliasBlendPoseVerts in r_alias.c):
//  1. Decode both frame's vertices via the existing framePose helper.
//  2. Lerp each vertex in BYTE space:
//     lerped[i].V[k] = round(poseA[i].V[k]*(1-t) + poseB[i].V[k]*t)
//     (Scale + ScaleOrigin are affine, so byte-space lerp produces
//     the same world-space result as a world-space lerp.)
//  3. Pass the lerped vertex array through the shared
//     drawAliasFromPose helper -- same per-triangle workflow as
//     DrawAlias.
//
// Returns ErrAliasInterpRange when Lerp falls outside [0, 1]; the
// usual nil/frame sentinels (ErrAliasNilFB / NilModel / NilRefDef /
// NilSkin / BadFrame for either FrameIdx OR FrameIdxNext) otherwise;
// or any FillTexturedPolygon error.
func DrawAliasInterp(fb *FrameBuffer, rd *RefDef, cm *ColorMap, lightLevel int,
	model *mdl.Model, skin *Pic, ent AliasEntityInterp) error {
	if fb == nil {
		return ErrAliasNilFB
	}
	if model == nil {
		return ErrAliasNilModel
	}
	if rd == nil {
		return ErrAliasNilRefDef
	}
	if skin == nil {
		return ErrAliasNilSkin
	}
	if ent.Lerp < 0 || ent.Lerp > 1 {
		return ErrAliasInterpRange
	}
	if ent.FrameIdx < 0 || ent.FrameIdx >= len(model.Frames) {
		return ErrAliasBadFrame
	}
	if ent.FrameIdxNext < 0 || ent.FrameIdxNext >= len(model.Frames) {
		return ErrAliasBadFrame
	}

	vertsA := framePose(model.Frames[ent.FrameIdx])
	if ent.Lerp == 0 || ent.FrameIdxNext == ent.FrameIdx {
		return drawAliasFromPose(fb, rd, cm, lightLevel, model, skin, ent.AliasEntity, vertsA)
	}
	vertsB := framePose(model.Frames[ent.FrameIdxNext])
	if ent.Lerp == 1 {
		return drawAliasFromPose(fb, rd, cm, lightLevel, model, skin, ent.AliasEntity, vertsB)
	}

	verts := lerpAliasPose(vertsA, vertsB, ent.Lerp)
	return drawAliasFromPose(fb, rd, cm, lightLevel, model, skin, ent.AliasEntity, verts)
}

// lerpAliasPose builds the byte-space linear interpolation of two
// per-frame vertex arrays. Mirrors R_AliasBlendPoseVerts: blend in
// byte space with float weights, round to nearest. The shorter input
// bounds the output -- a degenerate pair (one empty) yields an empty
// slice, and the caller's triangle loop iterates zero work.
//
// The lightnormalindex is taken from whichever pose carries more
// weight (a == lerp < 0.5, b otherwise), matching tyrquake.
func lerpAliasPose(a, b []mdl.TriVertx, t float32) []mdl.TriVertx {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]mdl.TriVertx, n)
	w0 := 1 - t
	w1 := t
	for i := 0; i < n; i++ {
		out[i].V[0] = byte(float32(a[i].V[0])*w0 + float32(b[i].V[0])*w1 + 0.5)
		out[i].V[1] = byte(float32(a[i].V[1])*w0 + float32(b[i].V[1])*w1 + 0.5)
		out[i].V[2] = byte(float32(a[i].V[2])*w0 + float32(b[i].V[2])*w1 + 0.5)
		if t < 0.5 {
			out[i].LightNormalIndex = a[i].LightNormalIndex
		} else {
			out[i].LightNormalIndex = b[i].LightNormalIndex
		}
	}
	return out
}
