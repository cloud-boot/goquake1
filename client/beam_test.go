// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"math"
	"testing"
)

func TestNewBeamPool_AllDead(t *testing.T) {
	p := NewBeamPool()
	if got := p.NumAlive(0); got != 0 {
		t.Fatalf("NumAlive on fresh pool = %d want 0", got)
	}
}

func TestBeamPool_Spawn_Basic(t *testing.T) {
	p := NewBeamPool()
	idx := p.Spawn(int(TELightning2), 7,
		[3]float32{0, 0, 0}, [3]float32{300, 0, 0}, 1.0)
	if idx != 0 {
		t.Fatalf("first spawn idx = %d want 0", idx)
	}
	s := p.Slots[0]
	if !s.Alive {
		t.Fatalf("slot Alive = false")
	}
	if s.Kind != int(TELightning2) {
		t.Fatalf("Kind = %d want %d", s.Kind, int(TELightning2))
	}
	if s.EntityNum != 7 {
		t.Fatalf("EntityNum = %d want 7", s.EntityNum)
	}
	if s.SpawnTime != 1.0 {
		t.Fatalf("SpawnTime = %v want 1.0", s.SpawnTime)
	}
	if s.Start != [3]float32{0, 0, 0} {
		t.Fatalf("Start = %v want zero", s.Start)
	}
	if s.End != [3]float32{300, 0, 0} {
		t.Fatalf("End = %v want {300,0,0}", s.End)
	}
}

func TestBeamPool_Spawn_SameKindEntReusesSlot(t *testing.T) {
	p := NewBeamPool()
	first := p.Spawn(int(TELightning2), 5,
		[3]float32{0, 0, 0}, [3]float32{10, 0, 0}, 1.0)
	second := p.Spawn(int(TELightning2), 5,
		[3]float32{1, 2, 3}, [3]float32{20, 0, 0}, 1.1)
	if first != second {
		t.Fatalf("re-spawn idx = %d want %d (same slot)", second, first)
	}
	s := p.Slots[first]
	if s.SpawnTime != 1.1 {
		t.Fatalf("SpawnTime = %v want 1.1 (extended)", s.SpawnTime)
	}
	if s.Start != [3]float32{1, 2, 3} {
		t.Fatalf("Start = %v want {1,2,3}", s.Start)
	}
}

func TestBeamPool_Spawn_DifferentKindNewSlot(t *testing.T) {
	p := NewBeamPool()
	a := p.Spawn(int(TELightning2), 5,
		[3]float32{}, [3]float32{10, 0, 0}, 1.0)
	b := p.Spawn(int(TELightning1), 5,
		[3]float32{}, [3]float32{10, 0, 0}, 1.0)
	if a == b {
		t.Fatalf("different kind reused slot %d", a)
	}
}

func TestBeamPool_Spawn_DifferentEntNewSlot(t *testing.T) {
	p := NewBeamPool()
	a := p.Spawn(int(TELightning2), 5,
		[3]float32{}, [3]float32{10, 0, 0}, 1.0)
	b := p.Spawn(int(TELightning2), 6,
		[3]float32{}, [3]float32{10, 0, 0}, 1.0)
	if a == b {
		t.Fatalf("different ent reused slot %d", a)
	}
}

func TestBeamPool_NumAlive_RespectsLifetime(t *testing.T) {
	p := NewBeamPool()
	p.Spawn(int(TELightning2), 1,
		[3]float32{}, [3]float32{30, 0, 0}, 10)
	if got := p.NumAlive(10); got != 1 {
		t.Fatalf("at spawn time NumAlive = %d want 1", got)
	}
	if got := p.NumAlive(10 + BeamLifetime/2); got != 1 {
		t.Fatalf("mid-life NumAlive = %d want 1", got)
	}
	// Past-expiry: NumAlive must report 0. The exact at-expiry edge is
	// `now-SpawnTime < BeamLifetime` which can flicker by one float ULP
	// either side; +0.1 is well clear of the boundary.
	if got := p.NumAlive(10 + BeamLifetime + 0.1); got != 0 {
		t.Fatalf("past-expiry NumAlive = %d want 0", got)
	}
}

func TestBeamPool_NumAlive_SkipsDeadSlots(t *testing.T) {
	p := NewBeamPool()
	// One dead slot interleaved with one live; NumAlive must skip it.
	if got := p.NumAlive(0); got != 0 {
		t.Fatalf("empty NumAlive = %d want 0", got)
	}
	p.Spawn(int(TELightning2), 1,
		[3]float32{}, [3]float32{30, 0, 0}, 0)
	// Mutate a *different* slot to confirm the !Alive continue branch.
	if p.Slots[1].Alive {
		t.Fatalf("slot 1 unexpectedly alive")
	}
	if got := p.NumAlive(0); got != 1 {
		t.Fatalf("NumAlive = %d want 1", got)
	}
}

func TestBeamPool_Walk_AgesOutAndInvokesPerSegment(t *testing.T) {
	p := NewBeamPool()
	// 90-unit beam = 3 segments of 30 units each.
	p.Spawn(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{90, 0, 0}, 0)
	calls := 0
	p.Walk(BeamLifetime/2, func(seg BeamSegment) {
		if seg.Total != 3 {
			t.Errorf("Total = %d want 3", seg.Total)
		}
		if seg.Kind != int(TELightning2) {
			t.Errorf("Kind = %d want %d", seg.Kind, int(TELightning2))
		}
		calls++
	})
	if calls != 3 {
		t.Fatalf("calls @ live time = %d want 3", calls)
	}
	if !p.Slots[0].Alive {
		t.Fatalf("Walk retired a live slot")
	}
	calls = 0
	p.Walk(BeamLifetime+0.01, func(seg BeamSegment) { calls++ })
	if calls != 0 {
		t.Fatalf("calls @ expired = %d want 0", calls)
	}
	if p.Slots[0].Alive {
		t.Fatalf("Walk did NOT retire an expired slot")
	}
}

func TestBeamPool_Walk_NilCallback(t *testing.T) {
	p := NewBeamPool()
	p.Spawn(int(TELightning2), 1,
		[3]float32{}, [3]float32{30, 0, 0}, 0)
	// nil draw: still ages out expired slots, no panic.
	p.Walk(BeamLifetime+0.5, nil)
	if p.Slots[0].Alive {
		t.Fatalf("nil-draw Walk didn't retire expired slot")
	}
}

func TestBeamPool_Walk_NilCallbackOnLiveBeamSkipsSegments(t *testing.T) {
	p := NewBeamPool()
	p.Spawn(int(TELightning2), 1,
		[3]float32{}, [3]float32{30, 0, 0}, 0)
	// nil draw on a LIVE beam takes the "draw == nil continue" branch
	// before the segment loop; no panic, slot stays alive.
	p.Walk(BeamLifetime/2, nil)
	if !p.Slots[0].Alive {
		t.Fatalf("nil-draw Walk on live beam retired the slot")
	}
}

func TestBeamPool_Spawn_ReusesDeadSlots(t *testing.T) {
	p := NewBeamPool()
	p.Spawn(int(TELightning2), 1,
		[3]float32{}, [3]float32{30, 0, 0}, 0)
	p.Spawn(int(TELightning1), 2,
		[3]float32{}, [3]float32{30, 0, 0}, 0)
	// Expire slot 0 + slot 1 via Walk @ past-lifetime.
	p.Walk(BeamLifetime+0.5, nil)
	if p.Slots[0].Alive || p.Slots[1].Alive {
		t.Fatalf("slots not aged out: %v %v", p.Slots[0].Alive, p.Slots[1].Alive)
	}
	idx := p.Spawn(int(TELightning3), 9,
		[3]float32{}, [3]float32{30, 0, 0}, 1.0)
	if idx != 0 {
		t.Fatalf("reuse idx = %d want 0", idx)
	}
}

func TestBeamPool_Spawn_EvictsOldestWhenFull(t *testing.T) {
	p := NewBeamPool()
	// Fill every slot with a long lifetime so none expires.
	for i := 0; i < MaxBeams; i++ {
		p.Spawn(int(TELightning2), i+1,
			[3]float32{}, [3]float32{30, 0, 0}, float32(i)+1)
	}
	// Every slot alive; oldest is slot 0 (spawn time 1).
	// Spawn a *new* (kind, ent) pair so the first-pass extend doesn't
	// hit. Use 1000 as SpawnTime so it's clearly newer than slot 0.
	idx := p.Spawn(int(TELightning3), 999,
		[3]float32{1, 2, 3}, [3]float32{31, 2, 3}, 1000)
	if idx != 0 {
		t.Fatalf("eviction idx = %d want 0 (oldest)", idx)
	}
	if p.Slots[0].EntityNum != 999 {
		t.Fatalf("oldest slot not overwritten: ent=%d", p.Slots[0].EntityNum)
	}
	if p.Slots[0].SpawnTime != 1000 {
		t.Fatalf("evicted slot SpawnTime = %v want 1000", p.Slots[0].SpawnTime)
	}
}

// TestBeamPool_Spawn_EvictsTrueOldestNotFirstSlot exercises the
// in-loop `oldest = i` rebind: the first slot is the YOUNGEST and a
// later slot is the oldest. The eviction must target the later slot,
// proving the comparison arm runs.
func TestBeamPool_Spawn_EvictsTrueOldestNotFirstSlot(t *testing.T) {
	p := NewBeamPool()
	// Spawn slots with DESCENDING SpawnTime: slot 0 = newest,
	// slot MaxBeams-1 = oldest.
	for i := 0; i < MaxBeams; i++ {
		p.Spawn(int(TELightning2), i+1,
			[3]float32{}, [3]float32{30, 0, 0}, float32(MaxBeams-i))
	}
	last := MaxBeams - 1
	if p.Slots[last].SpawnTime != 1 {
		t.Fatalf("setup: last SpawnTime = %v want 1", p.Slots[last].SpawnTime)
	}
	idx := p.Spawn(int(TELightning3), 999,
		[3]float32{}, [3]float32{30, 0, 0}, 9999)
	if idx != last {
		t.Fatalf("evicted idx = %d want %d (true oldest)", idx, last)
	}
}

func TestBeamPool_Reset(t *testing.T) {
	p := NewBeamPool()
	for i := 0; i < 4; i++ {
		p.Spawn(int(TELightning2), i+1,
			[3]float32{}, [3]float32{30, 0, 0}, 0)
	}
	if p.NumAlive(0) != 4 {
		t.Fatalf("before reset NumAlive = %d want 4", p.NumAlive(0))
	}
	p.Reset()
	if p.NumAlive(0) != 0 {
		t.Fatalf("after reset NumAlive = %d want 0", p.NumAlive(0))
	}
	for i := range p.Slots {
		if p.Slots[i].Alive {
			t.Fatalf("slot %d still alive after Reset", i)
		}
	}
}

// --- BeamSegments helper ---------------------------------------------------

func TestBeamSegments_ZeroLengthIsNil(t *testing.T) {
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{5, 5, 5}, [3]float32{5, 5, 5})
	if got != nil {
		t.Fatalf("zero-length BeamSegments = %v want nil", got)
	}
}

func TestBeamSegments_NinetyUnits_PlusX(t *testing.T) {
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{90, 0, 0})
	if len(got) != 3 {
		t.Fatalf("len = %d want 3", len(got))
	}
	for i, s := range got {
		if s.Index != i {
			t.Errorf("seg[%d].Index = %d want %d", i, s.Index, i)
		}
		if s.Total != 3 {
			t.Errorf("seg[%d].Total = %d want 3", i, s.Total)
		}
		wantX := float32(i) * BeamSegmentLength
		if s.Origin[0] != wantX {
			t.Errorf("seg[%d].Origin[0] = %v want %v", i, s.Origin[0], wantX)
		}
		if s.Origin[1] != 0 || s.Origin[2] != 0 {
			t.Errorf("seg[%d].Origin off-axis: %v", i, s.Origin)
		}
	}
	// +X direction => yaw 0, pitch 0.
	if got[0].Yaw != 0 {
		t.Errorf("Yaw = %v want 0 (+X)", got[0].Yaw)
	}
	if got[0].Pitch != 0 {
		t.Errorf("Pitch = %v want 0 (+X)", got[0].Pitch)
	}
}

func TestBeamSegments_ShortBeamYieldsOneSegment(t *testing.T) {
	got := BeamSegments(int(TEBeam), 2,
		[3]float32{0, 0, 0}, [3]float32{10, 0, 0})
	if len(got) != 1 {
		t.Fatalf("len = %d want 1", len(got))
	}
}

func TestBeamSegments_ExactBoundary(t *testing.T) {
	// 60 units = 2 segments (length/30 = 2, no rounding).
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{60, 0, 0})
	if len(got) != 2 {
		t.Fatalf("len = %d want 2", len(got))
	}
}

func TestBeamSegments_PureUpYaw0Pitch90(t *testing.T) {
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{0, 0, 90})
	if len(got) == 0 {
		t.Fatalf("no segments")
	}
	if got[0].Yaw != 0 {
		t.Errorf("Yaw = %v want 0 (vertical)", got[0].Yaw)
	}
	if got[0].Pitch != 90 {
		t.Errorf("Pitch = %v want 90 (+Z)", got[0].Pitch)
	}
}

func TestBeamSegments_PureDownYaw0Pitch270(t *testing.T) {
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{0, 0, -90})
	if len(got) == 0 {
		t.Fatalf("no segments")
	}
	if got[0].Pitch != 270 {
		t.Errorf("Pitch = %v want 270 (-Z)", got[0].Pitch)
	}
}

func TestBeamSegments_NegYWrapsTo270(t *testing.T) {
	// -Y direction: atan2(-1, 0) = -90 degrees -> +360 = 270.
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{0, -90, 0})
	if len(got) == 0 {
		t.Fatalf("no segments")
	}
	if got[0].Yaw != 270 {
		t.Errorf("Yaw = %v want 270 (-Y)", got[0].Yaw)
	}
}

func TestBeamSegments_DownwardSlope_NegPitchWraps(t *testing.T) {
	// Flat-X + dz=-flat: atan2(-1,1) ~ -45 -> +360 = 315.
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{0, 0, 0}, [3]float32{60, 0, -60})
	if len(got) == 0 {
		t.Fatalf("no segments")
	}
	// Allow tiny float drift.
	if d := math.Abs(float64(got[0].Pitch - 315)); d > 1e-3 {
		t.Errorf("Pitch = %v want ~315", got[0].Pitch)
	}
}

func TestBeamSegments_CapsAtMaxSegments(t *testing.T) {
	// 100 segments worth of distance; must clamp at MaxBeamSegments.
	long := float32(MaxBeamSegments+50) * BeamSegmentLength
	got := BeamSegments(int(TELightning2), 1,
		[3]float32{}, [3]float32{long, 0, 0})
	if len(got) != MaxBeamSegments {
		t.Fatalf("len = %d want %d (cap)", len(got), MaxBeamSegments)
	}
}

// --- Apply arm for lightning kinds -----------------------------------------

func TestApply_TempEntity_Lightning_RoutesToEmitBeam(t *testing.T) {
	s := NewState()
	var seen struct {
		kind, ent  int
		start, end [3]float32
		count      int
	}
	s.EmitBeam = func(kind, ent int, start, end [3]float32) {
		seen.kind = kind
		seen.ent = ent
		seen.start = start
		seen.end = end
		seen.count++
	}
	// EmitTempEntity should NOT fire for lightning kinds.
	tePointHits := 0
	s.EmitTempEntity = func(kind int, origin [3]float32) {
		tePointHits++
	}
	for _, k := range []TempEntityKind{TELightning1, TELightning2, TELightning3, TEBeam} {
		msg := DecodedTempEntity{
			Kind:      k,
			EntityNum: 7,
			Start:     [3]float32{1, 2, 3},
			End:       [3]float32{4, 5, 6},
		}
		if err := Apply(s, msg, 1.0); err != nil {
			t.Fatalf("Apply kind=%v: %v", k, err)
		}
	}
	if seen.count != 4 {
		t.Fatalf("EmitBeam fired %d times want 4", seen.count)
	}
	if tePointHits != 0 {
		t.Fatalf("EmitTempEntity fired %d times for lightning kinds; want 0", tePointHits)
	}
	if seen.kind != int(TEBeam) || seen.ent != 7 {
		t.Fatalf("last call kind=%d ent=%d", seen.kind, seen.ent)
	}
	if seen.start != ([3]float32{1, 2, 3}) || seen.end != ([3]float32{4, 5, 6}) {
		t.Fatalf("start/end mismatch: %v -> %v", seen.start, seen.end)
	}
}

func TestApply_TempEntity_Lightning_NilSink_NoPanic(t *testing.T) {
	s := NewState()
	// Both sinks nil: lightning kind must early-return without panic.
	msg := DecodedTempEntity{
		Kind:      TELightning2,
		EntityNum: 1,
		Start:     [3]float32{0, 0, 0},
		End:       [3]float32{10, 0, 0},
	}
	if err := Apply(s, msg, 1.0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.MsgTime != 1.0 {
		t.Fatalf("MsgTime = %v want 1.0", s.MsgTime)
	}
}
