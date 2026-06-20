// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import "container/list"

// Key identifies an edict to the world layer. In the C upstream the
// equivalent is EDICT_TO_PROG(ent) (a byte offset into the prog's
// edict pool). The server glue layer is responsible for the
// Key <-> *progs.Edict translation; world treats Keys as opaque.
type Key uint32

// SolidKind classifies how the world layer treats an entity's
// participation in the area tree. SOLID_NOT entities get
// [SolidKindSkip] (never linked); SOLID_TRIGGER gets
// [SolidKindTrigger] (linked into the trigger list, fires touch
// callbacks); every other SOLID_* (BBOX, SLIDEBOX, BSP) becomes
// [SolidKindSolid] (linked into the solid list, blocks traces).
// tyrquake: the SOLID_* dispatch inside SV_LinkEdict that picks
// between trigger_edicts and solid_edicts.
type SolidKind int

const (
	SolidKindSkip    SolidKind = iota // SOLID_NOT -- not linked
	SolidKindTrigger                  // SOLID_TRIGGER
	SolidKindSolid                    // SOLID_BBOX, SOLID_SLIDEBOX, SOLID_BSP
)

// AreaQueryKind filters which area-tree lists [World.AreaQuery]
// walks. Most callers want QuerySolidsOnly (the SV_TraceMove
// broadphase); the trigger-touch loop in SV_TouchLinks wants
// QueryTriggersOnly; a few debug paths want QueryBoth.
type AreaQueryKind int

const (
	QueryTriggersOnly AreaQueryKind = iota
	QuerySolidsOnly
	QueryBoth
)

// linkEntry holds the per-edict state the world layer remembers
// across LinkBounds + UnlinkBounds + AreaQuery. The bounds are
// stored so AreaQuery can apply the bounding-box overlap filter
// the C upstream does inline (ent->v.absmin/absmax check inside
// the trigger / solid loops).
type linkEntry struct {
	key     Key
	absmin  [3]float32
	absmax  [3]float32
	kind    SolidKind
	node    *AreaNode     // which AreaNode owns the list element
	element *list.Element // the element pointer used to remove on Unlink
}

// LinkBounds inserts (or relinks) a key into the area tree at the
// first node whose split plane the (absmin..absmax) box crosses. The
// edict's caller-computed absolute bounds (including any FL_ITEM
// expansion + the 1-unit epsilon the C upstream applies) are stored
// so AreaQuery can apply the bounding-box overlap filter the C
// loops do inline. tyrquake: SV_LinkEdict, post the absmin/absmax
// compute that the server glue is responsible for.
//
// kind == SolidKindSkip is a no-op (SOLID_NOT edicts are never
// linked); the call still removes any prior link, mirroring the
// C upstream's SV_UnlinkEdict-then-SOLID_NOT-early-out shape.
//
// Re-linking a key that is already present unlinks it first (the
// upstream's "unlink from old position" branch at the top of
// SV_LinkEdict).
func (w *World) LinkBounds(key Key, absmin, absmax [3]float32, kind SolidKind) {
	if w.links == nil {
		w.links = map[Key]*linkEntry{}
	}
	// Always drop any prior link first -- matches the C upstream's
	// "unlink from old position" prefix in SV_LinkEdict.
	w.UnlinkBounds(key)
	if kind == SolidKindSkip {
		return
	}
	if w.root == nil {
		// Clear() never called -- silently drop. The upstream
		// crashes on a null worldmodel; the Go port treats it as
		// a no-op so a half-initialised World doesn't panic.
		return
	}

	// Descend to the first area node the box crosses.
	node := w.root
	for node.Axis != -1 {
		if absmin[node.Axis] > node.Dist {
			node = node.Children[0]
		} else if absmax[node.Axis] < node.Dist {
			node = node.Children[1]
		} else {
			break // straddles -> live at this node
		}
	}

	entry := &linkEntry{
		key:    key,
		absmin: absmin,
		absmax: absmax,
		kind:   kind,
		node:   node,
	}
	if kind == SolidKindTrigger {
		entry.element = node.TriggerEdicts.PushBack(entry)
	} else {
		entry.element = node.SolidEdicts.PushBack(entry)
	}
	w.links[key] = entry
}

// UnlinkBounds removes key from the area tree. No-op if the key
// is not currently linked. tyrquake: SV_UnlinkEdict (the
// !ent->area.prev early-out).
func (w *World) UnlinkBounds(key Key) {
	entry, ok := w.links[key]
	if !ok {
		return
	}
	if entry.kind == SolidKindTrigger {
		entry.node.TriggerEdicts.Remove(entry.element)
	} else {
		entry.node.SolidEdicts.Remove(entry.element)
	}
	delete(w.links, key)
}

// IsLinked reports whether key is currently in the area tree.
// Exposed for callers (the upcoming sv_phys + sv_user layers) that
// need to skip a re-link when the entity hasn't moved.
func (w *World) IsLinked(key Key) bool {
	_, ok := w.links[key]
	return ok
}

// AreaQuery returns every linked key whose stored absmin/absmax box
// overlaps (mins..maxs), filtered by want. The walk follows the
// same descent shape the C SV_TouchLinks + SV_AreaEdicts use:
// recurse into children[0] when the query's maxs exceed the node's
// split plane, into children[1] when its mins fall below.
//
// The returned slice is freshly allocated per call; callers may
// retain it.
func (w *World) AreaQuery(mins, maxs [3]float32, want AreaQueryKind) []Key {
	if w.root == nil {
		return nil
	}
	var out []Key
	w.areaQueryR(w.root, mins, maxs, want, &out)
	return out
}

func (w *World) areaQueryR(node *AreaNode, mins, maxs [3]float32, want AreaQueryKind, out *[]Key) {
	if want == QueryTriggersOnly || want == QueryBoth {
		for e := node.TriggerEdicts.Front(); e != nil; e = e.Next() {
			entry := e.Value.(*linkEntry)
			if boundsOverlap(entry.absmin, entry.absmax, mins, maxs) {
				*out = append(*out, entry.key)
			}
		}
	}
	if want == QuerySolidsOnly || want == QueryBoth {
		for e := node.SolidEdicts.Front(); e != nil; e = e.Next() {
			entry := e.Value.(*linkEntry)
			if boundsOverlap(entry.absmin, entry.absmax, mins, maxs) {
				*out = append(*out, entry.key)
			}
		}
	}
	if node.Axis == -1 {
		return
	}
	if maxs[node.Axis] > node.Dist {
		w.areaQueryR(node.Children[0], mins, maxs, want, out)
	}
	if mins[node.Axis] < node.Dist {
		w.areaQueryR(node.Children[1], mins, maxs, want, out)
	}
}

// boundsOverlap returns true iff the two axis-aligned bounding
// boxes intersect (touching edges count). The C upstream's
// loop-and-break shape inverts the predicate (continue on any-axis
// disjoint); the inverted positive form here is equivalent.
func boundsOverlap(amin, amax, bmin, bmax [3]float32) bool {
	for i := 0; i < 3; i++ {
		if amin[i] > bmax[i] || amax[i] < bmin[i] {
			return false
		}
	}
	return true
}
