// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package boxhull

import (
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
)

// Create returns a Hull with the expected 6-plane shape: the planes
// match the (mins, maxs) corners; the clipnodes encode the box's
// solid-inside / empty-outside classification.
func TestCreate_PlanesMatchBounds(t *testing.T) {
	mins := [3]float32{-1, -2, -3}
	maxs := [3]float32{4, 5, 6}
	h := Create(mins, maxs)

	if len(h.Planes) != 6 {
		t.Fatalf("len(Planes)=%d, want 6", len(h.Planes))
	}
	want := [6]float32{maxs[0], mins[0], maxs[1], mins[1], maxs[2], mins[2]}
	for i, p := range h.Planes {
		if p.Dist != want[i] {
			t.Errorf("plane %d Dist: got %v want %v", i, p.Dist, want[i])
		}
	}
	// Plane normals + types come from boxPlaneTemplate verbatim --
	// just verify the (normal, type) couples per face axis.
	checks := []struct {
		i      int
		normal [3]float32
		ptype  int32
	}{
		{0, [3]float32{1, 0, 0}, bspfile.PlaneX},
		{1, [3]float32{1, 0, 0}, bspfile.PlaneX},
		{2, [3]float32{0, 1, 0}, bspfile.PlaneY},
		{3, [3]float32{0, 1, 0}, bspfile.PlaneY},
		{4, [3]float32{0, 0, 1}, bspfile.PlaneZ},
		{5, [3]float32{0, 0, 1}, bspfile.PlaneZ},
	}
	for _, c := range checks {
		if h.Planes[c.i].Normal != c.normal || h.Planes[c.i].Type != c.ptype {
			t.Errorf("plane %d normal/type: got (%v, %d) want (%v, %d)",
				c.i, h.Planes[c.i].Normal, h.Planes[c.i].Type, c.normal, c.ptype)
		}
	}
	if h.FirstClipNode != 0 || h.LastClipNode != 5 {
		t.Errorf("clipnode range: got [%d, %d] want [0, 5]", h.FirstClipNode, h.LastClipNode)
	}
	if len(h.ClipNodes) != 6 {
		t.Errorf("len(ClipNodes)=%d, want 6", len(h.ClipNodes))
	}
}

// HullPointContents traversal: a point INSIDE the box returns SOLID;
// a point on any face's outside returns EMPTY.
func TestCreate_PointContents_InsideVsOutside(t *testing.T) {
	h := Create([3]float32{-10, -10, -10}, [3]float32{10, 10, 10})

	// Inside the box -- expect SOLID.
	got, err := bsptrace.HullPointContents(&h, 0, [3]float32{0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsSolid {
		t.Errorf("origin inside: got %d want SOLID", got)
	}

	// Outside each face -- expect EMPTY.
	outsides := [][3]float32{
		{20, 0, 0},    // beyond +X
		{-20, 0, 0},   // beyond -X
		{0, 20, 0},    // beyond +Y
		{0, -20, 0},   // beyond -Y
		{0, 0, 20},    // beyond +Z
		{0, 0, -20},   // beyond -Z
		{15, 15, 15},  // outside all positive faces (corner)
		{-15, -15, 0}, // outside two negative faces
	}
	for _, p := range outsides {
		got, err := bsptrace.HullPointContents(&h, 0, p)
		if err != nil {
			t.Fatalf("at %v: %v", p, err)
		}
		if got != bspfile.ContentsEmpty {
			t.Errorf("point %v: got %d want EMPTY", p, got)
		}
	}
}

// A trace from outside to inside should record an impact (fraction<1)
// and the impact plane's normal should be the entry face's normal.
func TestCreate_TraceFromOutsideHitsBox(t *testing.T) {
	h := Create([3]float32{-5, -5, -5}, [3]float32{5, 5, 5})

	// Approach from +X (start at x=20, end at origin) -- entry on
	// the +X face (normal=(1,0,0)).
	tr := bsptrace.DefaultTrace()
	_, err := bsptrace.TraceHull(&h, 0, [3]float32{20, 0, 0}, [3]float32{0, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Fraction >= 1.0 {
		t.Errorf("expected impact, fraction=%v", tr.Fraction)
	}
	// The +X face's outward normal is (1,0,0) -- the trace approaches
	// from +X so side=0 (dist1>=0) and the plane is recorded
	// unflipped.
	if tr.Plane.Normal != [3]float32{1, 0, 0} {
		t.Errorf("impact plane normal: got %v want (1,0,0)", tr.Plane.Normal)
	}
}

// A trace entirely inside a box stays solid; AllSolid + StartSolid set.
func TestCreate_TraceInsideStaysSolid(t *testing.T) {
	h := Create([3]float32{-5, -5, -5}, [3]float32{5, 5, 5})
	tr := bsptrace.DefaultTrace()
	_, err := bsptrace.TraceHull(&h, 0, [3]float32{-2, 0, 0}, [3]float32{2, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.AllSolid || !tr.StartSolid {
		t.Errorf("expected AllSolid+StartSolid, got tr=%+v", tr)
	}
}

// Zero-extent box (mins == maxs) is degenerate but must still build
// without panic; trace through it should miss (fraction=1).
func TestCreate_ZeroExtent(t *testing.T) {
	h := Create([3]float32{0, 0, 0}, [3]float32{0, 0, 0})
	if len(h.Planes) != 6 {
		t.Fatalf("len(Planes)=%d", len(h.Planes))
	}
	tr := bsptrace.DefaultTrace()
	ok, err := bsptrace.TraceHull(&h, 0, [3]float32{10, 0, 0}, [3]float32{-10, 0, 0}, &tr)
	if err != nil {
		t.Fatal(err)
	}
	// A zero-extent box is a degenerate point; the trace passes
	// through without impact.
	if !ok || tr.Fraction < 1.0 {
		t.Errorf("zero-extent trace should not impact: ok=%v fraction=%v", ok, tr.Fraction)
	}
}

// The Planes slice returned by Create must be heap-allocated --
// modifying one return value's planes must not affect another's.
// (Smoke test for the heap-allocation invariant in the Create
// comment.)
func TestCreate_PlanesAreIndependent(t *testing.T) {
	h1 := Create([3]float32{-1, -1, -1}, [3]float32{1, 1, 1})
	h2 := Create([3]float32{-10, -10, -10}, [3]float32{10, 10, 10})
	if h1.Planes[0].Dist == h2.Planes[0].Dist {
		t.Errorf("planes alias each other: h1[0]=%v h2[0]=%v", h1.Planes[0].Dist, h2.Planes[0].Dist)
	}
	// Mutating h1's planes must not affect h2.
	h1.Planes[0].Dist = 999
	if h2.Planes[0].Dist == 999 {
		t.Errorf("plane aliasing: h2[0] changed when h1[0] was mutated")
	}
}
