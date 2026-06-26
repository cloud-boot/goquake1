// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

// TempSpriteLifetime is the canonical visible-window for an explosion
// sprite, in seconds. The vanilla engine spawns s_explod.spr with 6
// frames at 10 fps = 0.6 s total; we mirror that as the default
// per-slot decay so spawning + decaying are decoupled from the actual
// sprite's frame count (a 4-frame variant would still age out at the
// same wall-clock moment).
const TempSpriteLifetime float32 = 0.6

// MaxTempSprites caps the number of live billboards the client
// tracks. Old slots reincarnate first-come-first-out (the oldest
// die-time wins eviction) so a long firefight doesn't unboundedly
// allocate. tyrquake collapses temp sprites into the unified
// MAX_TEMP_ENTITIES table; we keep a dedicated pool here so the
// renderer can iterate without filtering by kind on every tic.
const MaxTempSprites = 32

// TempSprite is one live billboard slot.  SpawnTime is the wall-
// clock time the slot was created (the `now` argument to Spawn);
// the per-tic walk reads (now - SpawnTime) to derive both the
// chosen animation frame and the alive/dead bit (slot dies when
// the elapsed exceeds [TempSpriteLifetime]).
type TempSprite struct {
	Origin    [3]float32
	SpawnTime float32
	Lifetime  float32 // seconds; 0 means use TempSpriteLifetime
	Alive     bool
}

// TempSpritePool is a fixed-cap ring of explosion-style billboards
// the client maintains between draw passes. The pool is intentionally
// allocation-free after construction: Spawn finds a dead slot OR
// evicts the slot with the earliest spawn time; Walk iterates and
// auto-retires expired slots.
//
// The pool is goroutine-unsafe -- the bring-up's per-frame loop is
// single-threaded; mirroring tyrquake's cl_temp_entities[] which is
// also touched from one thread only.
type TempSpritePool struct {
	Slots [MaxTempSprites]TempSprite
}

// NewTempSpritePool returns a fresh pool with every slot Alive=false.
func NewTempSpritePool() *TempSpritePool {
	return &TempSpritePool{}
}

// Spawn writes a new live slot at Origin/now. lifetime <= 0 picks
// the canonical [TempSpriteLifetime]. Returns the index of the
// reused/freshly-allocated slot.
//
// Eviction policy: prefer a dead slot; if every slot is alive, evict
// the slot with the EARLIEST SpawnTime (longest in flight). This is
// the LRU-by-age strategy id1 uses for its cl_dlights[] and
// cl_temp_entities[] tables.
func (p *TempSpritePool) Spawn(origin [3]float32, now, lifetime float32) int {
	if lifetime <= 0 {
		lifetime = TempSpriteLifetime
	}
	// First pass: reuse a dead slot.
	oldest := 0
	for i := range p.Slots {
		if !p.Slots[i].Alive {
			p.Slots[i] = TempSprite{
				Origin:    origin,
				SpawnTime: now,
				Lifetime:  lifetime,
				Alive:     true,
			}
			return i
		}
		if p.Slots[i].SpawnTime < p.Slots[oldest].SpawnTime {
			oldest = i
		}
	}
	// All slots alive: overwrite the oldest.
	p.Slots[oldest] = TempSprite{
		Origin:    origin,
		SpawnTime: now,
		Lifetime:  lifetime,
		Alive:     true,
	}
	return oldest
}

// NumAlive returns the number of slots whose lifetime has NOT yet
// elapsed at `now`. Slots whose elapsed >= Lifetime are reported as
// dead even if not yet retired (the per-tic [TempSpritePool.Walk]
// retires them lazily).
func (p *TempSpritePool) NumAlive(now float32) int {
	n := 0
	for i := range p.Slots {
		s := &p.Slots[i]
		if !s.Alive {
			continue
		}
		if now-s.SpawnTime < s.Lifetime {
			n++
		}
	}
	return n
}

// Walk invokes draw on every live, non-expired slot in spawn order
// (not slot index order -- callers shouldn't depend on either) and
// retires slots whose elapsed time has crossed their Lifetime.
//
// draw receives the slot's world-space origin + the elapsed time
// since spawn (so it can pick the per-elapsed sprite frame via
// render.SpriteFrameForElapsed). draw must NOT mutate the pool from
// inside the callback (the iteration walks the slot array directly).
//
// nil draw is treated as a "tick only" call: expired slots still age
// out, but no callback fires.
func (p *TempSpritePool) Walk(now float32, draw func(origin [3]float32, elapsed float32)) {
	for i := range p.Slots {
		s := &p.Slots[i]
		if !s.Alive {
			continue
		}
		elapsed := now - s.SpawnTime
		if elapsed >= s.Lifetime {
			s.Alive = false
			continue
		}
		if draw != nil {
			draw(s.Origin, elapsed)
		}
	}
}

// Reset retires every slot. Called by the embedder on map-change
// (the new map's wire stream starts fresh; stale explosions from the
// previous map would otherwise linger until their natural decay).
func (p *TempSpritePool) Reset() {
	for i := range p.Slots {
		p.Slots[i].Alive = false
	}
}
