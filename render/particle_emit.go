// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

// EmitParticleColorMask masks the low 3 bits of a Particle colour
// index so the per-particle palette pick happens in a contiguous
// 8-shade band (the upstream's `color = (color & ~7) | (rand & 7)`
// idiom seen in R_RunParticleEffect / R_ParticleEffect callers).
const EmitParticleColorMask byte = 0xf8 // ~7

// Emit is the canonical entry point for the QuakeC particle() builtin
// (pr_cmds.c PF_particle) and the simple temp-entity dust/spark/blood
// arms in cl_tent.c (CL_ParseTEnt -> R_RunParticleEffect). Spawns
// `count` particles centred on `origin`, each with:
//
//	Origin   = origin + per-axis (rand%32 - 16)
//	Velocity = dir + per-axis (rand%32 - 16)
//	Color    = (baseColor & ~7) | (rand & 7)
//	Type     = ParticleSlowGrav
//	Die      = now + 0.1 * (rand%5)
//
// Stops early on the first Spawn that fails (pool full). Returns the
// number of particles successfully spawned -- callers (the census
// log + tests) use it for visibility into pool pressure.
//
// tyrquake source-of-truth: R_RunParticleEffect in NQ/r_part.c (lines
// 461-489). The C signature is
//
//	void R_RunParticleEffect(vec3_t org, vec3_t dir, int color, int count)
//
// and PF_particle's body is one straight call into it after pulling
// the four parms off the QC stack. By implementing the colour-mask /
// random-jitter logic here once, both the builtin path (QC code asks
// for particles via the bytecode) and the svc_particle decoder arm
// (the server fires particles directly over the wire, e.g. on
// shotgun-impact in QC's TraceAttack) collapse to a single call.
//
// rng is a deterministic byte source -- tests pass a counter or PRNG;
// production passes a math/rand-wrapped or RDRAND-wrapped closure.
// Each call returns one uniform byte (0..255); the helper uses only
// the low bits via `&` masks so the rng can be as cheap as a counter
// without skewing the distribution shape.
//
// A nil pool / count <= 0 / nil rng returns 0 (silent no-op) so the
// builtin / decoder arms can call Emit unconditionally without their
// own nil-checks.
func (p *Pool) Emit(origin, dir [3]float32, baseColor byte, count int, now float32, rng func() byte) int {
	if p == nil || count <= 0 || rng == nil {
		return 0
	}
	spawned := 0
	for i := 0; i < count; i++ {
		// Color: keep the upper 5 bits of the base colour; randomise
		// the low 3 so the particle burst fans through one contiguous
		// 8-shade ramp in the palette. Matches PF_particle byte-for-
		// byte.
		col := (baseColor & EmitParticleColorMask) | (rng() & 7)
		// Lifetime jitter (0..0.4s); 0 = die instantly so first Run
		// frees the slot.
		life := 0.1 * float32(int(rng())%5)
		// Per-axis 32-bucket origin + velocity jitter, centred on
		// 0 by the -16 bias.
		var init Particle
		init.Type = ParticleSlowGrav
		init.Color = col
		init.Die = now + life
		for axis := 0; axis < 3; axis++ {
			init.Origin[axis] = origin[axis] + float32(int(rng()&31)-16)
			init.Velocity[axis] = dir[axis] + float32(int(rng()&31)-16)
		}
		if _, ok := p.Spawn(init, now); !ok {
			return spawned
		}
		spawned++
	}
	return spawned
}

// TrailKind selects which of the upstream's six rocket-trail variants
// EmitTrail uses to seed each particle in the trail. tyrquake:
// R_RocketTrail's `type` int argument (NQ/r_part.c lines 491-575).
type TrailKind int

const (
	// TrailRocket is the standard rocket smoke trail: greyscale fire
	// ramp (Ramp3), ParticleFire type, gentle upward bias.
	TrailRocket TrailKind = 0
	// TrailGrenade is the grenade smoke trail: same shape as
	// TrailRocket but starts deeper in Ramp3 (ramp = 2) for a
	// darker, shorter-lived smoke puff.
	TrailGrenade TrailKind = 1
	// TrailBlood is the gib/zombie chunk blood-drip trail:
	// ParticleGrav, colour band 67..70 (Q1 dark-red palette).
	TrailBlood TrailKind = 2
	// TrailTracer is the wizard/scrag green tracer trail:
	// ParticleStatic, colour band 52..55, fixed lifetime ~0.5s.
	TrailTracer TrailKind = 3
	// TrailSlightBlood is the lighter blood trail (e.g. zombie crucified
	// drip): same colour band as TrailBlood with a sparser per-step
	// count. The Go port shares the per-particle init with TrailBlood;
	// callers regulate density by reducing the per-tic call frequency.
	TrailSlightBlood TrailKind = 4
	// TrailTracer2 is the knight/hellknight orange tracer trail:
	// ParticleStatic, colour band 230..233.
	TrailTracer2 TrailKind = 5
	// TrailVoor is the Voor-projectile trail: ParticleStatic, colour
	// band 152..155. Vanilla Q1 only fires this from the Vore
	// homing-orb -- mods often reuse the slot for plasma weapons.
	TrailVoor TrailKind = 6
)

// EmitTrail divides the segment from `start` to `end` into 3-unit
// steps and spawns one particle per step. Used per server tic for
// every projectile carrying a trail effect (rockets, grenades, gib
// blood, scrag/wizard/knight tracers, Voor orbs).
//
// Returns the number of particles successfully spawned (capped by
// pool capacity; the loop stops on the first Spawn failure).
//
// tyrquake source-of-truth: R_RocketTrail in NQ/r_part.c (lines
// 491-575). Per-step layout:
//
//	dir   = end - start
//	dist  = |dir|
//	step  = 3.0          (units per particle)
//	N     = ceil(dist/3)
//
// for each step i in 0..N-1:
//
//	pos = start + (dir/|dir|) * (i*3)
//	per-type init shape (see TrailKind doc above)
//
// rng / pool / start==end / kind out-of-range return 0 (silent no-op).
//
// kind is a TrailKind selector (see the constants above). Indices
// outside the 0..6 range fall through to TrailRocket so a future
// upstream extension or a typo doesn't silently spawn nothing.
func (p *Pool) EmitTrail(start, end [3]float32, kind TrailKind, now float32, rng func() byte) int {
	if p == nil || rng == nil {
		return 0
	}
	dx := end[0] - start[0]
	dy := end[1] - start[1]
	dz := end[2] - start[2]
	dist := sqrt32(dx*dx + dy*dy + dz*dz)
	if dist < 0.001 {
		return 0
	}
	inv := 1 / dist
	stepLen := float32(3)
	// ux/uy/uz are the unit-direction components (used for the tracer
	// perpendicular hop). sx/sy/sz are the per-step world-space deltas
	// (= unit*stepLen) used to advance pos each iteration.
	ux, uy, uz := dx*inv, dy*inv, dz*inv
	sx, sy, sz := ux*stepLen, uy*stepLen, uz*stepLen
	_ = uz // uz is unused today; reserved for future Z-aware variants.

	spawned := 0
	pos := start
	for left := dist; left > 0; left -= stepLen {
		var init Particle
		init.Die = now + 2 // base lifetime; per-kind overrides below
		switch kind {
		case TrailBlood, TrailSlightBlood:
			init.Type = ParticleGrav
			init.Color = 67 + (rng() & 3) // 67..70
			for axis := 0; axis < 3; axis++ {
				init.Origin[axis] = pos[axis] + float32(int(rng()&15)-8)
			}
		case TrailTracer, TrailTracer2:
			init.Type = ParticleStatic
			init.Die = now + 0.5
			if kind == TrailTracer {
				init.Color = 52 + 2*(rng()&1) // 52 or 54 (green band)
			} else {
				init.Color = 230 + 2*(rng()&1) // 230 or 232 (orange band)
			}
			// Tracer wiggle: alternating perpendicular hop per step.
			// Match the upstream pingpong by toggling on parity of
			// the per-emit spawn count (cheap + deterministic).
			if spawned&1 == 0 {
				init.Velocity[0] = 30 * uy
				init.Velocity[1] = -30 * ux
			} else {
				init.Velocity[0] = -30 * uy
				init.Velocity[1] = 30 * ux
			}
			init.Origin = pos
		case TrailVoor:
			init.Type = ParticleStatic
			init.Color = 9*16 + 8 + (rng() & 3) // 152..155
			for axis := 0; axis < 3; axis++ {
				init.Origin[axis] = pos[axis] + float32(int(rng()&15)-8)
			}
		case TrailGrenade:
			init.Type = ParticleFire
			init.Ramp = 2
			init.Color = Ramp3[2]
			for axis := 0; axis < 3; axis++ {
				init.Origin[axis] = pos[axis] + float32(int(rng()&5)-3)
			}
		default: // TrailRocket + fall-through for out-of-range kinds
			init.Type = ParticleFire
			init.Ramp = float32(rng() & 3)
			init.Color = Ramp3[uint(init.Ramp)]
			for axis := 0; axis < 3; axis++ {
				init.Origin[axis] = pos[axis] + float32(int(rng()&5)-3)
			}
		}
		if _, ok := p.Spawn(init, now); !ok {
			return spawned
		}
		spawned++
		pos[0] += sx
		pos[1] += sy
		pos[2] += sz
	}
	return spawned
}
