// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// --- SweptBounds ---------------------------------------------------------

func TestSweptBounds_MoveAllPositive(t *testing.T) {
	// Test object (-1..1)^3 moves from (0,0,0) to (10,10,10).
	smin, smax := SweptBounds(
		[3]float32{0, 0, 0}, [3]float32{10, 10, 10},
		[3]float32{-1, -1, -1}, [3]float32{1, 1, 1},
	)
	// end > start on every axis -> mins = start + objMins - 1, maxs = end + objMaxs + 1.
	wantMin := [3]float32{-2, -2, -2} // 0 + (-1) - 1
	wantMax := [3]float32{12, 12, 12} // 10 + 1 + 1
	if smin != wantMin || smax != wantMax {
		t.Errorf("got (%v, %v) want (%v, %v)", smin, smax, wantMin, wantMax)
	}
}

func TestSweptBounds_MoveAllNegative(t *testing.T) {
	smin, smax := SweptBounds(
		[3]float32{10, 10, 10}, [3]float32{0, 0, 0},
		[3]float32{-1, -1, -1}, [3]float32{1, 1, 1},
	)
	// end < start on every axis -> mins = end + objMins - 1, maxs = start + objMaxs + 1.
	wantMin := [3]float32{-2, -2, -2}
	wantMax := [3]float32{12, 12, 12}
	if smin != wantMin || smax != wantMax {
		t.Errorf("got (%v, %v) want (%v, %v)", smin, smax, wantMin, wantMax)
	}
}

// Mixed axes: +x, -y, +z. Each axis takes its own branch independently.
func TestSweptBounds_MixedAxes(t *testing.T) {
	smin, smax := SweptBounds(
		[3]float32{0, 100, 0}, [3]float32{50, 50, 50},
		[3]float32{-1, -1, -1}, [3]float32{1, 1, 1},
	)
	want := struct {
		smin, smax [3]float32
	}{
		smin: [3]float32{-2, 48, -2},  // x: 0 + -1 - 1 ; y: 50 + -1 - 1 ; z: 0 + -1 - 1
		smax: [3]float32{52, 102, 52}, // x: 50 + 1 + 1 ; y: 100 + 1 + 1 ; z: 50 + 1 + 1
	}
	if smin != want.smin || smax != want.smax {
		t.Errorf("got (%v, %v) want (%v, %v)", smin, smax, want.smin, want.smax)
	}
}

// --- MissileMonsterBounds drift detector ---------------------------------

func TestMissileMonsterBounds_TyrquakeValues(t *testing.T) {
	if MissileMonsterBounds.Mins != [3]float32{-15, -15, -15} {
		t.Errorf("Mins drift: %v", MissileMonsterBounds.Mins)
	}
	if MissileMonsterBounds.Maxs != [3]float32{15, 15, 15} {
		t.Errorf("Maxs drift: %v", MissileMonsterBounds.Maxs)
	}
}

// --- ClipToTarget --------------------------------------------------------

// Trace through empty space: no target overlap, clean Fraction=1.
func TestClipToTarget_CleanThroughBBoxTarget(t *testing.T) {
	target := Target{
		Origin: [3]float32{1000, 0, 0}, // far away
		Mins:   [3]float32{-1, -1, -1},
		Maxs:   [3]float32{1, 1, 1},
		Solid:  server.SolidBBox,
	}
	tr, err := ClipToTarget(target,
		[3]float32{0, 0, 0},                      // start
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, // test mins, maxs
		[3]float32{10, 0, 0}) // end
	if err != nil {
		t.Fatal(err)
	}
	if tr.Fraction != 1.0 {
		t.Errorf("clean trace: got Fraction=%v want 1", tr.Fraction)
	}
	if tr.EndPos != [3]float32{10, 0, 0} {
		t.Errorf("clean trace: EndPos got %v want (10,0,0)", tr.EndPos)
	}
}

// Trace into a target: Fraction < 1, EndPos translated back to
// world coordinates from hull-local.
func TestClipToTarget_ImpactWithBBoxTarget(t *testing.T) {
	target := Target{
		Origin: [3]float32{50, 0, 0},
		Mins:   [3]float32{-10, -10, -10},
		Maxs:   [3]float32{10, 10, 10},
		Solid:  server.SolidBBox,
	}
	// Point-test-object (mins=maxs=0) traveling from x=-1000 to x=1000.
	// Boxhull built with hullmins = (-10..10) - (0..0) = (-10..10).
	// World x=40 is the -X face of the target (origin 50, -10) and that's
	// where the trace should first impact, pulled in by DistEpsilon.
	tr, err := ClipToTarget(target,
		[3]float32{-1000, 0, 0},                  // start
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, // test mins, maxs
		[3]float32{1000, 0, 0}) // end
	if err != nil {
		t.Fatal(err)
	}
	if tr.Fraction >= 1.0 {
		t.Errorf("expected impact, Fraction=%v", tr.Fraction)
	}
	// EndPos should be near x=40 (the -X face of the target at origin x=50,
	// pulled in by DistEpsilon).
	if tr.EndPos[0] > 41 || tr.EndPos[0] < 39 {
		t.Errorf("EndPos.x near 40: got %v", tr.EndPos)
	}
}

// SOLID_BSP target requires a BrushModel -> error propagates.
func TestClipToTarget_BSPNilBrushModelErrors(t *testing.T) {
	target := Target{Solid: server.SolidBSP} // no BrushModel
	_, err := ClipToTarget(target,
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{0, 0, 0}, [3]float32{1, 0, 0})
	if err == nil {
		t.Error("expected error for SOLID_BSP with nil BrushModel")
	}
}

// bsptrace.TraceHull error propagates (corrupt hull).
func TestClipToTarget_BSPTraceErrorPropagates(t *testing.T) {
	bm := &model.BrushModel{}
	// All 4 hulls corrupt; first one (idx 0) is what point-size dispatch picks.
	corrupt := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm.Hulls[0] = corrupt
	bm.Hulls[1] = corrupt
	bm.Hulls[2] = corrupt
	target := Target{
		Origin:     [3]float32{0, 0, 0},
		Solid:      server.SolidBSP,
		BrushModel: bm,
	}
	_, err := ClipToTarget(target,
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{-1000, 0, 0}, [3]float32{1000, 0, 0})
	if err == nil {
		t.Error("expected error from corrupt hull")
	}
}

// --- TraceMove ----------------------------------------------------------

// Build a tiny world brushmodel whose hull 0 is "all empty" so
// world-trace never clips.
func makeEmptyWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// Build a world brushmodel whose hull 0 is "+x solid, -x empty" so
// traces from x<0 toward x>0 impact at x=0.
func makeWorldWithWall() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// Clean trace through empty world, no candidates: Fraction=1,
// !WorldClipped, EntityIdx=-1.
func TestTraceMove_CleanEmptyWorld(t *testing.T) {
	res, err := TraceMove(makeEmptyWorld(), nil,
		[3]float32{-10, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{-5, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Trace.Fraction != 1.0 {
		t.Errorf("clean trace: Fraction=%v want 1", res.Trace.Fraction)
	}
	if res.WorldClipped {
		t.Error("clean trace should not WorldClipped")
	}
	if res.EntityIdx != -1 {
		t.Errorf("EntityIdx: got %d want -1", res.EntityIdx)
	}
}

// World-only clip: WorldClipped=true, EntityIdx=-1.
func TestTraceMove_WorldOnlyClip(t *testing.T) {
	res, err := TraceMove(makeWorldWithWall(), nil,
		[3]float32{-10, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{10, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Trace.Fraction >= 1.0 {
		t.Errorf("expected world impact, Fraction=%v", res.Trace.Fraction)
	}
	if !res.WorldClipped {
		t.Error("WorldClipped should be true")
	}
	if res.EntityIdx != -1 {
		t.Errorf("EntityIdx: got %d want -1 (world-only clip)", res.EntityIdx)
	}
}

// Single candidate that clips: EntityIdx=0.
func TestTraceMove_CandidateClipsBeforeWorld(t *testing.T) {
	candidate := Target{
		Origin: [3]float32{20, 0, 0}, // closer than the +x wall at x=0 if traced from x=100
		Mins:   [3]float32{-5, -5, -5},
		Maxs:   [3]float32{5, 5, 5},
		Solid:  server.SolidBBox,
	}
	// Trace from x=100 toward x=-100. World wall at x=0 hits at fraction ~0.5;
	// candidate at x=20 hits at fraction ~0.4 (closer).
	res, err := TraceMove(makeWorldWithWall(), []Target{candidate},
		[3]float32{100, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{-100, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Trace.Fraction >= 0.5 {
		t.Errorf("candidate (closer) should win: Fraction=%v", res.Trace.Fraction)
	}
	if res.EntityIdx != 0 {
		t.Errorf("EntityIdx: got %d want 0", res.EntityIdx)
	}
}

// World clips closer than candidate -> WorldClipped=true, EntityIdx=-1.
func TestTraceMove_WorldClipsCloserThanCandidate(t *testing.T) {
	candidate := Target{
		Origin: [3]float32{50, 0, 0}, // FAR from origin, world wall is closer
		Mins:   [3]float32{-5, -5, -5},
		Maxs:   [3]float32{5, 5, 5},
		Solid:  server.SolidBBox,
	}
	// Trace from x=-10 toward x=100. World wall at x=0 hits FIRST.
	res, err := TraceMove(makeWorldWithWall(), []Target{candidate},
		[3]float32{-10, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{100, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if !res.WorldClipped {
		t.Error("WorldClipped should be true")
	}
	if res.EntityIdx != -1 {
		t.Errorf("EntityIdx: got %d want -1 (world wins)", res.EntityIdx)
	}
}

// Multiple candidates -- the one hit FIRST along the trace (lower
// Fraction) wins regardless of iteration order. Put the late-impact
// candidate first in the slice; the early-impact candidate should
// still take EntityIdx.
func TestTraceMove_EarliestImpactWins(t *testing.T) {
	// Trace from x=0 toward x=100. lateImpact is at x=80, earlyImpact at x=20.
	lateImpact := Target{
		Origin: [3]float32{80, 0, 0},
		Mins:   [3]float32{-5, -5, -5}, Maxs: [3]float32{5, 5, 5},
		Solid: server.SolidBBox,
	}
	earlyImpact := Target{
		Origin: [3]float32{20, 0, 0},
		Mins:   [3]float32{-5, -5, -5}, Maxs: [3]float32{5, 5, 5},
		Solid: server.SolidBBox,
	}
	// Order: late first, then early. earlyImpact should still take EntityIdx.
	res, err := TraceMove(makeEmptyWorld(), []Target{lateImpact, earlyImpact},
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{100, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.EntityIdx != 1 {
		t.Errorf("EntityIdx: got %d want 1 (earlyImpact)", res.EntityIdx)
	}
}

// AllSolid early-exit: iteration stops after the trace flips AllSolid.
// Build a world that makes the trace AllSolid (start IN solid) +
// add a sentinel candidate that, if iterated, would set a distinct
// EntityIdx -- but the early-exit should leave EntityIdx=-1.
func TestTraceMove_AllSolidEarlyExit(t *testing.T) {
	// World: both children SOLID -> any trace through it is AllSolid.
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsSolid}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	sentinel := Target{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-1, -1, -1}, Maxs: [3]float32{1, 1, 1},
		Solid: server.SolidBBox,
	}
	res, err := TraceMove(bm, []Target{sentinel},
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{1, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Trace.AllSolid {
		t.Error("expected AllSolid trace from solid world")
	}
	// Sentinel should NOT have been iterated -> EntityIdx stays -1
	// (set only at TraceResult init since world clip doesn't set
	// EntityIdx).
	if res.EntityIdx != -1 {
		t.Errorf("EntityIdx after AllSolid early-exit: got %d want -1", res.EntityIdx)
	}
}

// World error propagates (corrupt world hull).
func TestTraceMove_WorldErrorPropagates(t *testing.T) {
	bm := &model.BrushModel{}
	corrupt := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm.Hulls[0] = corrupt
	bm.Hulls[1] = corrupt
	bm.Hulls[2] = corrupt
	_, err := TraceMove(bm, nil,
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{10, 0, 0})
	if err == nil {
		t.Error("expected world-hull error")
	}
}

// Per-candidate error propagates.
func TestTraceMove_CandidateErrorPropagates(t *testing.T) {
	// World is fine.
	wm := makeEmptyWorld()
	// Candidate is SOLID_BSP with nil BrushModel -> ErrSolidBSPNeedsBrushModel.
	badCandidate := Target{
		Origin: [3]float32{0, 0, 0},
		Solid:  server.SolidBSP,
	}
	_, err := TraceMove(wm, []Target{badCandidate},
		[3]float32{-10, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{-5, 0, 0})
	if err == nil {
		t.Error("expected candidate error to propagate")
	}
}

// StartSolid merge-only path: a per-candidate trace sets StartSolid
// but doesn't change the running trace's Fraction or AllSolid.
// Construct: world is empty (clean trace, Fraction=1). Candidate
// contains the start point in solid (Origin+bbox covers start) but
// the trace direction exits the candidate before any closer impact
// than world. The candidate's stack.StartSolid=true but
// stack.AllSolid=false and stack.Fraction is not less than 1.
//
// For a candidate box at origin (0,0,0) +-5, start at (0,0,0) (in
// solid) and end at (100,0,0). The boxhull trace will fire StartSolid
// (start in solid) but the trace eventually exits and Fraction is
// fractional (e.g. 0.05) -- which IS less than the running 1.0, so
// EntityIdx gets set.
//
// To exercise the merge-only path we need: stack.StartSolid=true
// AND stack.AllSolid=false AND stack.Fraction == 1.0. That happens
// when the test object starts in solid but the trace endpoint is
// inside the solid still (so the trace never exits). But then
// stack.AllSolid would typically be true.
//
// Honest answer: the merge-only path is hard to hit naturally with a
// boxhull because BSP straddle traces flip AllSolid to false the
// moment they see an EMPTY leaf. Construct a target whose boxhull
// passes through start-in-solid but the trace end is also in solid
// (so AllSolid stays true, Fraction stays 1.0).
//
// Actually simpler: ContentsSolid both sides -> StartSolid=true,
// AllSolid=true at every step, Fraction=1.0 (the trace never exits).
// That doesn't match the merge-only path predicate either (AllSolid
// is true).
//
// Looking at the merge predicate: stack.AllSolid || stack.StartSolid ||
// stack.Fraction < trace.Fraction. The "else if stack.StartSolid"
// branch fires when NONE of the three apply individually -- wait, it
// fires when the FIRST branch DOESN'T (so !(AllSolid || StartSolid ||
// Fraction<trace.Fraction)) but stack.StartSolid is true. That's
// contradictory -- if StartSolid then the first branch IS taken.
//
// So the merge-only "else if" branch is DEAD CODE in the C upstream.
// (Confirmed by inspection: the OR clause includes StartSolid, so
// any StartSolid case goes through the first branch.) The Go port
// inherits this dead branch. Document it.
//
// Per the bsptrace project standard (drop unreachable code), the
// next iteration could remove the else-if. For now, keep + cover
// it with an explicit no-op test that's documented.
//
// Actually, we CAN'T cover the dead branch -- by construction it's
// unreachable. So we have to either remove it (drop dead code, as
// per the bsptrace pattern) or carry a coverage exception.
// Decision: remove the dead branch in trace.go.

// We don't test the dead branch -- it's removed in trace.go.

// EntityIdx tracking: the C upstream sets clipent on any "interesting"
// stacktrace, but in the Go port we set EntityIdx ONLY when the
// per-target trace becomes the running trace. Sanity-check the
// "stack.AllSolid" path: trace through a candidate that's allsolid
// -> EntityIdx gets set.
func TestTraceMove_CandidateAllSolidSetsEntityIdx(t *testing.T) {
	// Candidate whose box trace produces stack.AllSolid (both leaves
	// solid). Build a target with bounds that include the entire
	// trace, so the boxhull is huge and the trace stays inside it.
	huge := Target{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-1000, -1000, -1000},
		Maxs:   [3]float32{1000, 1000, 1000},
		Solid:  server.SolidBBox,
	}
	res, err := TraceMove(makeEmptyWorld(), []Target{huge},
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{1, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.EntityIdx != 0 {
		t.Errorf("AllSolid candidate should set EntityIdx: got %d want 0", res.EntityIdx)
	}
}

// Running trace's StartSolid is preserved when a closer impact lands.
// Two candidates: first starts the test in solid (StartSolid=true,
// AllSolid=false), second clips at a normal impact. The merged
// trace should keep StartSolid=true (preserve the prior bit) AND
// take the second candidate's impact.
func TestTraceMove_PreservesPriorStartSolid(t *testing.T) {
	// First candidate: contains start point (sets StartSolid).
	startInSolid := Target{
		Origin: [3]float32{0, 0, 0},
		Mins:   [3]float32{-5, -5, -5}, Maxs: [3]float32{5, 5, 5},
		Solid: server.SolidBBox,
	}
	// Second candidate: small box at the trace end (impact further along).
	farImpact := Target{
		Origin: [3]float32{100, 0, 0},
		Mins:   [3]float32{-2, -2, -2}, Maxs: [3]float32{2, 2, 2},
		Solid: server.SolidBBox,
	}
	res, err := TraceMove(makeEmptyWorld(), []Target{startInSolid, farImpact},
		[3]float32{0, 0, 0}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0},
		[3]float32{200, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Trace.StartSolid {
		t.Error("expected StartSolid preserved across candidate iteration")
	}
}
