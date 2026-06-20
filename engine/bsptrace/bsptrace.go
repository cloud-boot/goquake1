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

// Trace is the swept-trace result. Mirrors tyrquake's trace_t.
//   AllSolid   -- true when the start AND end are both in solid
//                 space (no impact plane is valid).
//   StartSolid -- true when the start point was already in solid.
//   InOpen     -- the trace passed through CONTENTS_EMPTY at some point.
//   InWater    -- the trace passed through a liquid (water/slime/lava).
//   Fraction   -- 1.0 = the trace completed without hitting anything;
//                 < 1.0 = fraction of the (p2-p1) span at which the
//                 first impact occurred.
//   EndPos     -- the final position (= p1 + Fraction*(p2-p1) for an
//                 impact; = p2 for a clean trace).
//   Plane      -- the surface plane at the impact point (Normal +
//                 Dist). Only meaningful when AllSolid is false AND
//                 Fraction < 1.0.
type Trace struct {
	AllSolid   bool
	StartSolid bool
	InOpen     bool
	InWater    bool
	Fraction   float32
	EndPos     [3]float32
	Plane      bspfile.Plane
}

// DefaultTrace returns a Trace pre-initialised the way tyrquake's
// SV_DefaultTrace does -- AllSolid = true, Fraction = 1.0 -- so a
// follow-up TraceHull call can update fields in place. tyrquake:
// SV_DefaultTrace (the static inline in world.h).
func DefaultTrace() Trace {
	return Trace{AllSolid: true, Fraction: 1.0}
}

// TraceHull traces a line segment from p1 to p2 through hull
// starting at nodenum, populating trace with the first-impact data
// (or the clean trace when no impact occurred). Returns true iff
// the trace completed without entering solid (i.e. ended in the
// configured space). tyrquake: Mod_TraceHull.
//
// trace MUST be pre-initialised (typically via DefaultTrace) so the
// AllSolid / Fraction defaults are sane on no-impact + start-in-
// solid paths.
func TraceHull(hull *Hull, nodenum int32, p1, p2 [3]float32, trace *Trace) (bool, error) {
	if hull == nil {
		return false, errors.New("bsptrace: nil hull")
	}
	if trace == nil {
		return false, errors.New("bsptrace: nil trace")
	}
	return traceHullR(hull, nodenum, 0, 1, p1, p2, trace)
}

func traceHullR(hull *Hull, nodenum int32, p1f, p2f float32, p1, p2 [3]float32, trace *Trace) (bool, error) {
	// Leaf: classify the contents tag + return.
	if nodenum < 0 {
		if nodenum != bspfile.ContentsSolid {
			trace.AllSolid = false
			if nodenum == bspfile.ContentsEmpty {
				trace.InOpen = true
			} else {
				trace.InWater = true
			}
		} else {
			trace.StartSolid = true
		}
		return true, nil
	}

	if nodenum < hull.FirstClipNode || nodenum > hull.LastClipNode {
		return false, ErrBadNodeIndex
	}
	if int(nodenum) >= len(hull.ClipNodes) {
		return false, ErrBadNodeIndex
	}
	node := &hull.ClipNodes[nodenum]
	if node.PlaneNum < 0 || int(node.PlaneNum) >= len(hull.Planes) {
		return false, ErrBadPlaneIndex
	}
	plane := &hull.Planes[node.PlaneNum]

	var dist1, dist2 float32
	if plane.Type < 3 {
		dist1 = p1[plane.Type] - plane.Dist
		dist2 = p2[plane.Type] - plane.Dist
	} else {
		dist1 = plane.Normal[0]*p1[0] + plane.Normal[1]*p1[1] + plane.Normal[2]*p1[2] - plane.Dist
		dist2 = plane.Normal[0]*p2[0] + plane.Normal[1]*p2[1] + plane.Normal[2]*p2[2] - plane.Dist
	}

	// Both endpoints on the +side: descend the +child.
	if dist1 >= 0 && dist2 >= 0 {
		return traceHullR(hull, int32(node.Children[0]), p1f, p2f, p1, p2, trace)
	}
	// Both endpoints on the -side: descend the -child.
	if dist1 < 0 && dist2 < 0 {
		return traceHullR(hull, int32(node.Children[1]), p1f, p2f, p1, p2, trace)
	}

	// Straddles the plane -- find the crossing point biased
	// DistEpsilon onto the near side.
	var frac float32
	if dist1 < 0 {
		frac = (dist1 + DistEpsilon) / (dist1 - dist2)
	} else {
		frac = (dist1 - DistEpsilon) / (dist1 - dist2)
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}

	midf := p1f + (p2f-p1f)*frac
	mid := [3]float32{
		p1[0] + frac*(p2[0]-p1[0]),
		p1[1] + frac*(p2[1]-p1[1]),
		p1[2] + frac*(p2[2]-p1[2]),
	}

	side := 0
	if dist1 < 0 {
		side = 1
	}

	// Move up to the node along the near side.
	near := int32(node.Children[side])
	ok, err := traceHullR(hull, near, p1f, midf, p1, mid, trace)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	// Check the far side: if it's also empty, recurse onward.
	far := int32(node.Children[side^1])
	farContents, err := HullPointContents(hull, far, mid)
	if err != nil {
		return false, err
	}
	if farContents != bspfile.ContentsSolid {
		return traceHullR(hull, far, midf, p2f, mid, p2, trace)
	}

	// Far side is solid; the near-to-far transition is the impact.
	if trace.AllSolid {
		return false, nil
	}

	if side == 0 {
		trace.Plane.Normal = plane.Normal
		trace.Plane.Dist = plane.Dist
	} else {
		trace.Plane.Normal = [3]float32{-plane.Normal[0], -plane.Normal[1], -plane.Normal[2]}
		trace.Plane.Dist = -plane.Dist
	}

	// Back off from the impact until we're out of solid (the
	// "shouldn't happen but does" loop from upstream).
	contents, err := HullPointContents(hull, hull.FirstClipNode, mid)
	if err != nil {
		return false, err
	}
	for contents == bspfile.ContentsSolid {
		frac -= 0.1
		if frac < 0 {
			trace.Fraction = midf
			trace.EndPos = mid
			return false, nil
		}
		midf = p1f + (p2f-p1f)*frac
		mid = [3]float32{
			p1[0] + frac*(p2[0]-p1[0]),
			p1[1] + frac*(p2[1]-p1[1]),
			p1[2] + frac*(p2[2]-p1[2]),
		}
		contents, err = HullPointContents(hull, hull.FirstClipNode, mid)
		if err != nil {
			return false, err
		}
	}

	trace.Fraction = midf
	trace.EndPos = mid
	return false, nil
}

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
