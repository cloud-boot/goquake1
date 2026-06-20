// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// --- HullForBounds: SOLID_BSP branch -------------------------------------

// makeBrushModelStub builds a minimal *model.BrushModel for HullForBounds
// tests. We don't need a full BSP -- just 4 hulls with distinct
// ClipMins so the offset computation is observable per index pick.
func makeBrushModelStub() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipMins: [3]float32{0, 0, 0},
		ClipMaxs: [3]float32{0, 0, 0},
	}
	bm.Hulls[1] = bsptrace.Hull{
		ClipMins: model.BrushHullSizes[1].Mins,
		ClipMaxs: model.BrushHullSizes[1].Maxs,
	}
	bm.Hulls[2] = bsptrace.Hull{
		ClipMins: model.BrushHullSizes[2].Mins,
		ClipMaxs: model.BrushHullSizes[2].Maxs,
	}
	return bm
}

// size[0] < 3 -> hull 0 (the BSP draw tree).
func TestHullForBounds_BSP_SmallSizePicksHull0(t *testing.T) {
	bm := makeBrushModelStub()
	target := Target{
		Origin:     [3]float32{100, 200, 300},
		Solid:      server.SolidBSP,
		BrushModel: bm,
	}
	// Point-like test object (mins == maxs) -> size 0 < 3.
	hull, offset, err := HullForBounds([3]float32{0, 0, 0}, [3]float32{0, 0, 0}, target)
	if err != nil {
		t.Fatal(err)
	}
	if hull.ClipMins != bm.Hulls[0].ClipMins {
		t.Errorf("expected hull 0; ClipMins got %v want %v", hull.ClipMins, bm.Hulls[0].ClipMins)
	}
	if offset != [3]float32{100, 200, 300} {
		t.Errorf("offset: got %v want target origin (100, 200, 300)", offset)
	}
}

// 3 <= size[0] <= 32 -> hull 1 (player size).
func TestHullForBounds_BSP_MediumSizePicksHull1(t *testing.T) {
	bm := makeBrushModelStub()
	target := Target{
		Origin:     [3]float32{0, 0, 0},
		Solid:      server.SolidBSP,
		BrushModel: bm,
	}
	// Player-sized test object: size_x = 32.
	hull, _, err := HullForBounds([3]float32{-16, -16, -24}, [3]float32{16, 16, 32}, target)
	if err != nil {
		t.Fatal(err)
	}
	if hull.ClipMins != bm.Hulls[1].ClipMins {
		t.Errorf("expected hull 1; got %v want %v", hull.ClipMins, bm.Hulls[1].ClipMins)
	}
}

// size[0] > 32 -> hull 2 (monster size).
func TestHullForBounds_BSP_LargeSizePicksHull2(t *testing.T) {
	bm := makeBrushModelStub()
	target := Target{
		Origin:     [3]float32{0, 0, 0},
		Solid:      server.SolidBSP,
		BrushModel: bm,
	}
	// Monster-sized test object: size_x = 64.
	hull, _, err := HullForBounds([3]float32{-32, -32, -24}, [3]float32{32, 32, 64}, target)
	if err != nil {
		t.Fatal(err)
	}
	if hull.ClipMins != bm.Hulls[2].ClipMins {
		t.Errorf("expected hull 2; got %v want %v", hull.ClipMins, bm.Hulls[2].ClipMins)
	}
}

// Offset formula: hull.ClipMins - mins + target.Origin.
func TestHullForBounds_BSP_OffsetFormula(t *testing.T) {
	bm := makeBrushModelStub()
	// Force hull 1 with player-size test object; ClipMins = (-16,-16,-24).
	target := Target{
		Origin:     [3]float32{1000, 2000, 3000},
		Solid:      server.SolidBSP,
		BrushModel: bm,
	}
	mins := [3]float32{-16, -16, -24}
	maxs := [3]float32{16, 16, 32}
	_, offset, err := HullForBounds(mins, maxs, target)
	if err != nil {
		t.Fatal(err)
	}
	// Expected offset = (-16,-16,-24) - (-16,-16,-24) + (1000,2000,3000) = (1000,2000,3000)
	if offset != [3]float32{1000, 2000, 3000} {
		t.Errorf("offset: got %v want (1000,2000,3000)", offset)
	}
}

// SOLID_BSP without a BrushModel -> ErrSolidBSPNeedsBrushModel.
func TestHullForBounds_BSP_NilBrushModelErrors(t *testing.T) {
	target := Target{Solid: server.SolidBSP}
	_, _, err := HullForBounds([3]float32{0, 0, 0}, [3]float32{0, 0, 0}, target)
	if !errors.Is(err, ErrSolidBSPNeedsBrushModel) {
		t.Errorf("got %v want ErrSolidBSPNeedsBrushModel", err)
	}
}

// --- HullForBounds: non-BSP branch ---------------------------------------

// A BBOX target: builds a boxhull from Minkowski-diff bounds.
func TestHullForBounds_BBoxBuildsBoxhull(t *testing.T) {
	// Test object: 32x32x32 player.
	mins := [3]float32{-16, -16, -16}
	maxs := [3]float32{16, 16, 16}
	target := Target{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-10, -10, -10}, // target's own size
		Maxs:   [3]float32{10, 10, 10},
		Solid:  server.SolidBBox,
	}
	hull, offset, err := HullForBounds(mins, maxs, target)
	if err != nil {
		t.Fatal(err)
	}
	// Minkowski-diff:
	//   hullmins = target.Mins - maxs = (-10,-10,-10) - (16,16,16) = (-26,-26,-26)
	//   hullmaxs = target.Maxs - mins = (10,10,10)   - (-16,-16,-16) = (26,26,26)
	// boxhull plane 0 = +X face, .Dist = hullmaxs[0] = 26.
	if hull.Planes[0].Dist != 26 {
		t.Errorf("plane 0 Dist: got %v want 26 (Minkowski-diff hullmaxs[0])", hull.Planes[0].Dist)
	}
	if hull.Planes[1].Dist != -26 {
		t.Errorf("plane 1 Dist: got %v want -26 (Minkowski-diff hullmins[0])", hull.Planes[1].Dist)
	}
	// Offset = target.Origin = (0,0,0).
	if offset != target.Origin {
		t.Errorf("offset: got %v want target origin %v", offset, target.Origin)
	}
}

// SOLID_TRIGGER takes the same non-BSP branch as BBOX.
func TestHullForBounds_TriggerBuildsBoxhull(t *testing.T) {
	target := Target{
		Origin: [3]float32{5, 5, 5},
		Mins:   [3]float32{-10, -10, -10},
		Maxs:   [3]float32{10, 10, 10},
		Solid:  server.SolidTrigger,
	}
	_, offset, err := HullForBounds([3]float32{0, 0, 0}, [3]float32{0, 0, 0}, target)
	if err != nil {
		t.Fatal(err)
	}
	if offset != [3]float32{5, 5, 5} {
		t.Errorf("offset: got %v want (5,5,5)", offset)
	}
}

// SOLID_SLIDEBOX same.
func TestHullForBounds_SlideBoxBuildsBoxhull(t *testing.T) {
	target := Target{
		Mins:  [3]float32{-1, -1, -1},
		Maxs:  [3]float32{1, 1, 1},
		Solid: server.SolidSlideBox,
	}
	hull, _, err := HullForBounds([3]float32{0, 0, 0}, [3]float32{0, 0, 0}, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(hull.Planes) != 6 {
		t.Errorf("expected 6-plane boxhull, got %d planes", len(hull.Planes))
	}
}

// SOLID_NOT also takes the non-BSP branch (the caller is responsible
// for not passing SOLID_NOT in normally; the path is still defined).
func TestHullForBounds_NotBuildsBoxhull(t *testing.T) {
	target := Target{
		Mins:  [3]float32{-1, -1, -1},
		Maxs:  [3]float32{1, 1, 1},
		Solid: server.SolidNot,
	}
	_, _, err := HullForBounds([3]float32{0, 0, 0}, [3]float32{0, 0, 0}, target)
	if err != nil {
		t.Errorf("SolidNot should not error in HullForBounds, got %v", err)
	}
}

// --- PointContents -------------------------------------------------------

// Build a 1-clipnode "everything inside this hull is SOLID" hull
// stub for PointContents testing.
func makeSolidWorldModel() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// Build a 1-clipnode hull whose +x leaf is the requested contents tag.
func makeWorldModelReturning(contents int32) *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{int16(contents), int16(contents)}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

func TestPointContents_NilWorldModelErrors(t *testing.T) {
	_, err := PointContents(nil, [3]float32{0, 0, 0})
	if !errors.Is(err, ErrWorldModelNil) {
		t.Errorf("got %v want ErrWorldModelNil", err)
	}
}

func TestPointContents_HappyPath(t *testing.T) {
	wm := makeSolidWorldModel()
	got, err := PointContents(wm, [3]float32{1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != bspfile.ContentsSolid {
		t.Errorf("PointContents: got %d want SOLID", got)
	}
}

// CONTENTS_CURRENT_* (-9..-14) remap to CONTENTS_WATER (-3).
func TestPointContents_CurrentRemapsToWater(t *testing.T) {
	checks := []int32{
		bspfile.ContentsCurrent0,
		bspfile.ContentsCurrent90,
		bspfile.ContentsCurrent180,
		bspfile.ContentsCurrent270,
		bspfile.ContentsCurrentUp,
		bspfile.ContentsCurrentDn,
	}
	for _, in := range checks {
		wm := makeWorldModelReturning(in)
		got, err := PointContents(wm, [3]float32{1, 1, 1})
		if err != nil {
			t.Fatal(err)
		}
		if got != bspfile.ContentsWater {
			t.Errorf("Current %d should remap to WATER, got %d", in, got)
		}
	}
}

// Non-current contents pass through unchanged.
func TestPointContents_NonCurrentPassthrough(t *testing.T) {
	checks := []int32{
		bspfile.ContentsEmpty,
		bspfile.ContentsSolid,
		bspfile.ContentsWater,
		bspfile.ContentsSlime,
		bspfile.ContentsLava,
		bspfile.ContentsSky,
		bspfile.ContentsOrigin,
		bspfile.ContentsClip,
	}
	for _, in := range checks {
		wm := makeWorldModelReturning(in)
		got, err := PointContents(wm, [3]float32{1, 1, 1})
		if err != nil {
			t.Fatal(err)
		}
		if got != in {
			t.Errorf("contents %d should pass through, got %d", in, got)
		}
	}
}

// PointContents propagates errors from bsptrace.HullPointContents
// (corrupt clipnode -> ErrBadPlaneIndex).
func TestPointContents_PropagatesWalkError(t *testing.T) {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	if _, err := PointContents(bm, [3]float32{0, 0, 0}); err == nil {
		t.Error("expected error from corrupt hull")
	}
}
