// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"
)

func TestNewDLightPool(t *testing.T) {
	p := NewDLightPool()
	if p == nil {
		t.Fatalf("NewDLightPool returned nil")
	}
	if p.AliveCount(0) != 0 {
		t.Fatalf("fresh pool AliveCount = %d want 0", p.AliveCount(0))
	}
}

// ----- Alloc -------------------------------------------------------

func TestAlloc_FirstSlot(t *testing.T) {
	p := NewDLightPool()
	idx, err := p.Alloc(0, DLight{Radius: 100, Die: 10})
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	if idx != 0 {
		t.Fatalf("first alloc idx = %d want 0", idx)
	}
	if p.Lights[0].Radius != 100 {
		t.Fatalf("init not written")
	}
}

func TestAlloc_PoolFull(t *testing.T) {
	p := NewDLightPool()
	for i := 0; i < MaxDLights; i++ {
		if _, err := p.Alloc(0, DLight{Die: 100}); err != nil {
			t.Fatalf("Alloc %d: %v", i, err)
		}
	}
	if _, err := p.Alloc(0, DLight{Die: 100}); !errors.Is(err, ErrDLightNoSlot) {
		t.Fatalf("full-pool Alloc err = %v want ErrDLightNoSlot", err)
	}
}

func TestAlloc_ReuseExpiredSlot(t *testing.T) {
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 5, Radius: 50})
	// Advance time past slot 0's expiry; Alloc should reuse it.
	idx, err := p.Alloc(10, DLight{Die: 20, Radius: 100})
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	if idx != 0 {
		t.Fatalf("reuse idx = %d want 0", idx)
	}
}

// ----- AllocByKey --------------------------------------------------

func TestAllocByKey_NewKey(t *testing.T) {
	p := NewDLightPool()
	idx, err := p.AllocByKey(0, DLight{Key: 42, Die: 10})
	if err != nil {
		t.Fatalf("AllocByKey: %v", err)
	}
	if idx != 0 {
		t.Fatalf("new-key alloc idx = %d want 0", idx)
	}
}

func TestAllocByKey_OverwriteSameKey(t *testing.T) {
	p := NewDLightPool()
	_, _ = p.AllocByKey(0, DLight{Key: 42, Die: 10, Radius: 100})
	// Same key again -> should overwrite slot 0 (not allocate slot 1).
	idx, err := p.AllocByKey(0, DLight{Key: 42, Die: 20, Radius: 200})
	if err != nil {
		t.Fatalf("AllocByKey: %v", err)
	}
	if idx != 0 {
		t.Fatalf("same-key idx = %d want 0", idx)
	}
	if p.Lights[0].Radius != 200 {
		t.Fatalf("overwrite did not happen: radius = %v", p.Lights[0].Radius)
	}
}

func TestAllocByKey_ZeroKeyRejected(t *testing.T) {
	p := NewDLightPool()
	idx, err := p.AllocByKey(0, DLight{Key: 0, Die: 10})
	if !errors.Is(err, ErrDLightNoSlot) {
		t.Fatalf("AllocByKey(key=0) err = %v want ErrDLightNoSlot", err)
	}
	if idx != -1 {
		t.Fatalf("AllocByKey(key=0) idx = %d want -1", idx)
	}
}

func TestAllocByKey_FullPoolNoSameKey(t *testing.T) {
	p := NewDLightPool()
	for i := 0; i < MaxDLights; i++ {
		_, _ = p.AllocByKey(0, DLight{Key: i + 1, Die: 100})
	}
	// No slot has key=9999; pool is full -> Alloc returns ErrDLightNoSlot
	idx, err := p.AllocByKey(0, DLight{Key: 9999, Die: 100})
	if !errors.Is(err, ErrDLightNoSlot) {
		t.Fatalf("err = %v want ErrDLightNoSlot", err)
	}
	if idx != -1 {
		t.Fatalf("idx = %d want -1", idx)
	}
}

// ----- AnimateLights -----------------------------------------------

func TestAnimateLights_RadiusDecreases(t *testing.T) {
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 100, Radius: 100, Decay: 10})
	p.AnimateLights(0, 1) // dt=1, Decay=10 -> radius -= 10
	if p.Lights[0].Radius != 90 {
		t.Fatalf("radius after animate = %v want 90", p.Lights[0].Radius)
	}
}

func TestAnimateLights_ExpiredSkipped(t *testing.T) {
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 5, Radius: 100, Decay: 10})
	// At now=10 the slot is already expired (Die <= now). Animate
	// must skip it (no further radius decay).
	p.AnimateLights(10, 1)
	if p.Lights[0].Radius != 100 {
		t.Fatalf("expired-slot radius decayed: %v", p.Lights[0].Radius)
	}
}

func TestAnimateLights_DecaysToZero(t *testing.T) {
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 100, Radius: 5, Decay: 10})
	p.AnimateLights(0, 1) // radius 5 - 10 = -5 -> floor 0 -> expire
	if p.Lights[0].Radius != 0 {
		t.Fatalf("expired radius = %v want 0", p.Lights[0].Radius)
	}
	if p.Lights[0].Die > 0 {
		t.Fatalf("Die not pulled back: %v", p.Lights[0].Die)
	}
}

func TestAnimateLights_NegativeMinLightClamps(t *testing.T) {
	// MinLight negative -> floor clamps to 0; radius reaching 0 expires.
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 100, Radius: 5, Decay: 10, MinLight: -5})
	p.AnimateLights(0, 1)
	if p.Lights[0].Radius != 0 {
		t.Fatalf("expired radius = %v want 0", p.Lights[0].Radius)
	}
}

func TestAnimateLights_MinLightFloor(t *testing.T) {
	// MinLight=15 -> light expires when radius drops to <= 15.
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 100, Radius: 20, Decay: 10, MinLight: 15})
	p.AnimateLights(0, 1) // 20 - 10 = 10, below MinLight=15 -> expire
	if p.Lights[0].Die > 0 {
		t.Fatalf("MinLight expire failed: Die = %v", p.Lights[0].Die)
	}
}

// ----- AliveCount --------------------------------------------------

func TestAliveCount(t *testing.T) {
	p := NewDLightPool()
	_, _ = p.Alloc(0, DLight{Die: 100})
	_, _ = p.Alloc(0, DLight{Die: 100})
	_, _ = p.Alloc(0, DLight{Die: 5})
	if got := p.AliveCount(10); got != 2 {
		t.Fatalf("AliveCount(10) = %d want 2", got)
	}
}

func TestDLightConstants(t *testing.T) {
	if MaxDLights != 32 {
		t.Fatalf("MaxDLights drift: %d want 32", MaxDLights)
	}
}
