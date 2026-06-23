// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import "math"

// BeamLifetime is the canonical visible-window for one lightning bolt
// flash, in seconds. tyrquake's CL_ParseBeam stamps b->endtime =
// cl.time + 0.2 -- each TE_LIGHTNING* message extends the beam for one
// fifth of a second, so a continuously-firing thunderbolt looks like a
// solid wall of bolts while a one-shot fires for 200 ms then dies.
const BeamLifetime float32 = 0.2

// BeamSegmentLength is the world-space tile size of one bolt model
// instance. id1's bolt1/bolt2/bolt3.mdl are each 30 units long along
// the +X axis; the renderer chains N copies along the beam vector to
// cover the full distance. tyrquake: hard-coded `org[i] += dist[i] *
// 30;` step inside CL_UpdateBeams.
const BeamSegmentLength float32 = 30

// MaxBeamSegments caps the per-beam tile count so a single
// pathologically-long traceline (server-side hit-scan goes up to 600
// units; a future grappling-hook beam might be longer) can't push the
// renderer into a million-segment loop. 64 segments * 30 units = 1920
// world units, comfortably past the longest expected beam.
const MaxBeamSegments = 64

// MaxLiveBeams caps the number of concurrently visible bolts the
// client tracks. tyrquake's MAX_BEAMS is 24; we mirror it via the
// existing [MaxBeams] constant for the per-process pool capacity.
// Both lightning2 (the player's thunderbolt) and lightning1/3 (boss
// beams) share the same pool.

// Beam is one live lightning-bolt slot. SpawnTime is the wall-clock
// time the slot was created; the per-tic walk reads (now - SpawnTime)
// to derive the alive/dead bit (slot dies when the elapsed exceeds
// [BeamLifetime]).
//
// Kind is one of the TE_* sub-type values (TE_LIGHTNING1 / 2 / 3 /
// TE_BEAM) so the renderer can pick the matching bolt1/bolt2/bolt3
// model. EntityNum is the owning entity (the player or boss firing the
// bolt) -- tyrquake re-anchors the beam's Start each tic to the owner
// edict's current origin (so the player's thunderbolt follows them as
// they strafe); the Go port stashes the field so a future per-tic
// re-anchor pass has the input.
type Beam struct {
	Kind      int
	EntityNum int
	Start     [3]float32
	End       [3]float32
	SpawnTime float32
	Alive     bool
}

// BeamPool is a fixed-cap ring of live lightning beams the client
// maintains between draw passes. The pool is intentionally allocation-
// free after construction: Spawn finds a dead slot OR evicts the slot
// with the earliest spawn time; Walk iterates and auto-retires expired
// slots. tyrquake: cl_beams[MAX_BEAMS] in NQ/client.h.
type BeamPool struct {
	Slots [MaxBeams]Beam
}

// NewBeamPool returns a fresh pool with every slot Alive=false.
func NewBeamPool() *BeamPool {
	return &BeamPool{}
}

// Spawn writes a new live slot at the given (start, end, kind, ent).
// tyrquake's CL_ParseBeam re-uses an existing slot when the SAME
// (kind, ent) pair is already alive -- this models the per-tic re-arm
// the server emits for as long as the trigger is held, so multiple
// successive TE_LIGHTNING messages from the same entity collapse to
// one continuously-extended bolt rather than racing through the slot
// table. The Go port mirrors that "same (kind,ent) wins" lookup.
//
// Returns the index of the reused / freshly-allocated slot.
func (p *BeamPool) Spawn(kind, ent int, start, end [3]float32, now float32) int {
	// First pass: extend any existing beam from the same (kind, ent).
	for i := range p.Slots {
		s := &p.Slots[i]
		if s.Alive && s.Kind == kind && s.EntityNum == ent {
			s.Start = start
			s.End = end
			s.SpawnTime = now
			return i
		}
	}
	// Second pass: reuse a dead slot.
	oldest := 0
	for i := range p.Slots {
		if !p.Slots[i].Alive {
			p.Slots[i] = Beam{
				Kind:      kind,
				EntityNum: ent,
				Start:     start,
				End:       end,
				SpawnTime: now,
				Alive:     true,
			}
			return i
		}
		if p.Slots[i].SpawnTime < p.Slots[oldest].SpawnTime {
			oldest = i
		}
	}
	// All slots alive: overwrite the oldest. tyrquake: cl_beams[]'s
	// LRU-by-spawn-time eviction is the same shape as the temp-sprite
	// pool's; the bolt that's been on screen the longest is the one
	// the player has already absorbed visually + can be retired.
	p.Slots[oldest] = Beam{
		Kind:      kind,
		EntityNum: ent,
		Start:     start,
		End:       end,
		SpawnTime: now,
		Alive:     true,
	}
	return oldest
}

// NumAlive returns the number of slots whose lifetime has NOT yet
// elapsed at `now`. Slots whose elapsed >= [BeamLifetime] are reported
// as dead even if not yet retired (the per-tic [BeamPool.Walk] retires
// them lazily).
func (p *BeamPool) NumAlive(now float32) int {
	n := 0
	for i := range p.Slots {
		s := &p.Slots[i]
		if !s.Alive {
			continue
		}
		if now-s.SpawnTime < BeamLifetime {
			n++
		}
	}
	return n
}

// BeamSegment is the per-tile draw payload [BeamPool.Walk] hands to
// its callback. Origin is the world-space anchor of one bolt model
// instance; Yaw / Pitch are the alias-renderer angle inputs (degrees)
// that orient the +X-aligned bolt mesh along the beam vector. Kind is
// the TE_* sub-type so the embedder can pick bolt1 / bolt2 / bolt3.
type BeamSegment struct {
	Kind      int
	EntityNum int
	Index     int        // 0..Total-1
	Total     int        // per-beam segment count
	Origin    [3]float32 // world-space tile anchor
	Yaw       float32    // degrees
	Pitch     float32    // degrees
}

// BeamSegments returns the per-tile draw payloads for one (start, end)
// pair. The beam vector is divided into ceil(length /
// [BeamSegmentLength]) tiles, capped at [MaxBeamSegments]; each tile's
// Origin is start + dir * (i * BeamSegmentLength), with (Yaw, Pitch)
// derived from dir so the +X-aligned bolt mesh visually points end-
// ward. tyrquake: the CL_UpdateBeams `org[i] += dist[i] * 30` loop +
// VectorAngles call.
//
// Returns a nil slice for a degenerate (length == 0) beam.
func BeamSegments(kind, ent int, start, end [3]float32) []BeamSegment {
	dx := end[0] - start[0]
	dy := end[1] - start[1]
	dz := end[2] - start[2]
	length := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	if length <= 0 {
		return nil
	}
	segments := int(length / BeamSegmentLength)
	// Always emit at least one tile for a non-degenerate beam, even
	// when length < BeamSegmentLength -- a one-tile bolt overshoots
	// the endpoint slightly but that's how tyrquake renders it too
	// (the while-loop runs at least once).
	if segments < 1 {
		segments = 1
	}
	if segments > MaxBeamSegments {
		segments = MaxBeamSegments
	}
	yaw, pitch := beamAngles(dx, dy, dz, length)
	invLen := 1.0 / length
	ux := dx * invLen
	uy := dy * invLen
	uz := dz * invLen
	out := make([]BeamSegment, segments)
	for i := 0; i < segments; i++ {
		step := float32(i) * BeamSegmentLength
		out[i] = BeamSegment{
			Kind:      kind,
			EntityNum: ent,
			Index:     i,
			Total:     segments,
			Origin: [3]float32{
				start[0] + ux*step,
				start[1] + uy*step,
				start[2] + uz*step,
			},
			Yaw:   yaw,
			Pitch: pitch,
		}
	}
	return out
}

// beamAngles derives the (yaw, pitch) the alias renderer needs to
// rotate a +X-aligned bolt model onto the beam direction. Mirrors
// mathlib.VectorAngles (R_AliasSetUpTransform's input formula), but
// kept local to avoid a client -> mathlib import cycle. Degrees.
func beamAngles(dx, dy, dz, length float32) (yaw, pitch float32) {
	const rad2deg = float32(180.0 / math.Pi)
	if dx == 0 && dy == 0 {
		if dz > 0 {
			return 0, 90
		}
		return 0, 270
	}
	yaw = float32(math.Atan2(float64(dy), float64(dx))) * rad2deg
	if yaw < 0 {
		yaw += 360
	}
	flat := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	pitch = float32(math.Atan2(float64(dz), float64(flat))) * rad2deg
	if pitch < 0 {
		pitch += 360
	}
	return yaw, pitch
}

// Walk invokes draw on every per-tile segment of every live, non-
// expired beam (oldest-spawned beams visited first by slot order; the
// per-beam segment iteration runs start-to-end). Retires beams whose
// elapsed time has crossed [BeamLifetime].
//
// nil draw is treated as a "tick only" call: expired slots still age
// out, no callback fires.
func (p *BeamPool) Walk(now float32, draw func(seg BeamSegment)) {
	for i := range p.Slots {
		s := &p.Slots[i]
		if !s.Alive {
			continue
		}
		elapsed := now - s.SpawnTime
		if elapsed >= BeamLifetime {
			s.Alive = false
			continue
		}
		if draw == nil {
			continue
		}
		for _, seg := range BeamSegments(s.Kind, s.EntityNum, s.Start, s.End) {
			draw(seg)
		}
	}
}

// Reset retires every slot. Called by the embedder on map-change so
// stale bolts from the previous map don't linger until their natural
// decay.
func (p *BeamPool) Reset() {
	for i := range p.Slots {
		p.Slots[i].Alive = false
	}
}
