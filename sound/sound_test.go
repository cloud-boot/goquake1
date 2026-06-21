// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sound

import (
	"errors"
	"testing"
)

// ----- Channel ----------------------------------------------------

func TestChannel_FreeWhenSfxNil(t *testing.T) {
	var c Channel
	if !c.Free() {
		t.Fatalf("default channel not free")
	}
}

func TestChannel_NotFreeWhenPlaying(t *testing.T) {
	c := Channel{Sfx: &Sample{}}
	if c.Free() {
		t.Fatalf("playing channel reported free")
	}
}

func TestChannel_Stop(t *testing.T) {
	c := Channel{
		Sfx:      &Sample{},
		Position: 100,
		EndPos:   200,
		LeftVol:  128,
		RightVol: 128,
	}
	c.Stop()
	if !c.Free() {
		t.Fatalf("Stop did not free channel")
	}
	if c.Position != 0 || c.EndPos != 0 || c.LeftVol != 0 || c.RightVol != 0 {
		t.Fatalf("Stop did not zero fields: %+v", c)
	}
}

// ----- NewPool -----------------------------------------------------

func TestNewPool_Happy(t *testing.T) {
	p, err := NewPool(8)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	if p.ReservedStatic != 8 {
		t.Fatalf("ReservedStatic = %d want 8", p.ReservedStatic)
	}
	if p.ActiveCount() != 0 {
		t.Fatalf("fresh pool ActiveCount = %d want 0", p.ActiveCount())
	}
}

func TestNewPool_BadReserve(t *testing.T) {
	for _, n := range []int{-1, MaxChannels + 1, MaxChannels + 100} {
		_, err := NewPool(n)
		if !errors.Is(err, ErrPoolBadReserve) {
			t.Fatalf("NewPool(%d) err = %v want ErrPoolBadReserve", n, err)
		}
	}
}

func TestNewPool_AcceptsZero(t *testing.T) {
	if _, err := NewPool(0); err != nil {
		t.Fatalf("NewPool(0): %v", err)
	}
}

func TestNewPool_AcceptsMax(t *testing.T) {
	if _, err := NewPool(MaxChannels); err != nil {
		t.Fatalf("NewPool(MaxChannels): %v", err)
	}
}

// ----- Alloc -------------------------------------------------------

func TestAlloc_FirstSlot(t *testing.T) {
	p, _ := NewPool(8)
	idx, err := p.Alloc(1, 1)
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	if idx != 8 {
		t.Fatalf("first alloc idx = %d want 8 (ReservedStatic)", idx)
	}
}

func TestAlloc_ReuseSameEntityChannel(t *testing.T) {
	p, _ := NewPool(8)
	// Allocate + occupy slot 8 with ent=5, channel=2.
	p.Channels[8] = Channel{Sfx: &Sample{}, EntNum: 5, EntChannel: 2, EndPos: 1000}
	idx, err := p.Alloc(5, 2)
	if err != nil {
		t.Fatalf("Alloc reuse: %v", err)
	}
	if idx != 8 {
		t.Fatalf("reuse alloc idx = %d want 8", idx)
	}
}

func TestAlloc_ChannelZeroNoReuse(t *testing.T) {
	// EntChannel == 0 means "ambient" sounds that don't replace
	// each other -- Alloc should NOT reuse the same slot.
	p, _ := NewPool(8)
	p.Channels[8] = Channel{Sfx: &Sample{}, EntNum: 5, EntChannel: 0, EndPos: 1000}
	idx, err := p.Alloc(5, 0)
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	if idx == 8 {
		t.Fatalf("Alloc with channel=0 reused the slot")
	}
}

func TestAlloc_EvictionByRemaining(t *testing.T) {
	p, _ := NewPool(0) // no reserved, all 128 dynamic
	// Fill every slot. Give increasing remaining-samples so the
	// slot with the smallest remaining wins eviction.
	for i := 0; i < MaxChannels; i++ {
		p.Channels[i] = Channel{
			Sfx:      &Sample{},
			Position: 0,
			EndPos:   1000 + i*10, // slot 0 has lowest EndPos
			EntNum:   i,
			EntChannel: 1,
		}
	}
	idx, err := p.Alloc(999, 1) // new entity, no reuse match
	if err != nil {
		t.Fatalf("Alloc eviction: %v", err)
	}
	if idx != 0 {
		t.Fatalf("evicted slot = %d want 0 (lowest remaining)", idx)
	}
	if !p.Channels[0].Free() {
		t.Fatalf("evicted slot not freed")
	}
}

func TestAlloc_NoDynamicSlots(t *testing.T) {
	// ReservedStatic == MaxChannels -> zero dynamic slots; Alloc
	// must report ErrPoolNoFreeSlot (the eviction loop is empty).
	p, _ := NewPool(MaxChannels)
	idx, err := p.Alloc(1, 1)
	if !errors.Is(err, ErrPoolNoFreeSlot) {
		t.Fatalf("Alloc err = %v want ErrPoolNoFreeSlot", err)
	}
	if idx != FreeChannel {
		t.Fatalf("Alloc idx = %d want FreeChannel (%d)", idx, FreeChannel)
	}
}

// ----- StopAll + ActiveCount --------------------------------------

func TestStopAll(t *testing.T) {
	p, _ := NewPool(0)
	for i := 0; i < 5; i++ {
		p.Channels[i].Sfx = &Sample{}
	}
	if p.ActiveCount() != 5 {
		t.Fatalf("ActiveCount before stop = %d want 5", p.ActiveCount())
	}
	p.StopAll()
	if p.ActiveCount() != 0 {
		t.Fatalf("ActiveCount after stop = %d want 0", p.ActiveCount())
	}
}

func TestActiveCount_Mixed(t *testing.T) {
	p, _ := NewPool(0)
	p.Channels[0].Sfx = &Sample{}
	p.Channels[50].Sfx = &Sample{}
	p.Channels[100].Sfx = &Sample{}
	if got := p.ActiveCount(); got != 3 {
		t.Fatalf("ActiveCount = %d want 3", got)
	}
}

// ----- Drift detectors --------------------------------------------

func TestDefaultsAreSane(t *testing.T) {
	// Quake's hardcoded sample rate is 11025 Hz mono 8-bit.
	if DefaultSampleRate != 11025 || DefaultChannels != 1 || DefaultBitsPerSam != 8 {
		t.Fatalf("audio defaults drift: %d/%d/%d", DefaultSampleRate, DefaultChannels, DefaultBitsPerSam)
	}
	if MaxChannels != 128 {
		t.Fatalf("MaxChannels = %d want 128", MaxChannels)
	}
}
