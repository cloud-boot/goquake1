// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsptrace

import (
	"errors"

	"github.com/cloud-boot/goquake1/engine/bspfile"
)

// Hull is one collision tree -- a flat clipnodes + planes pair with
// a valid-index range + a per-side bbox offset for asymmetric
// swept-box traces. tyrquake: hull_t.
type Hull struct {
	ClipNodes      []bspfile.ClipNode
	Planes         []bspfile.Plane
	FirstClipNode  int32
	LastClipNode   int32
	ClipMins       [3]float32
	ClipMaxs       [3]float32
}

// DistEpsilon is the tyrquake "1/32 epsilon to keep floating point
// happy" used by every trace-side comparison. Exposed for the
// follow-up swept-box port + for tests that want to assert
// behaviour at the threshold.
const DistEpsilon = 0.03125

// Sentinel errors.
var (
	ErrBadNodeIndex = errors.New("bsptrace: node index outside hull's clipnode range")
	ErrBadPlaneIndex = errors.New("bsptrace: clipnode plane index outside hull's planes slice")
)

// HullPointContents walks from nodenum following plane-side decisions
// until a leaf (negative nodenum) is reached. The returned int32 is
// the CONTENTS_* tag of the addressed leaf (CONTENTS_EMPTY = -1,
// CONTENTS_SOLID = -2, etc.). tyrquake: Mod_HullPointContents (non-
// asm path).
//
// Errors surface when:
//   - nodenum starts negative (caller error; pass a real entry node)
//   - a walked child index is out of the hull's valid clipnode range
//   - a clipnode's plane index falls outside the hull's planes slice
//
// The upstream Sys_Error's on the bad-node-number case; the Go
// port surfaces ErrBadNodeIndex so callers can decide between
// panic-via-sys.Error + recover-and-skip semantics.
func HullPointContents(h *Hull, nodenum int32, point [3]float32) (int32, error) {
	if h == nil {
		return 0, errors.New("bsptrace: nil hull")
	}
	for nodenum >= 0 {
		if nodenum < h.FirstClipNode || nodenum > h.LastClipNode {
			return 0, ErrBadNodeIndex
		}
		// ClipNode indices are absolute into the hull's ClipNodes
		// slice; tyrquake's `hull->clipnodes + num` is the same.
		if int(nodenum) >= len(h.ClipNodes) {
			return 0, ErrBadNodeIndex
		}
		node := &h.ClipNodes[nodenum]
		if node.PlaneNum < 0 || int(node.PlaneNum) >= len(h.Planes) {
			return 0, ErrBadPlaneIndex
		}
		plane := &h.Planes[node.PlaneNum]
		var dist float32
		if plane.Type < 3 {
			// Axial plane fast path -- the normal is a single 1.0
			// component on the addressed axis.
			dist = point[plane.Type] - plane.Dist
		} else {
			dist = plane.Normal[0]*point[0] +
				plane.Normal[1]*point[1] +
				plane.Normal[2]*point[2] -
				plane.Dist
		}
		if dist < 0 {
			nodenum = int32(node.Children[1])
		} else {
			nodenum = int32(node.Children[0])
		}
	}
	return nodenum, nil
}
