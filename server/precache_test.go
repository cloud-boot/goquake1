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
