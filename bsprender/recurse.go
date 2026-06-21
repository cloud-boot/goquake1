// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender

import (
	"errors"

	"github.com/go-quake1/engine/render"
)

// SurfaceRef points to one face in the BSP. The per-frame [SurfaceList]
// is just an []SurfaceRef; the rasterizer's span builder (a separate
// batch) walks the list and emits spans per ref. tyrquake equivalent:
// the msurface_t* values chained into r_surface_chains by R_RenderFace.
type SurfaceRef struct {
	// FaceIdx indexes the model's faces lump
	// ([bspfile.File.Faces] / brushmodel's surfaces[] slice).
	FaceIdx int
	// LeafIdx is the source leaf the face was reached from. Kept so
	// the rasterizer's dynamic-light pass can re-find the leaf's
	// PVS row and the renderer's debug HUD can attribute the surface.
	LeafIdx int
	// BackfaceCull is reserved for the per-surface side test the
	// node-walk applies as it descends; today this package leaves it
	// false because back-face culling lives in the rasterizer (the
	// upstream R_RenderFace cull happens AFTER the recursive walk
	// queues the surface). The field is part of the API surface so
	// future revisions can move the cull earlier without breaking
	// callers.
	BackfaceCull bool
}

// SurfaceList is the per-frame accumulator the BSP walk fills. Cleared
// at the top of each frame via [SurfaceList.Reset]; appended to by the
// walk via [SurfaceList.Append]. tyrquake: there is no first-class
// accumulator -- R_RenderFace queues straight into r_surface_chains;
// the Go split keeps the walk pure (no rasterizer dependency) and lets
// the test suite assert append order directly.
type SurfaceList struct {
	Refs []SurfaceRef
}

// Reset clears the underlying slice while preserving capacity, so the
// same list can be reused frame to frame without re-allocating.
func (l *SurfaceList) Reset() { l.Refs = l.Refs[:0] }

// Append adds one [SurfaceRef] to the list.
func (l *SurfaceList) Append(r SurfaceRef) { l.Refs = append(l.Refs, r) }

// Len returns the number of refs accumulated so far.
func (l *SurfaceList) Len() int { return len(l.Refs) }

// NodeKind tags one child slot of a BSP node. Q1's on-disk BSP encodes
// child slots as signed int16 where >= 0 is a node index and < 0 is
// `^leaf_index` (so the leaf index is the bitwise-NOT of the value);
// the Go port surfaces the distinction via this enum so the walker can
// dispatch on it without re-encoding the negative-NOT trick. A
// [NodeKindEmpty] return lets the world-loader flag leaves that hold
// no faces (typically the outside-the-map sentinel leaf 0) so the walk
// can skip them silently.
type NodeKind int

const (
	// NodeKindInterior: the index points at an interior BSP node. The
	// walker recurses through its plane + children.
	NodeKindInterior NodeKind = 0
	// NodeKindLeaf: the index points at a leaf with faces to draw.
	NodeKindLeaf NodeKind = 1
	// NodeKindEmpty: the index points at a leaf with no faces (the
	// solid-outside sentinel, or any leaf whose marksurfaces span is
	// empty). The walker silently skips these.
	NodeKindEmpty NodeKind = 2
)

// Errors returned by [WalkWorld].
var (
	// ErrWalkNilList is returned when the caller passes a nil
	// SurfaceList. The walk has no place to deposit refs and refuses
	// to start.
	ErrWalkNilList = errors.New("bsprender: nil SurfaceList in walk")
	// ErrWalkRootRange is returned when rootIdx is outside
	// [0, NumNodes). A root index of -1 (which Q1 uses to mean "the
	// whole model is one leaf") is intentionally rejected here; the
	// caller should special-case the degenerate world separately.
	ErrWalkRootRange = errors.New("bsprender: root index out of [0, NumNodes)")
)

// WalkContext is the per-walk parameter bundle for [WalkWorld]. As with
// [MarkContext], the walk takes closures rather than a *model.BrushModel
// so this package stays decoupled from engine/model and so synthetic
// BSPs can be wired in unit tests without building a full BSP file.
//
// Closures the walk calls:
//
//   - NodeKind(idx) classifies index `idx` as an interior node, a
//     drawable leaf, or an empty leaf. The walk dispatches on the
//     result.
//   - NodeChildren(idx) returns the [front, back] child indices for an
//     interior node. Each child index is itself classified via
//     NodeKind; the dispatcher handles all three kinds.
//   - NodePlane(idx) returns the splitting plane the walker uses to
//     decide which side of the node the viewer is on.
//   - NodeBBox(idx) returns the (mins, maxs) culling AABB so the walker
//     can frustum-cull whole subtrees.
//   - NodeVisFrame(idx) returns the per-frame mark
//     [MarkVisibleLeaves] wrote; the walk skips any node whose stamp
//     != currentFrame.
//   - LeafVisFrame(idx) is the same for leaves.
//   - LeafFaces(idx) returns the FaceIdx slice for the given leaf
//     (decoded from the leaf's [FirstMarkSurface, FirstMarkSurface +
//     NumMarkSurfaces) span). An empty / nil slice is treated as
//     [NodeKindEmpty]; the walker still calls LeafFaces but appends
//     nothing.
//
// NumNodes / NumLeaves are kept for caller-side bounds checks. The
// walker uses NumNodes once to validate the root and otherwise trusts
// NodeChildren to return in-range indices (the brushmodel loader has
// already validated the on-disk children at LoadBrush time).
type WalkContext struct {
	NumNodes     int
	NumLeaves    int
	NodeKind     func(idx int) NodeKind
	NodeChildren func(idx int) [2]int
	NodePlane    func(idx int) render.Plane
	NodeBBox     func(idx int) (mins, maxs [3]float32)
	NodeVisFrame func(idx int) int32
	LeafVisFrame func(idx int) int32
	LeafFaces    func(idx int) []int
}

// WalkWorld is the Go port of tyrquake's R_RecursiveWorldNode. It walks
// the BSP rooted at rootIdx in front-to-back order from viewer's
// perspective, applies the per-frame PVS mask (NodeVisFrame /
// LeafVisFrame must equal currentFrame for the node/leaf to survive),
// frustum-culls each interior node's AABB, and Append's every visible
// drawable leaf's faces into out.
//
// Front-to-back order matters because the rasterizer's span buffer
// resolves opaque occlusion by accept-first-touch: the first surface
// to claim each span wins, and "first" here means "closer to the
// camera". The walker delivers that ordering by visiting the child
// whose half-space contains the viewer BEFORE the far-side child,
// matching upstream's `side = (dot >= 0) ? 0 : 1; recurse(children[side])
// ...; recurse(children[!side])` pattern.
//
// Side convention (matches tyrquake r_bsp.c):
//
//   - PlaneSide(viewer, plane) > 0 (viewer in front)  -> visit child[0] first.
//   - PlaneSide(viewer, plane) < 0 (viewer behind)    -> visit child[1] first.
//   - PlaneSide(viewer, plane) == 0 (viewer on plane) -> visit child[0] first
//     (tyrquake's `dot >= 0` treats the on-plane case as "front side").
//
// Recursion depth is bounded by BSP tree depth (typically log2 of
// numNodes, so ~12-15 for vanilla Q1 maps; the deepest commercial map
// goes to about 19). Go's goroutine stack autogrows; no manual stack
// promotion needed.
//
// Returns ErrWalkNilList if out == nil, ErrWalkRootRange if rootIdx is
// outside [0, ctx.NumNodes), and nil after a successful walk.
func WalkWorld(
	ctx WalkContext,
	rootIdx int,
	viewer [3]float32,
	frustum render.Frustum,
	currentFrame int32,
	out *SurfaceList,
) error {
	if out == nil {
		return ErrWalkNilList
	}
	if rootIdx < 0 || rootIdx >= ctx.NumNodes {
		return ErrWalkRootRange
	}
	walkRecurse(ctx, rootIdx, viewer, frustum, currentFrame, out)
	return nil
}

// walkRecurse is the recursive worker. Split out so the public entry
// point can do its argument validation once at the top level instead
// of on every recursive call.
//
// Dispatch is on [NodeKind] so the walker treats interior nodes and
// leaves uniformly without manually decoding the negative-NOT
// child-index trick from the on-disk BSP at every step.
func walkRecurse(
	ctx WalkContext,
	idx int,
	viewer [3]float32,
	frustum render.Frustum,
	currentFrame int32,
	out *SurfaceList,
) {
	switch ctx.NodeKind(idx) {
	case NodeKindEmpty:
		// A leaf with no faces (the outside-the-map sentinel, etc.).
		// Nothing to draw, nothing to recurse into.
		return
	case NodeKindLeaf:
		// PVS mask: skip leaves not stamped this frame.
		if ctx.LeafVisFrame(idx) != currentFrame {
			return
		}
		for _, faceIdx := range ctx.LeafFaces(idx) {
			out.Append(SurfaceRef{FaceIdx: faceIdx, LeafIdx: idx})
		}
		return
	}

	// Interior node: PVS first (cheapest test), then frustum.
	if ctx.NodeVisFrame(idx) != currentFrame {
		return
	}
	mins, maxs := ctx.NodeBBox(idx)
	if !frustum.BoxInFrustum(mins, maxs) {
		return
	}

	plane := ctx.NodePlane(idx)
	children := ctx.NodeChildren(idx)
	// Front side first. tyrquake: side = (dot >= 0) ? 0 : 1; we route
	// the on-plane case to the front child for the exact same reason
	// -- it keeps the painter's algorithm right when the camera sits
	// flush against a splitting plane.
	near, far := children[0], children[1]
	if render.PlaneSide(viewer, plane) < 0 {
		near, far = children[1], children[0]
	}
	walkRecurse(ctx, near, viewer, frustum, currentFrame, out)
	walkRecurse(ctx, far, viewer, frustum, currentFrame, out)
}
