// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"errors"
	"math"
	"testing"
)

// progsForArena returns a stub Progs with the given EntityFields
// count and a string table sufficient for the edict tests below.
func progsForArena(entityFields int32) *Progs {
	return &Progs{
		Header:  Header{EntityFields: entityFields},
		Strings: []byte("\x00field1\x00origin\x00health\x00classname\x00ammo\x00"),
	}
}

func TestNewEdictArena_AllocatesFieldBlocks(t *testing.T) {
	p := progsForArena(5) // 5 fields * 4 bytes = 20-byte block
	a := NewEdictArena(p, 4)
	if a.Cap() != 4 {
		t.Errorf("Cap: %d", a.Cap())
	}
	for i := 0; i < a.Cap(); i++ {
		e, _ := a.Get(i)
		if len(e.Fields) != 20 {
			t.Errorf("slot %d field len: %d", i, len(e.Fields))
		}
	}
}

func TestNewEdictArena_MinCapOne(t *testing.T) {
	// Cap<1 silently bumps to 1 -- the world slot must exist.
	a := NewEdictArena(progsForArena(1), 0)
	if a.Cap() != 1 {
		t.Errorf("min cap: got %d", a.Cap())
	}
}

func TestNewEdictArena_NilProgsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil progs")
		}
	}()
	NewEdictArena(nil, 4)
}

func TestEdictArena_GetOutOfRange(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 4)
	if _, err := a.Get(-1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("Get(-1): %v", err)
	}
	if _, err := a.Get(100); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("Get(100): %v", err)
	}
}

func TestEdictArena_AllocFreeRoundTrip(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 4)
	// Mark slots 1..3 free so Alloc can use them.
	a.Reset()
	e, idx, err := a.Alloc()
	if err != nil || e == nil || idx != 1 {
		t.Fatalf("Alloc: e=%v idx=%d err=%v", e, idx, err)
	}
	if e.Free {
		t.Error("Alloc'd edict still Free")
	}
	if a.Count() != 1 {
		t.Errorf("Count after Alloc: %d", a.Count())
	}
	a.Free(e, 1.5)
	if !e.Free || e.FreeTime != 1.5 {
		t.Errorf("Free: %+v", e)
	}
	if a.Count() != 0 {
		t.Errorf("Count after Free: %d", a.Count())
	}
}

func TestEdictArena_AllocExhaustion(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 3) // world + 2 game slots
	a.Reset()
	if _, _, err := a.Alloc(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Alloc(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Alloc(); !errors.Is(err, ErrArenaFull) {
		t.Errorf("third Alloc: got %v want ErrArenaFull", err)
	}
}

func TestEdictArena_AllocReturnsFirstFreeSkipsWorld(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 4)
	a.Reset()
	_, idx, _ := a.Alloc()
	if idx != 1 {
		t.Errorf("Alloc must skip world (0); got %d", idx)
	}
}

func TestEdictArena_AllocSinceFreshGate(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 4)
	a.Reset()
	// Free slot 1 at t=10; "fresh" window 2 seconds.
	e, _, _ := a.Alloc()
	a.Free(e, 10)
	// Within the fresh window: skip slot 1, return slot 2.
	_, idx, err := a.AllocSince(11, 2)
	if err != nil || idx != 2 {
		t.Errorf("AllocSince fresh skip: idx=%d err=%v", idx, err)
	}
}

func TestEdictArena_AllocSinceFallback(t *testing.T) {
	// All free slots are within the fresh window -> fall back to
	// the first free anyway (don't return ErrArenaFull when
	// freshness alone is the gate).
	a := NewEdictArena(progsForArena(1), 3)
	a.Reset()
	a.Free(&a.edicts[1], 10)
	a.Free(&a.edicts[2], 10)
	_, idx, err := a.AllocSince(11, 2)
	if err != nil {
		t.Fatalf("AllocSince fallback: %v", err)
	}
	if idx != 1 {
		t.Errorf("fallback picked slot %d; want 1", idx)
	}
}

// AllocSince must skip non-free slots before applying the freshness
// gate -- covers the `if !e.Free { continue }` early-skip branch.
func TestEdictArena_AllocSinceSkipsAllocated(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 4)
	a.Reset()
	// Allocate slot 1 (now NOT free), leaving slots 2..3 free.
	_, _, _ = a.Alloc()
	_, idx, err := a.AllocSince(0, 0)
	if err != nil || idx != 2 {
		t.Errorf("AllocSince should land on slot 2 (slot 1 already allocated); got idx=%d err=%v", idx, err)
	}
}

func TestEdictArena_AllocSinceArenaFull(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 1) // world only; no game slot
	a.Reset()
	if _, _, err := a.AllocSince(0, 0); !errors.Is(err, ErrArenaFull) {
		t.Errorf("got %v", err)
	}
}

func TestEdictArena_FreeNil(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 2)
	a.Free(nil, 5) // must not panic
}

func TestEdictArena_NumFor(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 4)
	e2, _ := a.Get(2)
	if got := a.NumFor(e2); got != 2 {
		t.Errorf("NumFor: %d", got)
	}
	if got := a.NumFor(&Edict{}); got != -1 {
		t.Errorf("NumFor foreign: %d", got)
	}
}

func TestEdictArena_ResetWorldStaysAllocated(t *testing.T) {
	a := NewEdictArena(progsForArena(1), 3)
	a.Reset()
	world, _ := a.Get(0)
	if world.Free {
		t.Error("world (slot 0) must NEVER be Free")
	}
}

// --- typed field accessors --------------------------------------------------

func TestEdict_FieldFloatRoundTrip(t *testing.T) {
	a := NewEdictArena(progsForArena(4), 2)
	a.Reset()
	e, _, _ := a.Alloc()
	if err := e.FieldSetFloat(1, math.Pi); err != nil {
		t.Fatal(err)
	}
	got, err := e.FieldFloat(1)
	if err != nil || got != float32(math.Pi) {
		t.Errorf("round-trip: got %v err=%v", got, err)
	}
}

func TestEdict_FieldIntRoundTrip(t *testing.T) {
	a := NewEdictArena(progsForArena(4), 2)
	a.Reset()
	e, _, _ := a.Alloc()
	if err := e.FieldSetInt(2, -42); err != nil {
		t.Fatal(err)
	}
	got, _ := e.FieldInt(2)
	if got != -42 {
		t.Errorf("got %d want -42", got)
	}
}

func TestEdict_FieldVectorRoundTrip(t *testing.T) {
	a := NewEdictArena(progsForArena(4), 2) // 4 fields = 16 bytes; vector at ofs 0 uses 12 bytes
	a.Reset()
	e, _, _ := a.Alloc()
	want := [3]float32{1.5, -2.5, 3.5}
	if err := e.FieldSetVector(0, want); err != nil {
		t.Fatal(err)
	}
	got, _ := e.FieldVector(0)
	if got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestEdict_FieldOutOfRange(t *testing.T) {
	a := NewEdictArena(progsForArena(2), 2) // 2 fields = 8 bytes
	a.Reset()
	e, _, _ := a.Alloc()
	for name, fn := range map[string]func() error{
		"float-neg":  func() error { return e.FieldSetFloat(-1, 0) },
		"float-far":  func() error { return e.FieldSetFloat(100, 0) },
		"int-far":    func() error { return e.FieldSetInt(100, 0) },
		"vec-near-end": func() error { return e.FieldSetVector(1, [3]float32{}) }, // needs 12 bytes from ofs 4, but only 8 total
	} {
		if err := fn(); !errors.Is(err, ErrFieldOffset) {
			t.Errorf("%s: got %v want ErrFieldOffset", name, err)
		}
	}
	if _, err := e.FieldFloat(-1); !errors.Is(err, ErrFieldOffset) {
		t.Error("FieldFloat neg")
	}
	if _, err := e.FieldInt(-1); !errors.Is(err, ErrFieldOffset) {
		t.Error("FieldInt neg")
	}
	if _, err := e.FieldVector(-1); !errors.Is(err, ErrFieldOffset) {
		t.Error("FieldVector neg")
	}
}

// Alloc + Free + Alloc should reuse the freed slot with zeroed fields.
func TestEdictArena_AllocReusesAndClears(t *testing.T) {
	a := NewEdictArena(progsForArena(2), 2) // world + 1 game slot
	a.Reset()
	e, _, _ := a.Alloc()
	_ = e.FieldSetInt(0, 0xDEADBEEF-0x100000000) // sentinel value
	a.Free(e, 5)
	e2, _, _ := a.Alloc()
	if e2 != e {
		t.Error("re-Alloc should re-use the same slot")
	}
	got, _ := e2.FieldInt(0)
	if got != 0 {
		t.Errorf("fields not cleared on re-Alloc: got %#x", got)
	}
}
