// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender

import "github.com/go-quake1/engine/model"

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
