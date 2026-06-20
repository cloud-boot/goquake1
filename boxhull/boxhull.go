// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package boxhull

import (
	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
)

// boxClipNodes is the fixed BSP tree shape for an axis-aligned box
// -- 6 internal nodes, each branching to either CONTENTS_EMPTY (the
// outside of one face) or the next face's node, with the deepest
// leaf classifying the inside as CONTENTS_SOLID. tyrquake:
// box_clipnodes[6].
var boxClipNodes = [6]bspfile.ClipNode{
	{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, 1}},
	{PlaneNum: 1, Children: [2]int16{2, bspfile.ContentsEmpty}},
	{PlaneNum: 2, Children: [2]int16{bspfile.ContentsEmpty, 3}},
	{PlaneNum: 3, Children: [2]int16{4, bspfile.ContentsEmpty}},
	{PlaneNum: 4, Children: [2]int16{bspfile.ContentsEmpty, 5}},
	{PlaneNum: 5, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
}

// boxPlaneTemplate is the fixed (normal, type) pair for each box
// face; only the .Dist value varies per-box (= the face's
// coordinate on its axis). tyrquake: boxhull_template.planes.
var boxPlaneTemplate = [6]bspfile.Plane{
	{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX},
	{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX},
	{Normal: [3]float32{0, 1, 0}, Type: bspfile.PlaneY},
	{Normal: [3]float32{0, 1, 0}, Type: bspfile.PlaneY},
	{Normal: [3]float32{0, 0, 1}, Type: bspfile.PlaneZ},
	{Normal: [3]float32{0, 0, 1}, Type: bspfile.PlaneZ},
}

// Create returns a [bsptrace.Hull] whose 6 planes are the faces of
// the axis-aligned bounding box defined by mins and maxs. The
// result is ready to trace against via [bsptrace.TraceHull] with
// nodenum=0; the entry node 0 dispatches +X then -X then +Y then
// -Y then +Z then -Z so a point lands on CONTENTS_SOLID exactly
// when it is inside the box on all three axes.
//
// tyrquake: Mod_CreateBoxhull. The C upstream wraps the result in a
// caller-owned boxhull_t (planes array + hull header); the Go port
// heap-allocates the per-call planes slice instead, so the returned
// Hull is a self-contained value.
func Create(mins, maxs [3]float32) bsptrace.Hull {
	planes := make([]bspfile.Plane, len(boxPlaneTemplate))
	copy(planes, boxPlaneTemplate[:])
	planes[0].Dist = maxs[0]
	planes[1].Dist = mins[0]
	planes[2].Dist = maxs[1]
	planes[3].Dist = mins[1]
	planes[4].Dist = maxs[2]
	planes[5].Dist = mins[2]
	return bsptrace.Hull{
		ClipNodes:     boxClipNodes[:],
		Planes:        planes,
		FirstClipNode: 0,
		LastClipNode:  5,
	}
}
