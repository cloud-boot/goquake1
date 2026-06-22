// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"
)

// --- ModelIndex --------------------------------------------------------

func TestModelIndex_EmptyName(t *testing.T) {
	got, err := ModelIndex([]string{"world.bsp", "door.bsp"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("empty name: got %d want 0", got)
	}
}

func TestModelIndex_FoundAtZero(t *testing.T) {
	got, err := ModelIndex([]string{"world.bsp", "door.bsp"}, "world.bsp")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("got %d want 0", got)
	}
}

func TestModelIndex_FoundAtNonZero(t *testing.T) {
	got, err := ModelIndex([]string{"world.bsp", "door.bsp", "lift.bsp"}, "lift.bsp")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("got %d want 2", got)
	}
}

// Walk stops at the first empty entry: precache is sentinel-terminated.
func TestModelIndex_StopsAtEmptySentinel(t *testing.T) {
	precache := []string{"world.bsp", "door.bsp", "", "stale.bsp"}
	_, err := ModelIndex(precache, "stale.bsp")
	if !errors.Is(err, ErrNotPrecached) {
		t.Errorf("entries past empty sentinel must not match, got %v", err)
	}
}

func TestModelIndex_NotPrecached(t *testing.T) {
	_, err := ModelIndex([]string{"world.bsp"}, "missing.bsp")
	if !errors.Is(err, ErrNotPrecached) {
		t.Errorf("got %v want ErrNotPrecached", err)
	}
}

// --- SoundIndex --------------------------------------------------------

// SoundIndex starts the walk at slot 1; slot 0 is reserved.
func TestSoundIndex_StartsAtSlotOne(t *testing.T) {
	// "wing.wav" placed at slot 0 should be invisible.
	precache := []string{"wing.wav", "wing.wav", "rocket.wav"}
	got, err := SoundIndex(precache, "wing.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("got %d want 1 (slot 0 skipped)", got)
	}
}

func TestSoundIndex_FoundAtLaterSlot(t *testing.T) {
	got, err := SoundIndex([]string{"", "wing.wav", "rocket.wav"}, "rocket.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("got %d want 2", got)
	}
}

func TestSoundIndex_StopsAtEmptySentinel(t *testing.T) {
	precache := []string{"", "wing.wav", "", "stale.wav"}
	_, err := SoundIndex(precache, "stale.wav")
	if !errors.Is(err, ErrNotPrecached) {
		t.Errorf("got %v want ErrNotPrecached past sentinel", err)
	}
}

func TestSoundIndex_NotPrecached(t *testing.T) {
	_, err := SoundIndex([]string{"", "wing.wav"}, "missing.wav")
	if !errors.Is(err, ErrNotPrecached) {
		t.Errorf("got %v want ErrNotPrecached", err)
	}
}

// Empty precache: SoundIndex starts at i=1; with len 0 the loop
// doesn't execute -> ErrNotPrecached.
func TestSoundIndex_EmptyPrecache(t *testing.T) {
	_, err := SoundIndex(nil, "any.wav")
	if !errors.Is(err, ErrNotPrecached) {
		t.Errorf("got %v want ErrNotPrecached", err)
	}
}

// --- PrecacheModel -----------------------------------------------------

// Empty name resolves to slot 0 without mutating the table.
func TestPrecacheModel_EmptyName(t *testing.T) {
	precache := []string{"world.bsp", "", "", ""}
	got, err := PrecacheModel(precache, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("empty name: got %d want 0", got)
	}
	if precache[1] != "" {
		t.Errorf("empty name must not mutate: precache[1]=%q", precache[1])
	}
}

// Already-precached name returns its existing slot without re-adding.
func TestPrecacheModel_AlreadyPresent(t *testing.T) {
	precache := []string{"world.bsp", "door.bsp", "", ""}
	got, err := PrecacheModel(precache, "door.bsp")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("found at 1: got %d want 1", got)
	}
	if precache[2] != "" {
		t.Errorf("existing name must not append: precache[2]=%q", precache[2])
	}
}

// Missing name lands at the first empty slot + returns its index.
func TestPrecacheModel_AppendAtFirstEmpty(t *testing.T) {
	precache := []string{"world.bsp", "door.bsp", "", ""}
	got, err := PrecacheModel(precache, "progs/zombie.mdl")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("got %d want 2 (first empty)", got)
	}
	if precache[2] != "progs/zombie.mdl" {
		t.Errorf("precache[2]=%q want progs/zombie.mdl", precache[2])
	}
}

// All slots full + name absent -> ErrPrecacheFull.
func TestPrecacheModel_Full(t *testing.T) {
	precache := []string{"world.bsp", "door.bsp", "lift.bsp"}
	_, err := PrecacheModel(precache, "progs/zombie.mdl")
	if !errors.Is(err, ErrPrecacheFull) {
		t.Errorf("got %v want ErrPrecacheFull", err)
	}
}

// --- PrecacheSound -----------------------------------------------------

func TestPrecacheSound_EmptyName(t *testing.T) {
	precache := []string{"", "", "", ""}
	got, err := PrecacheSound(precache, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("empty name: got %d want 0", got)
	}
	if precache[1] != "" {
		t.Errorf("empty name must not mutate: precache[1]=%q", precache[1])
	}
}

func TestPrecacheSound_SkipsSlotZero(t *testing.T) {
	// A name lodged at slot 0 must NOT be returned -- PrecacheSound
	// mirrors SoundIndex's "slot 0 reserved" walk.
	precache := []string{"wing.wav", "", "", ""}
	got, err := PrecacheSound(precache, "wing.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("got %d want 1 (slot 0 reserved)", got)
	}
	if precache[1] != "wing.wav" {
		t.Errorf("precache[1]=%q want wing.wav", precache[1])
	}
}

func TestPrecacheSound_AlreadyPresent(t *testing.T) {
	precache := []string{"", "wing.wav", "rocket.wav", ""}
	got, err := PrecacheSound(precache, "rocket.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("got %d want 2", got)
	}
	if precache[3] != "" {
		t.Errorf("existing name must not append: precache[3]=%q", precache[3])
	}
}

func TestPrecacheSound_AppendAtFirstEmpty(t *testing.T) {
	precache := []string{"", "wing.wav", "", ""}
	got, err := PrecacheSound(precache, "rocket.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("got %d want 2", got)
	}
	if precache[2] != "rocket.wav" {
		t.Errorf("precache[2]=%q want rocket.wav", precache[2])
	}
}

func TestPrecacheSound_Full(t *testing.T) {
	precache := []string{"", "wing.wav", "rocket.wav"}
	_, err := PrecacheSound(precache, "boom.wav")
	if !errors.Is(err, ErrPrecacheFull) {
		t.Errorf("got %v want ErrPrecacheFull", err)
	}
}
