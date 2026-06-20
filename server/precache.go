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
