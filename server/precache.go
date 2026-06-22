// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"fmt"
)

// ErrNotPrecached is returned when ModelIndex / SoundIndex cannot
// find the requested asset name in the precache slice. The C
// upstream Host_Error's on the model path and Con_Printf-then-
// returns on the sound path; the Go port surfaces the failure as
// an error so callers pick the policy.
var ErrNotPrecached = errors.New("server: asset not in precache")

// ErrPrecacheFull is returned by [PrecacheModel] / [PrecacheSound]
// when the precache table has no empty slot left (every entry filled
// with a non-empty name). The C upstream calls Host_Error which
// long-jumps back to the main loop; the Go port surfaces the
// condition as an error so callers pick the policy.
var ErrPrecacheFull = errors.New("server: precache table full")

// PrecacheModel returns the slot index of name within precache,
// adding name to the first empty slot when it is not already present.
// Empty name resolves to slot 0 (the world-model sentinel) without
// mutating the table. Returns ErrPrecacheFull when every slot is
// filled with a non-empty name. tyrquake: SV_ModelIndex's
// "add-if-missing" branch (NQ/sv_main.c).
//
// The precache slice is sentinel-terminated: the walk stops at the
// first empty entry, and "first empty entry" is where new names land.
// The slice itself is fixed-size ([MaxModels]); this helper mutates
// the entry in place, it never re-slices, so the caller's reference
// stays valid.
func PrecacheModel(precache []string, name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	for i, entry := range precache {
		if entry == name {
			return i, nil
		}
		if entry == "" {
			precache[i] = name
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: model %q", ErrPrecacheFull, name)
}

// PrecacheSound mirrors [PrecacheModel] for the sound table. Slot 0
// is reserved (matching [SoundIndex]'s `for i := 1` walk), so the
// add-if-missing search starts at index 1. Empty name returns 0
// without mutating the table. tyrquake: SV_SoundIndex's
// "add-if-missing" branch (NQ/sv_main.c).
func PrecacheSound(precache []string, name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	for i := 1; i < len(precache); i++ {
		if precache[i] == name {
			return i, nil
		}
		if precache[i] == "" {
			precache[i] = name
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: sound %q", ErrPrecacheFull, name)
}

// ModelIndex returns the slot index of name within precache, or 0
// if name is empty. tyrquake: SV_ModelIndex.
//
// Slot 0 is the world model (always present, conventionally
// "maps/<name>.bsp"); slots 1..N hold the precached submodels +
// alias models + sprites loaded during SV_SpawnServer. The walk
// stops at the first nil entry: precache is a sentinel-terminated
// array.
//
// Returns ErrNotPrecached when name is non-empty but no slot
// matches; the upstream Host_Error's. Callers that want a
// silent-skip can match the error and return 0.
func ModelIndex(precache []string, name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	for i, entry := range precache {
		if entry == "" {
			break
		}
		if entry == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: model %q", ErrNotPrecached, name)
}

// SoundIndex returns the slot index of name within precache. Unlike
// ModelIndex, SoundIndex's slot 0 is reserved (the walk starts at
// 1) -- the upstream SV_StartSound iterates `for (sound_num = 1;
// ...; sound_num++)`. tyrquake: the inline lookup loop in
// SV_StartSound (NQ/sv_main.c).
//
// Returns ErrNotPrecached when name doesn't match any slot.
func SoundIndex(precache []string, name string) (int, error) {
	for i := 1; i < len(precache); i++ {
		if precache[i] == "" {
			break
		}
		if precache[i] == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: sound %q", ErrNotPrecached, name)
}
