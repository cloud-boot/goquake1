// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"sort"
	"testing"
)

// Helper to build a 200x200 World rooted at the origin so coordinate
// expectations in the tests stay readable.
func newTestWorld(t *testing.T) *World {
	t.Helper()
	w := New()
	w.Clear([3]float32{-100, -100, -100}, [3]float32{100, 100, 100})
	return w
}

// SolidKindSkip is a no-op: nothing gets stored, the post-link
// IsLinked check returns false.
func TestLinkBounds_SkipKindNoOp(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(7, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSkip)
	if w.IsLinked(7) {
		t.Error("SolidKindSkip should not link the key")
	}
}

// A trigger entity lives in TriggerEdicts at the first node that
// straddles its box.
func TestLinkBounds_TriggerLandsInTriggerList(t *testing.T) {
	w := newTestWorld(t)
	// (-1..1, -1..1, -1..1) -- straddles every root-tree split, so
	// it lives at the root.
	w.LinkBounds(42, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindTrigger)
	if !w.IsLinked(42) {
		t.Fatal("trigger not linked")
	}
	root := w.Root()
	if root.TriggerEdicts.Len() != 1 {
		t.Errorf("root TriggerEdicts: got %d want 1", root.TriggerEdicts.Len())
	}
	if root.SolidEdicts.Len() != 0 {
		t.Errorf("root SolidEdicts: got %d want 0", root.SolidEdicts.Len())
	}
}

// A solid entity lives in SolidEdicts.
func TestLinkBounds_SolidLandsInSolidList(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(13, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)
	root := w.Root()
	if root.SolidEdicts.Len() != 1 {
		t.Errorf("root SolidEdicts: got %d want 1", root.SolidEdicts.Len())
	}
	if root.TriggerEdicts.Len() != 0 {
		t.Errorf("root TriggerEdicts: got %d want 0", root.TriggerEdicts.Len())
	}
}

// An entity entirely on the + side of a split descends into
// Children[0]; entirely on the - side descends into Children[1].
func TestLinkBounds_DescendsToCorrectSide(t *testing.T) {
	w := newTestWorld(t)
	// 200x200 root -> Axis=1 (y tiebreaker), Dist=0.
	// (10..20) on x AND y -- entirely +y side, so Children[0].
	// Inside that child the axis is 0 (x is now longer at half y),
	// Dist = 0 (200x100 -> midpoint 0). (10..20) is +x -> Children[0]
	// again. The straddle test would only fire deeper or never.
	w.LinkBounds(5, [3]float32{10, 10, 0}, [3]float32{20, 20, 0}, SolidKindSolid)
	root := w.Root()
	if root.SolidEdicts.Len() != 0 {
		t.Errorf("box entirely +y should not live at root: got %d entries at root", root.SolidEdicts.Len())
	}
	// Walk Children[0]. At some depth the entity lives.
	upperRoot := root.Children[0]
	if upperRoot == nil {
		t.Fatal("Children[0] is nil")
	}
	// Don't pin which exact node -- just verify SOMETHING got linked
	// in the +y subtree.
	var count int
	var walk func(*AreaNode)
	walk = func(n *AreaNode) {
		if n == nil {
			return
		}
		count += n.SolidEdicts.Len()
		walk(n.Children[0])
		walk(n.Children[1])
	}
	walk(upperRoot)
	if count != 1 {
		t.Errorf("+y subtree solid count: got %d want 1", count)
	}
}

// Re-linking a key first unlinks the old position. After two
// LinkBounds calls with different bounds, only ONE entry exists
// (the C upstream's "unlink from old position" prefix in SV_LinkEdict).
func TestLinkBounds_RelinkReplacesOldEntry(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(99, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)
	w.LinkBounds(99, [3]float32{-2, -2, -2}, [3]float32{2, 2, 2}, SolidKindSolid)
	// Count solid entries across the entire tree.
	var total int
	var walk func(*AreaNode)
	walk = func(n *AreaNode) {
		if n == nil {
			return
		}
		total += n.SolidEdicts.Len()
		walk(n.Children[0])
		walk(n.Children[1])
	}
	walk(w.Root())
	if total != 1 {
		t.Errorf("after relink: got %d entries want 1", total)
	}
}

// Re-linking with SolidKindSkip after an earlier link unlinks.
func TestLinkBounds_RelinkWithSkipUnlinks(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(8, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)
	if !w.IsLinked(8) {
		t.Fatal("baseline link missing")
	}
	w.LinkBounds(8, [3]float32{0, 0, 0}, [3]float32{0, 0, 0}, SolidKindSkip)
	if w.IsLinked(8) {
		t.Error("SolidKindSkip relink should unlink the prior entry")
	}
}

// LinkBounds on a World whose Clear hasn't been called yet is a
// silent no-op (the upstream's null-worldmodel path crashes; the
// Go port treats it as defensively non-fatal).
func TestLinkBounds_NoClearIsNoOp(t *testing.T) {
	w := New()
	w.LinkBounds(1, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)
	if w.IsLinked(1) {
		t.Error("LinkBounds with no Clear should not link")
	}
}

// UnlinkBounds on a key never linked is a no-op (idempotent).
func TestUnlinkBounds_NeverLinkedNoOp(t *testing.T) {
	w := newTestWorld(t)
	w.UnlinkBounds(404) // no panic
	if w.IsLinked(404) {
		t.Error("unlinked key should not appear linked")
	}
}

// Unlinking a trigger keeps the rest of the tree intact.
func TestUnlinkBounds_RemovesTrigger(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(1, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindTrigger)
	w.LinkBounds(2, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindTrigger)
	w.UnlinkBounds(1)
	if w.IsLinked(1) {
		t.Error("key 1 should be unlinked")
	}
	if !w.IsLinked(2) {
		t.Error("key 2 should still be linked")
	}
	if w.Root().TriggerEdicts.Len() != 1 {
		t.Errorf("root TriggerEdicts after Unlink: got %d want 1", w.Root().TriggerEdicts.Len())
	}
}

// Unlinking a solid uses the SolidEdicts list (separate branch from
// trigger). Covers the kind == SolidKindSolid path in Unlink.
func TestUnlinkBounds_RemovesSolid(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(10, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)
	w.UnlinkBounds(10)
	if w.IsLinked(10) {
		t.Error("key 10 should be unlinked")
	}
	if w.Root().SolidEdicts.Len() != 0 {
		t.Errorf("root SolidEdicts after Unlink: got %d want 0", w.Root().SolidEdicts.Len())
	}
}

// --- AreaQuery -----------------------------------------------------------

// Empty World (no Clear): AreaQuery returns nil.
func TestAreaQuery_NoClear(t *testing.T) {
	w := New()
	got := w.AreaQuery([3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, QueryBoth)
	if got != nil {
		t.Errorf("query on empty World: got %v want nil", got)
	}
}

// QueryTriggersOnly returns trigger entries only; QuerySolidsOnly
// returns solids only; QueryBoth returns the union.
func TestAreaQuery_KindFilters(t *testing.T) {
	w := newTestWorld(t)
	w.LinkBounds(1, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindTrigger)
	w.LinkBounds(2, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)

	box := struct{ mins, maxs [3]float32 }{[3]float32{-10, -10, -10}, [3]float32{10, 10, 10}}

	if got := w.AreaQuery(box.mins, box.maxs, QueryTriggersOnly); !sameKeys(got, []Key{1}) {
		t.Errorf("triggers-only: got %v want [1]", got)
	}
	if got := w.AreaQuery(box.mins, box.maxs, QuerySolidsOnly); !sameKeys(got, []Key{2}) {
		t.Errorf("solids-only: got %v want [2]", got)
	}
	if got := w.AreaQuery(box.mins, box.maxs, QueryBoth); !sameKeys(got, []Key{1, 2}) {
		t.Errorf("both: got %v want [1 2]", got)
	}
}

// AreaQuery applies the bounding-box overlap filter -- an entity
// stored at the root with bounds (10..20) is excluded from a query
// of (-5..5).
func TestAreaQuery_BoundsOverlapFilter(t *testing.T) {
	w := newTestWorld(t)
	// First entity at (-1..1) -- in the query box.
	w.LinkBounds(1, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, SolidKindSolid)
	// Second entity at (10..20) -- straddles enough root-tree splits
	// to live near root, but its bounds are outside the query box.
	w.LinkBounds(2, [3]float32{10, 10, 10}, [3]float32{20, 20, 20}, SolidKindSolid)

	got := w.AreaQuery([3]float32{-5, -5, -5}, [3]float32{5, 5, 5}, QuerySolidsOnly)
	if !sameKeys(got, []Key{1}) {
		t.Errorf("bounds-filtered: got %v want [1]", got)
	}
}

// AreaQuery walks BOTH children when the query box straddles a
// node's split plane (the C upstream's "if (maxs[axis] > dist)" +
// "if (mins[axis] < dist)" pair).
func TestAreaQuery_BothChildrenWalked(t *testing.T) {
	w := newTestWorld(t)
	// Two entities entirely on each side of y=0. They live in
	// separate sub-trees; a query straddling y=0 must find both.
	w.LinkBounds(1, [3]float32{-2, 10, 0}, [3]float32{2, 20, 0}, SolidKindSolid)   // +y
	w.LinkBounds(2, [3]float32{-2, -20, 0}, [3]float32{2, -10, 0}, SolidKindSolid) // -y

	got := w.AreaQuery([3]float32{-5, -50, -5}, [3]float32{5, 50, 5}, QuerySolidsOnly)
	if !sameKeys(got, []Key{1, 2}) {
		t.Errorf("two-side query: got %v want [1 2]", got)
	}
}

// AreaQuery does NOT walk the - child when its mins is above the
// split plane (covers the maxs[axis] > dist false branch).
func TestAreaQuery_OneSideShortCircuits(t *testing.T) {
	w := newTestWorld(t)
	// Two entities on opposite y sides. Query entirely on the +y
	// side should only return the +y entity (the -y subtree's
	// branch is skipped).
	w.LinkBounds(1, [3]float32{-2, 10, 0}, [3]float32{2, 20, 0}, SolidKindSolid)
	w.LinkBounds(2, [3]float32{-2, -20, 0}, [3]float32{2, -10, 0}, SolidKindSolid)

	got := w.AreaQuery([3]float32{-5, 5, -5}, [3]float32{5, 50, 5}, QuerySolidsOnly)
	if !sameKeys(got, []Key{1}) {
		t.Errorf("+y-only query: got %v want [1]", got)
	}
}

// --- boundsOverlap (the predicate AreaQuery applies) --------------------

func TestBoundsOverlap(t *testing.T) {
	cases := []struct {
		name                   string
		amin, amax, bmin, bmax [3]float32
		want                   bool
	}{
		{"identical", [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, true},
		{"touching x", [3]float32{0, 0, 0}, [3]float32{1, 1, 1}, [3]float32{1, 0, 0}, [3]float32{2, 1, 1}, true},
		{"disjoint x", [3]float32{0, 0, 0}, [3]float32{1, 1, 1}, [3]float32{2, 0, 0}, [3]float32{3, 1, 1}, false},
		{"disjoint y", [3]float32{0, 0, 0}, [3]float32{1, 1, 1}, [3]float32{0, 2, 0}, [3]float32{1, 3, 1}, false},
		{"disjoint z", [3]float32{0, 0, 0}, [3]float32{1, 1, 1}, [3]float32{0, 0, 2}, [3]float32{1, 1, 3}, false},
		{"containing", [3]float32{-10, -10, -10}, [3]float32{10, 10, 10}, [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}, true},
	}
	for _, c := range cases {
		if got := boundsOverlap(c.amin, c.amax, c.bmin, c.bmax); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// sameKeys: returns true iff a and b contain the same keys (order-
// insensitive). The AreaQuery walk order is implementation-defined.
func sameKeys(a, b []Key) bool {
	if len(a) != len(b) {
		return false
	}
	ka := append([]Key(nil), a...)
	kb := append([]Key(nil), b...)
	sort.Slice(ka, func(i, j int) bool { return ka[i] < ka[j] })
	sort.Slice(kb, func(i, j int) bool { return kb[i] < kb[j] })
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}
