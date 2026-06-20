// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsptrace

import (
	"errors"
	"testing"

	"github.com/cloud-boot/goquake1/engine/bspfile"
)

// makeAxialHull builds a tiny synthetic Hull with one axial split
// plane: nodes[0] splits at x = splitX. Going left (x < split) -> solid,
// right -> empty.
func makeAxialHull(splitX float32) *Hull {
	return &Hull{
		ClipNodes: []bspfile.ClipNode{
			// node 0: plane 0, children = (empty leaf on +x side,
			// solid leaf on -x side).
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: splitX, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
}

// makeNonAxialHull builds a Hull with a single diagonal split plane
// (PlaneAnyZ -- the renderer's hint that the normal isn't an axis).
// Plane normal (1,1,1)/sqrt(3), distance = 5 -> the +side leaf is
// empty, -side is solid.
func makeNonAxialHull() *Hull {
	return &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{
				Normal: [3]float32{0.5773, 0.5773, 0.5773},
				Dist:   5,
				Type:   bspfile.PlaneAnyZ,
			},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
}

// --- axial-plane walk ------------------------------------------------------

func TestHullPointContents_Axial_EmptySide(t *testing.T) {
	h := makeAxialHull(10)
	got, err := HullPointContents(h, 0, [3]float32{20, 0, 0}) // x > 10 -> empty
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsEmpty {
		t.Errorf("got %d want %d (EMPTY)", got, bspfile.ContentsEmpty)
	}
}

func TestHullPointContents_Axial_SolidSide(t *testing.T) {
	h := makeAxialHull(10)
	got, err := HullPointContents(h, 0, [3]float32{5, 0, 0}) // x < 10 -> solid
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsSolid {
		t.Errorf("got %d want %d (SOLID)", got, bspfile.ContentsSolid)
	}
}

func TestHullPointContents_Axial_OnPlane(t *testing.T) {
	// dist == 0 -> not < 0 -> follows child[0] which is EMPTY.
	h := makeAxialHull(10)
	got, _ := HullPointContents(h, 0, [3]float32{10, 0, 0})
	if got != bspfile.ContentsEmpty {
		t.Errorf("on-plane: got %d want EMPTY (children[0] path)", got)
	}
}

// --- non-axial plane walk -------------------------------------------------

func TestHullPointContents_NonAxial(t *testing.T) {
	h := makeNonAxialHull()
	// dot((10,10,10), (0.5773,0.5773,0.5773)) - 5 = 17.3 - 5 = 12.3 > 0 -> EMPTY
	got, _ := HullPointContents(h, 0, [3]float32{10, 10, 10})
	if got != bspfile.ContentsEmpty {
		t.Errorf("non-axial +side: got %d want EMPTY", got)
	}
	// (0,0,0): dot = 0 - 5 = -5 < 0 -> SOLID
	got, _ = HullPointContents(h, 0, [3]float32{0, 0, 0})
	if got != bspfile.ContentsSolid {
		t.Errorf("non-axial -side: got %d want SOLID", got)
	}
}

// --- multi-level walk (two splits) ----------------------------------------

func TestHullPointContents_MultiLevel(t *testing.T) {
	// node 0: split at x=0; +x side goes to node 1, -x side is solid.
	// node 1: split at y=0; +y side is sky, -y side is empty.
	h := &Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{1, bspfile.ContentsSolid}}, // children[0]=+side=node1, children[1]=-side=SOLID
			{PlaneNum: 1, Children: [2]int16{bspfile.ContentsSky, bspfile.ContentsEmpty}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
			{Normal: [3]float32{0, 1, 0}, Dist: 0, Type: bspfile.PlaneY},
		},
		FirstClipNode: 0,
		LastClipNode:  1,
	}
	// (10, 10, 0) -> +x -> node 1 -> +y -> SKY
	got, _ := HullPointContents(h, 0, [3]float32{10, 10, 0})
	if got != bspfile.ContentsSky {
		t.Errorf("+x+y: got %d want SKY", got)
	}
	// (10, -10, 0) -> +x -> node 1 -> -y -> EMPTY
	got, _ = HullPointContents(h, 0, [3]float32{10, -10, 0})
	if got != bspfile.ContentsEmpty {
		t.Errorf("+x-y: got %d want EMPTY", got)
	}
	// (-10, 0, 0) -> -x -> SOLID
	got, _ = HullPointContents(h, 0, [3]float32{-10, 0, 0})
	if got != bspfile.ContentsSolid {
		t.Errorf("-x: got %d want SOLID", got)
	}
}

// --- error paths -----------------------------------------------------------

func TestHullPointContents_NilHull(t *testing.T) {
	if _, err := HullPointContents(nil, 0, [3]float32{}); err == nil {
		t.Error("expected nil-hull error")
	}
}

func TestHullPointContents_NodeOutOfRange_BelowFirst(t *testing.T) {
	h := makeAxialHull(0)
	h.FirstClipNode = 5
	h.LastClipNode = 10
	if _, err := HullPointContents(h, 0, [3]float32{}); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

func TestHullPointContents_NodeOutOfRange_AboveLast(t *testing.T) {
	h := makeAxialHull(0)
	h.LastClipNode = 0
	if _, err := HullPointContents(h, 99, [3]float32{}); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

func TestHullPointContents_NodeIndexPastSlice(t *testing.T) {
	// FirstClipNode + LastClipNode allow nodenum=2, but ClipNodes
	// slice only has 1 entry.
	h := makeAxialHull(0)
	h.LastClipNode = 10 // allows nodenum up to 10 by the range check
	if _, err := HullPointContents(h, 2, [3]float32{}); !errors.Is(err, ErrBadNodeIndex) {
		t.Errorf("got %v want ErrBadNodeIndex", err)
	}
}

func TestHullPointContents_BadPlaneIndex(t *testing.T) {
	h := makeAxialHull(0)
	h.ClipNodes[0].PlaneNum = 99 // past planes slice length
	if _, err := HullPointContents(h, 0, [3]float32{}); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex", err)
	}
}

func TestHullPointContents_NegativePlaneIndex(t *testing.T) {
	h := makeAxialHull(0)
	h.ClipNodes[0].PlaneNum = -1
	if _, err := HullPointContents(h, 0, [3]float32{}); !errors.Is(err, ErrBadPlaneIndex) {
		t.Errorf("got %v want ErrBadPlaneIndex", err)
	}
}

// --- start-already-a-leaf shortcut ---------------------------------------

func TestHullPointContents_StartAtLeaf(t *testing.T) {
	// nodenum starts negative -> the loop doesn't run; returns the
	// input contents tag verbatim. tyrquake matches this behaviour
	// since the loop condition is `while (num >= 0)`.
	h := makeAxialHull(0)
	got, err := HullPointContents(h, bspfile.ContentsLava, [3]float32{})
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsLava {
		t.Errorf("start-at-leaf: got %d want %d", got, bspfile.ContentsLava)
	}
}

// --- DistEpsilon constant ------------------------------------------------

func TestDistEpsilonValue(t *testing.T) {
	if DistEpsilon != 0.03125 {
		t.Errorf("DistEpsilon drift: %v", DistEpsilon)
	}
}
