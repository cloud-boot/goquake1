// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"testing"
)

func TestNewTempSpritePool_AllDead(t *testing.T) {
	p := NewTempSpritePool()
	if got := p.NumAlive(0); got != 0 {
		t.Fatalf("NumAlive on fresh pool = %d want 0", got)
	}
}

func TestTempSpritePool_Spawn_DefaultLifetime(t *testing.T) {
	p := NewTempSpritePool()
	idx := p.Spawn([3]float32{1, 2, 3}, 10, 0) // lifetime=0 -> default
	if idx != 0 {
		t.Fatalf("first spawn idx = %d want 0", idx)
	}
	s := p.Slots[0]
	if !s.Alive {
		t.Fatalf("slot Alive = false")
	}
	if s.Lifetime != TempSpriteLifetime {
		t.Fatalf("Lifetime = %v want %v", s.Lifetime, TempSpriteLifetime)
	}
	if s.SpawnTime != 10 {
		t.Fatalf("SpawnTime = %v want 10", s.SpawnTime)
	}
	if s.Origin != [3]float32{1, 2, 3} {
		t.Fatalf("Origin = %v want {1,2,3}", s.Origin)
	}
}

func TestTempSpritePool_Spawn_ExplicitLifetime(t *testing.T) {
	p := NewTempSpritePool()
	p.Spawn([3]float32{}, 0, 2.5)
	if p.Slots[0].Lifetime != 2.5 {
		t.Fatalf("Lifetime = %v want 2.5", p.Slots[0].Lifetime)
	}
}

func TestTempSpritePool_NumAlive_RespectsLifetime(t *testing.T) {
	p := NewTempSpritePool()
	p.Spawn([3]float32{}, 10, 1.0)
	if got := p.NumAlive(10); got != 1 {
		t.Fatalf("at spawn time NumAlive = %d want 1", got)
	}
	if got := p.NumAlive(10.5); got != 1 {
		t.Fatalf("mid-life NumAlive = %d want 1", got)
	}
	if got := p.NumAlive(11.0); got != 0 {
		t.Fatalf("at-expiry NumAlive = %d want 0", got)
	}
	if got := p.NumAlive(11.1); got != 0 {
		t.Fatalf("past-expiry NumAlive = %d want 0", got)
	}
}

func TestTempSpritePool_Walk_AgesOutSlots(t *testing.T) {
	p := NewTempSpritePool()
	p.Spawn([3]float32{}, 0, 0.5)
	calls := 0
	p.Walk(0.1, func(origin [3]float32, elapsed float32) {
		calls++
	})
	if calls != 1 {
		t.Fatalf("calls @ live time = %d want 1", calls)
	}
	if !p.Slots[0].Alive {
		t.Fatalf("Walk retired a live slot")
	}
	calls = 0
	p.Walk(0.6, func(origin [3]float32, elapsed float32) {
		calls++
	})
	if calls != 0 {
		t.Fatalf("calls @ expired = %d want 0", calls)
	}
	if p.Slots[0].Alive {
		t.Fatalf("Walk did NOT retire an expired slot")
	}
}

func TestTempSpritePool_Walk_NilCallback(t *testing.T) {
	p := NewTempSpritePool()
	p.Spawn([3]float32{}, 0, 0.5)
	// nil draw: still ages out expired slots, no panic.
	p.Walk(1.0, nil)
	if p.Slots[0].Alive {
		t.Fatalf("nil-draw Walk didn't retire expired slot")
	}
}

func TestTempSpritePool_Walk_PassesOriginAndElapsed(t *testing.T) {
	p := NewTempSpritePool()
	p.Spawn([3]float32{10, 20, 30}, 100, 1.0)
	var gotOrigin [3]float32
	var gotElapsed float32
	p.Walk(100.25, func(o [3]float32, e float32) {
		gotOrigin = o
		gotElapsed = e
	})
	if gotOrigin != [3]float32{10, 20, 30} {
		t.Fatalf("origin = %v want {10,20,30}", gotOrigin)
	}
	if gotElapsed != 0.25 {
		t.Fatalf("elapsed = %v want 0.25", gotElapsed)
	}
}

func TestTempSpritePool_Spawn_ReusesDeadSlots(t *testing.T) {
	p := NewTempSpritePool()
	// Fill slots 0 + 1, then expire slot 0 via Walk.
	p.Spawn([3]float32{}, 0, 0.1)
	p.Spawn([3]float32{}, 0, 1.0)
	p.Walk(0.5, nil) // retires slot 0 (lifetime 0.1)
	if p.Slots[0].Alive {
		t.Fatalf("slot 0 not aged out")
	}
	idx := p.Spawn([3]float32{}, 1.0, 0)
	if idx != 0 {
		t.Fatalf("reuse idx = %d want 0", idx)
	}
}

func TestTempSpritePool_Spawn_EvictsOldestWhenFull(t *testing.T) {
	p := NewTempSpritePool()
	// Fill every slot with a long lifetime so none expires.
	for i := 0; i < MaxTempSprites; i++ {
		p.Spawn([3]float32{}, float32(i)+1, 1000)
	}
	// Every slot alive; oldest is slot 0 (spawn time 1).
	idx := p.Spawn([3]float32{99, 99, 99}, 1000, 1000)
	if idx != 0 {
		t.Fatalf("eviction idx = %d want 0 (oldest)", idx)
	}
	if p.Slots[0].Origin != [3]float32{99, 99, 99} {
		t.Fatalf("oldest slot not overwritten: %v", p.Slots[0].Origin)
	}
	if p.Slots[0].SpawnTime != 1000 {
		t.Fatalf("evicted slot SpawnTime = %v want 1000", p.Slots[0].SpawnTime)
	}
}

// TestTempSpritePool_Spawn_EvictsTrueOldestNotFirstSlot exercises the
// in-loop `oldest = i` rebind: the first slot is the YOUNGEST and a
// later slot is the oldest. The eviction must target the later slot,
// proving the comparison arm runs.
func TestTempSpritePool_Spawn_EvictsTrueOldestNotFirstSlot(t *testing.T) {
	p := NewTempSpritePool()
	// Spawn slots with DESCENDING SpawnTime: slot 0 = newest,
	// slot MaxTempSprites-1 = oldest. Use a lifetime large enough
	// that no slot expires under the test horizon.
	for i := 0; i < MaxTempSprites; i++ {
		p.Spawn([3]float32{}, float32(MaxTempSprites-i), 1000)
	}
	// Slot 0 spawn time = MaxTempSprites; last slot spawn time = 1.
	last := MaxTempSprites - 1
	if p.Slots[last].SpawnTime != 1 {
		t.Fatalf("setup: last SpawnTime = %v want 1", p.Slots[last].SpawnTime)
	}
	idx := p.Spawn([3]float32{7, 8, 9}, 9999, 1000)
	if idx != last {
		t.Fatalf("evicted idx = %d want %d (true oldest)", idx, last)
	}
	if p.Slots[last].Origin != [3]float32{7, 8, 9} {
		t.Fatalf("true-oldest slot not overwritten: %v", p.Slots[last].Origin)
	}
}

func TestTempSpritePool_Reset(t *testing.T) {
	p := NewTempSpritePool()
	for i := 0; i < 4; i++ {
		p.Spawn([3]float32{}, 0, 100)
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
