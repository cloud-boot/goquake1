// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"container/list"

	"github.com/go-quake1/engine/server"
)

// AreaNode is one node of the depth-4 binary area tree the server
// uses to short-circuit entity sweeps. Internal nodes split the
// region at Dist along Axis (0=x, 1=y); Axis == -1 marks a leaf.
//
// TriggerEdicts and SolidEdicts hold the entities whose bounds
// straddle this node's split plane (entities entirely on one side
// descend into that child instead). The two lists are
// container/list.Lists so the per-entity Link/Unlink stays O(1)
// without a per-edict intrusive prev/next pair. tyrquake: areanode_t.
type AreaNode struct {
	Axis          int32   // -1 for a leaf, else 0 (x) or 1 (y)
	Dist          float32 // split-plane distance along Axis
	Children      [2]*AreaNode
	TriggerEdicts *list.List // SOLID_TRIGGER + crossing this node's plane
	SolidEdicts   *list.List // all other solids crossing this node's plane
}

// World owns the area-tree memory pool and the per-Clear root. A
// fresh World is empty -- call Clear with the map's worldmodel
// (mins, maxs) before linking edicts. tyrquake: the static
// sv_areanodes[] array + sv_numareanodes counter in world.c.
//
// The links map replaces the C upstream's intrusive link_t prev/next
// pair embedded in edict_t: world stores the per-edict link entry
// keyed by [Key] so progs.Edict stays decoupled from container/list.
type World struct {
	nodes [server.AreaNodes]AreaNode
	used  int // next free slot in nodes[]
	root  *AreaNode
	links map[Key]*linkEntry
}

// New returns an empty World. Use World.Clear to (re)build the area
// tree for a specific map's worldmodel bounds.
func New() *World {
	return &World{}
}

// Root returns the area-tree root node, or nil if Clear has not
// been called. Callers walk the tree via Children + Axis/Dist.
func (w *World) Root() *AreaNode { return w.root }

// NumNodes returns the count of area nodes allocated by the last
// Clear -- 31 for a complete depth-4 build, less if Clear was given
// a degenerate or zero-size bounds box.
func (w *World) NumNodes() int { return w.used }

// Clear resets the area-tree pool and (re)builds it to cover the
// (mins, maxs) bounding box of the loaded map's worldmodel.
// tyrquake: SV_ClearWorld.
//
// The build picks the LONGER of the (x, y) extents at each internal
// node and splits the region in half along that axis, recursing
// down to [server.AreaDepth] (=4) levels. The result is a balanced
// 31-node binary tree (16 leaves + 15 internal nodes) regardless
// of the input box's aspect ratio.
//
// Cannot fail: the depth-4 build always fits the
// [server.AreaNodes] (=32) static budget (1 + 2 + 4 + 8 + 16 = 31
// < 32). The internal pool guard panics if the two constants drift
// apart (an unrecoverable misconfiguration, not a runtime input
// problem).
func (w *World) Clear(mins, maxs [3]float32) {
	// Reset the pool entirely; container/list values inside the
	// previous nodes drop with the zero-out so old links don't
	// leak across Clear boundaries.
	for i := range w.nodes {
		w.nodes[i] = AreaNode{}
	}
	w.used = 0
	w.root = w.createAreaNode(0, mins, maxs)
}

// createAreaNode allocates one node from the pool, initializes its
// edict lists, and recurses to populate children when depth <
// server.AreaDepth. tyrquake: SV_CreateAreaNode.
//
// Panics if the pool is exhausted -- a depth-4 build needs 31 slots
// and [server.AreaNodes] is 32, so this only fires if the two
// constants drift apart. The C upstream's SV_Error path on the
// same condition serves the same "build-time invariant violated"
// role.
func (w *World) createAreaNode(depth int, mins, maxs [3]float32) *AreaNode {
	if w.used == server.AreaNodes {
		panic("world: area-tree node budget exhausted -- server.AreaNodes / AreaDepth drift?")
	}
	anode := &w.nodes[w.used]
	w.used++

	anode.TriggerEdicts = list.New()
	anode.SolidEdicts = list.New()

	if depth == server.AreaDepth {
		anode.Axis = -1
		anode.Children[0] = nil
		anode.Children[1] = nil
		return anode
	}

	// Pick the longer of (x, y) for this level's split. Quake's
	// area tree never subdivides along z -- maps are wide+long but
	// not very tall, so the y/x split gives the best fan-out.
	if maxs[0]-mins[0] > maxs[1]-mins[1] {
		anode.Axis = 0
	} else {
		anode.Axis = 1
	}
	anode.Dist = 0.5 * (maxs[anode.Axis] + mins[anode.Axis])

	mins1, maxs1 := mins, maxs
	mins2, maxs2 := mins, maxs
	maxs1[anode.Axis] = anode.Dist // lower half
	mins2[anode.Axis] = anode.Dist // upper half

	// tyrquake order: children[0] = upper half (mins2..maxs2),
	// children[1] = lower half (mins1..maxs1). Preserve that so
	// the LinkEdict / TraceMove descent picks the right child for
	// a given split-side comparison.
	anode.Children[0] = w.createAreaNode(depth+1, mins2, maxs2)
	anode.Children[1] = w.createAreaNode(depth+1, mins1, maxs1)
	return anode
}
