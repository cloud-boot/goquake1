// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender

import (
	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/render"
)

// NewMarkContext wires a loaded [model.BrushModel] into a [MarkContext]
// the [MarkVisibleLeaves] walk can consume. Each closure forwards to
// the matching accessor on bm; the 1-based [VisLeafIdx] indices the
// vis primitives use map directly onto the 1-based leaf accessors on
// [model.BrushModel] (which keep index 0 as the outside-the-map
// sentinel, matching tyrquake's mleaf_t array layout).
//
// Caller contract:
//
//   - bm must be non-nil and fully loaded (built via [model.LoadBrush]).
//     A nil bm would panic on the first closure call; callers should
//     gate vis updates on a non-nil world model upstream (typically
//     the per-frame `if cl.worldmodel == nil` check the host already
//     applies).
//
//   - bm's lifetime must outlive the returned [MarkContext]; the
//     closures hold a reference to it.
//
// tyrquake equivalent: there is no dedicated constructor -- the C
// code passes `cl.worldmodel` directly into R_MarkLeaves, which then
// reaches into worldmodel->leafs[] and ->nodes[] in-line. The Go
// split is here because [MarkContext] is intentionally model-agnostic
// (lets us run the synthetic-BSP unit tests in this package without
// pulling in engine/model).
func NewMarkContext(bm *model.BrushModel) MarkContext {
	return MarkContext{
		NumLeaves: bm.NumLeaves(),
		PVSForLeaf: func(leafIdx VisLeafIdx) []byte {
			return bm.PVSForLeaf(int(leafIdx))
		},
		LeafParentNode: func(leafIdx VisLeafIdx) int {
			return bm.LeafParentNode(int(leafIdx))
		},
		NodeParent: func(nodeIdx int) int {
			return bm.NodeParent(nodeIdx)
		},
		GetNodeVisFrame: func(nodeIdx int) FrameMarkSequence {
			return FrameMarkSequence(bm.GetNodeVisFrame(nodeIdx))
		},
		SetLeafVisFrame: func(leafIdx VisLeafIdx, frame FrameMarkSequence) {
			bm.SetLeafVisFrame(int(leafIdx), int32(frame))
		},
		SetNodeVisFrame: func(nodeIdx int, frame FrameMarkSequence) {
			bm.SetNodeVisFrame(nodeIdx, int32(frame))
		},
	}
}

// NewWalkContext wires a loaded [model.BrushModel] into a [WalkContext]
// the [WalkWorld] traversal can consume. The unified node+leaf id space
// follows the convention also used by the recurse package's test
// fixtures: ids 0..NumNodes-1 are interior nodes; ids
// NumNodes..NumNodes+TotalLeaves-1 are leaves (where TotalLeaves
// includes the outside-the-map sentinel leaf 0).
//
// Closure semantics:
//
//   - NodeKind: leaf 0 (the outside-the-map sentinel) and any leaf with
//     a SOLID contents tag are classified [NodeKindEmpty]; all other
//     leaves are [NodeKindLeaf]. The walker still calls LeafFaces on
//     a NodeKindLeaf with no surfaces, which simply contributes
//     nothing — matching tyrquake's R_RenderFace no-op on empty leaves.
//
//   - NodeChildren: decodes the on-disk negative-NOT trick. Children >= 0
//     stay as node ids; Children < 0 become leaf ids via
//     `NumNodes + ^raw`.
//
//   - NodePlane: read from the file's Planes lump. If the lump is
//     missing / read fails, an identity zero-plane is returned (the
//     walker still works -- both sides of a zero-distance zero-normal
//     plane are "front", so visits proceed in the natural order).
//
//   - NodeBBox: converts the on-disk int16 bounds to float32. tyrquake
//     stores the same int16 bounds + casts at runtime.
//
//   - NodeVisFrame / LeafVisFrame: read straight from the in-memory
//     Node / Leaf wrappers.
//
//   - LeafFaces: walks the leaf's marksurfaces span and resolves each
//     entry to a face index. Returns an empty slice on out-of-range
//     leaves, leaves with no marksurfaces, or marksurfaces-lump read
//     errors (synthetic BSPs that don't ship a marksurfaces lump fall
//     into the empty-slice path naturally).
//
// Caller contract: same as [NewMarkContext] — bm must be non-nil + fully
// loaded, and its lifetime must outlive the returned [WalkContext].
//
// tyrquake equivalent: the implicit closures formed by R_RecursiveWorldNode
// reaching into cl.worldmodel->nodes / ->leafs / ->planes / ->marksurfaces.
func NewWalkContext(bm *model.BrushModel) WalkContext {
	numNodes := bm.NumNodes()
	totalLeaves := bm.TotalLeaves()
	// Planes decoded once at construction. LoadBrush already validated
	// the planes lump (it'd have errored before producing bm), so we
	// trust the decode; a failed lump lookup here would surface as a
	// nil slice and NodePlane returns the zero plane on out-of-range.
	planes, _ := bm.File.Planes()
	// Per-leaf face resolution cache. The walker hits each visible
	// leaf once per frame, but the slice computation involves a lump
	// decode; cache the per-leaf result up-front so the per-frame walk
	// is alloc-free.
	leafFaces := make([][]int, totalLeaves)
	for i := 0; i < totalLeaves; i++ {
		faces, ferr := bm.LeafFaceIndices(i)
		if ferr == nil {
			leafFaces[i] = faces
		}
	}

	return WalkContext{
		NumNodes:  numNodes,
		NumLeaves: bm.NumLeaves(),
		NodeKind: func(id int) NodeKind {
			if id < numNodes {
				return NodeKindInterior
			}
			leafIdx := id - numNodes
			if leafIdx <= 0 || leafIdx >= totalLeaves {
				return NodeKindEmpty
			}
			if bm.Leaf(leafIdx).Contents == bspfile.ContentsSolid {
				return NodeKindEmpty
			}
			return NodeKindLeaf
		},
		NodeChildren: func(id int) [2]int {
			n := bm.Node(id)
			var out [2]int
			for j := 0; j < 2; j++ {
				raw := n.Children[j]
				if raw >= 0 {
					out[j] = int(raw)
					continue
				}
				out[j] = numNodes + int(^raw)
			}
			return out
		},
		NodePlane: func(id int) render.Plane {
			n := bm.Node(id)
			pn := int(n.PlaneNum)
			if pn < 0 || pn >= len(planes) {
				return render.Plane{}
			}
			p := planes[pn]
			return render.Plane{Normal: p.Normal, Dist: p.Dist}
		},
		NodeBBox: func(id int) (mins, maxs [3]float32) {
			n := bm.Node(id)
			for j := 0; j < 3; j++ {
				mins[j] = float32(n.Mins[j])
				maxs[j] = float32(n.Maxs[j])
			}
			return mins, maxs
		},
		NodeVisFrame: func(id int) int32 {
			return bm.Node(id).VisFrame
		},
		LeafVisFrame: func(id int) int32 {
			return bm.Leaf(id - numNodes).VisFrame
		},
		LeafFaces: func(id int) []int {
			leafIdx := id - numNodes
			if leafIdx < 0 || leafIdx >= len(leafFaces) {
				return nil
			}
			return leafFaces[leafIdx]
		},
	}
}
