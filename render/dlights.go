// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// MaxDLights is the per-frame dynamic light pool capacity.
// tyrquake: MAX_DLIGHTS in client.h.
const MaxDLights = 32

// DLightPool is the fixed-cap dynamic light bank. Each slot tracks a
// per-frame point light spawned by muzzle flashes, projectiles,
// explosions, etc. The pool re-uses slots when a light expires
// (Die <= now) or when AllocByKey replaces a same-keyed slot
// (entity-owned re-firing lights overwrite their prior slot).
type DLightPool struct {
	Lights [MaxDLights]DLight
}

// DLight is one slot. Mirrors the dlight_t shape from
// engine/client/state.go so callers can pass either client.DLight or
// render.DLight; they're field-compatible.
//
// tyrquake: dlight_t in client.h.
type DLight struct {
	Origin   [3]float32
	Radius   float32
	Die      float32 // server time at which the light expires
	Decay    float32 // radius reduction per second
	MinLight float32 // minimum visible threshold
	Key      int     // entity-owned key; same-key spawns reuse the slot
	Color    [3]float32
}

var ErrDLightNoSlot = errors.New("render: dlight pool exhausted")

// NewDLightPool returns a fresh pool with every slot expired
// (Die = 0).
func NewDLightPool() *DLightPool {
	return &DLightPool{}
}

// Alloc returns a pointer to a free slot, writes the supplied init
// data, and returns its index. Returns -1 + ErrDLightNoSlot if every
// slot is alive.
//
// `now` is wall-clock-like time; slots whose Die <= now are
// considered free.
//
// tyrquake: CL_AllocDlight when key == 0 (no entity-owned reuse).
func (p *DLightPool) Alloc(now float32, init DLight) (int, error) {
	for i := range p.Lights {
		if p.Lights[i].Die <= now {
			p.Lights[i] = init
			return i, nil
		}
	}
	return -1, ErrDLightNoSlot
}

// AllocByKey is like Alloc but checks for a slot whose Key matches
// `init.Key` first; if found, that slot is overwritten regardless of
// its remaining lifetime. tyrquake: CL_AllocDlight when key != 0
// (entity-owned reuse so a repeated muzzle flash doesn't allocate
// new slots).
//
// If no same-key slot exists, falls through to the free-slot scan.
// init.Key == 0 is invalid (use Alloc instead); returns ErrDLightNoSlot.
func (p *DLightPool) AllocByKey(now float32, init DLight) (int, error) {
	if init.Key == 0 {
		return -1, ErrDLightNoSlot
	}
	for i := range p.Lights {
		if p.Lights[i].Key == init.Key {
			p.Lights[i] = init
			return i, nil
		}
	}
	return p.Alloc(now, init)
}

// AnimateLights advances every alive light's radius by -Decay*dt
// and marks any whose radius drops below MinLight (or 0) as
// expired by setting Die <= now. tyrquake: CL_DecayLights /
// R_AnimateLight, called once per tic.
//
// `now`, `dt`: wall-clock-like time and frame delta (seconds).
func (p *DLightPool) AnimateLights(now, dt float32) {
	for i := range p.Lights {
		l := &p.Lights[i]
		if l.Die <= now {
			continue
		}
		l.Radius -= dt * l.Decay
		floor := l.MinLight
		if floor < 0 {
			floor = 0
		}
		if l.Radius <= floor {
			// Expired by radius decay; mark the slot free by
			// pulling Die back to "before now". A subtle 0 wouldn't
			// be enough if now also happens to be 0, so subtract
			// epsilon.
			l.Die = now - 1
			l.Radius = 0
		}
	}
}

// AliveCount returns the number of currently-burning lights.
func (p *DLightPool) AliveCount(now float32) int {
	n := 0
	for i := range p.Lights {
		if p.Lights[i].Die > now {
			n++
		}
	}
	return n
}
