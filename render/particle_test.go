// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"math"
	"testing"
)

// counterRNG returns a deterministic byte sequence: starting from
// `seed`, each call returns the next byte (mod 256). Useful for
// driving Spawn helpers that consume rng() many times -- the test
// asserts on the cumulative spawn count + a sampled slot rather
// than every byte.
func counterRNG(seed byte) func() byte {
	v := seed
	return func() byte {
		out := v
		v++
		return out
	}
}

// constRNG returns the same byte every call.
func constRNG(b byte) func() byte {
	return func() byte { return b }
}

func TestNewPool_AllSlotsFree(t *testing.T) {
	p := NewPool()
	if p.NumAlive != 0 {
		t.Fatalf("fresh pool NumAlive = %d, want 0", p.NumAlive)
	}
	for i := 0; i < MaxParticles; i++ {
		if p.Particles[i].Die != 0 {
			t.Fatalf("slot %d Die = %v, want 0 (free)", i, p.Particles[i].Die)
		}
	}
}

func TestSpawn_ReturnsAscendingSlots(t *testing.T) {
	p := NewPool()
	for want := 0; want < 8; want++ {
		idx, ok := p.Spawn(Particle{Die: 1}, 0)
		if !ok {
			t.Fatalf("spawn %d failed", want)
		}
		if idx != want {
			t.Fatalf("spawn %d returned slot %d, want %d", want, idx, want)
		}
	}
	if p.NumAlive != 8 {
		t.Fatalf("after 8 spawns NumAlive = %d, want 8", p.NumAlive)
	}
}

func TestSpawn_FillsPool_ThenFails(t *testing.T) {
	p := NewPool()
	for i := 0; i < MaxParticles; i++ {
		if _, ok := p.Spawn(Particle{Die: 1}, 0); !ok {
			t.Fatalf("spawn %d unexpectedly failed", i)
		}
	}
	idx, ok := p.Spawn(Particle{Die: 1}, 0)
	if ok {
		t.Fatalf("spawn into full pool returned ok=true, idx=%d", idx)
	}
	if idx != 0 {
		t.Fatalf("failed spawn returned idx=%d, want 0", idx)
	}
}

func TestSpawn_ReusesFreedSlot(t *testing.T) {
	p := NewPool()
	idx, _ := p.Spawn(Particle{Die: 1, Type: ParticleStatic}, 0)
	if idx != 0 {
		t.Fatalf("first spawn slot = %d, want 0", idx)
	}
	// Run with now > Die expires the slot.
	p.Run(0.1, 0, 2)
	if p.Particles[0].Die != 0 {
		t.Fatalf("after expiry slot 0 Die = %v, want 0", p.Particles[0].Die)
	}
	if p.NumAlive != 0 {
		t.Fatalf("after expiry NumAlive = %d, want 0", p.NumAlive)
	}
	idx2, ok := p.Spawn(Particle{Die: 1}, 0)
	if !ok || idx2 != 0 {
		t.Fatalf("re-spawn slot = %d ok=%v, want 0 true", idx2, ok)
	}
}

func TestRun_StaticDoesNotMoveByForces(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{0, 0, 0},
		Die:      10,
		Type:     ParticleStatic,
	}, 0)
	p.Run(1, 800, 1)
	got := p.Particles[0].Velocity
	if got != [3]float32{0, 0, 0} {
		t.Fatalf("static velocity changed: %v", got)
	}
}

func TestRun_GravLowersZVelocity(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Velocity: [3]float32{1, 2, 100},
		Die:      10,
		Type:     ParticleGrav,
	}, 0)
	// grav = dt*gravity*0.05 = 1*800*0.05 = 40
	p.Run(1, 800, 1)
	if got := p.Particles[0].Velocity[2]; got != 60 {
		t.Fatalf("Grav v[2] = %v, want 60 (100 - 40)", got)
	}
	// x/y velocity unchanged (no horizontal force for Grav).
	if p.Particles[0].Velocity[0] != 1 || p.Particles[0].Velocity[1] != 2 {
		t.Fatalf("Grav touched x/y velocity: %v", p.Particles[0].Velocity)
	}
}

func TestRun_SlowGravUsesSameGrav(t *testing.T) {
	// Upstream's pt_slowgrav falls through to pt_grav -- the name
	// is historical. Document the behavior with a parity assertion.
	p := NewPool()
	p.Spawn(Particle{Velocity: [3]float32{0, 0, 100}, Die: 10, Type: ParticleSlowGrav}, 0)
	q := NewPool()
	q.Spawn(Particle{Velocity: [3]float32{0, 0, 100}, Die: 10, Type: ParticleGrav}, 0)
	p.Run(1, 800, 1)
	q.Run(1, 800, 1)
	if p.Particles[0].Velocity != q.Particles[0].Velocity {
		t.Fatalf("SlowGrav (%v) != Grav (%v)", p.Particles[0].Velocity, q.Particles[0].Velocity)
	}
}

func TestRun_FireAdvancesRamp_ColorTracksRamp3_FloatsUp(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Velocity: [3]float32{0, 0, 0},
		Ramp:     0,
		Die:      10,
		Type:     ParticleFire,
	}, 0)
	// dt = 0.1 -> ramp += 0.5 per tick (time1 = dt*5)
	p.Run(0.1, 800, 1)
	if got := p.Particles[0].Ramp; got != 0.5 {
		t.Fatalf("Fire ramp = %v, want 0.5", got)
	}
	if got := p.Particles[0].Color; got != Ramp3[0] {
		t.Fatalf("Fire color = %#x, want Ramp3[0] = %#x", got, Ramp3[0])
	}
	// Fire FLOATS UP: grav = 0.1*800*0.05 = 4, v[2] += grav.
	if got := p.Particles[0].Velocity[2]; got != 4 {
		t.Fatalf("Fire v[2] = %v, want +4 (floats up)", got)
	}
}

func TestRun_FireExpiresOnRampOverflow(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Ramp: 5.5,
		Die:  100,
		Type: ParticleFire,
	}, 0)
	// time1 = 0.2*5 = 1; ramp goes 5.5 -> 6.5; >= 6 -> die.
	p.Run(0.2, 800, 1)
	if p.Particles[0].Die != 0 {
		t.Fatalf("Fire post-overflow Die = %v, want 0", p.Particles[0].Die)
	}
	if p.NumAlive != 0 {
		t.Fatalf("NumAlive = %d, want 0", p.NumAlive)
	}
}

func TestRun_ExplodeColorTracksRamp1_ExpandsVelocity(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Velocity: [3]float32{10, 20, 30},
		Ramp:     0,
		Die:      10,
		Type:     ParticleExplode,
	}, 0)
	// dt=0.1 -> time2=1, ramp 0->1. dvel = 0.4. grav=4.
	p.Run(0.1, 800, 1)
	pp := p.Particles[0]
	if pp.Ramp != 1 {
		t.Fatalf("Explode ramp = %v, want 1", pp.Ramp)
	}
	if pp.Color != Ramp1[1] {
		t.Fatalf("Explode color = %#x, want Ramp1[1] = %#x", pp.Color, Ramp1[1])
	}
	// v[0] = 10 + 10*0.4 = 14
	if pp.Velocity[0] != 14 {
		t.Fatalf("Explode v[0] = %v, want 14", pp.Velocity[0])
	}
	// v[1] = 20 + 20*0.4 = 28
	if pp.Velocity[1] != 28 {
		t.Fatalf("Explode v[1] = %v, want 28", pp.Velocity[1])
	}
	// v[2] = 30 + 30*0.4 - 4 = 38
	if pp.Velocity[2] != 38 {
		t.Fatalf("Explode v[2] = %v, want 38", pp.Velocity[2])
	}
}

func TestRun_ExplodeExpiresOnRampOverflow(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{Ramp: 7.5, Die: 100, Type: ParticleExplode}, 0)
	// time2 = 0.1*10 = 1; ramp 7.5 -> 8.5; >= 8 -> die.
	p.Run(0.1, 800, 1)
	if p.Particles[0].Die != 0 {
		t.Fatalf("Explode post-overflow Die = %v, want 0", p.Particles[0].Die)
	}
}

func TestRun_Explode2ColorTracksRamp2_DampsVelocity(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Velocity: [3]float32{10, 20, 30},
		Ramp:     0,
		Die:      10,
		Type:     ParticleExplode2,
	}, 0)
	// dt=0.1 -> time3=1.5, ramp 0->1.5 -> uint(1.5)=1.
	// v[i] -= v[i]*dt: v *= 0.9.
	p.Run(0.1, 800, 1)
	pp := p.Particles[0]
	if pp.Ramp != 1.5 {
		t.Fatalf("Explode2 ramp = %v, want 1.5", pp.Ramp)
	}
	if pp.Color != Ramp2[1] {
		t.Fatalf("Explode2 color = %#x, want Ramp2[1] = %#x", pp.Color, Ramp2[1])
	}
	if pp.Velocity[0] != 9 {
		t.Fatalf("Explode2 v[0] = %v, want 9 (10*0.9)", pp.Velocity[0])
	}
	if pp.Velocity[1] != 18 {
		t.Fatalf("Explode2 v[1] = %v, want 18 (20*0.9)", pp.Velocity[1])
	}
	// v[2] = 30*0.9 - 4 = 23
	if pp.Velocity[2] != 23 {
		t.Fatalf("Explode2 v[2] = %v, want 23", pp.Velocity[2])
	}
}

func TestRun_Explode2ExpiresOnRampOverflow(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{Ramp: 7.5, Die: 100, Type: ParticleExplode2}, 0)
	// time3 = 0.1*15 = 1.5; ramp 7.5 -> 9; >= 8 -> die.
	p.Run(0.1, 800, 1)
	if p.Particles[0].Die != 0 {
		t.Fatalf("Explode2 post-overflow Die = %v, want 0", p.Particles[0].Die)
	}
}

func TestRun_BlobExpandsAllAxes(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{Velocity: [3]float32{10, 20, 30}, Die: 10, Type: ParticleBlob}, 0)
	// dvel = 0.4; v*=1.4; v[2] -= grav(4).
	p.Run(0.1, 800, 1)
	pp := p.Particles[0]
	if pp.Velocity[0] != 14 {
		t.Fatalf("Blob v[0] = %v, want 14", pp.Velocity[0])
	}
	if pp.Velocity[1] != 28 {
		t.Fatalf("Blob v[1] = %v, want 28", pp.Velocity[1])
	}
	// 30 + 30*0.4 - 4 = 38
	if pp.Velocity[2] != 38 {
		t.Fatalf("Blob v[2] = %v, want 38", pp.Velocity[2])
	}
}

func TestRun_Blob2DampsXYOnly(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{Velocity: [3]float32{10, 20, 30}, Die: 10, Type: ParticleBlob2}, 0)
	// dvel = 0.4; v[0..1] -= v[0..1]*dvel (so *= 0.6); v[2] -= grav.
	p.Run(0.1, 800, 1)
	pp := p.Particles[0]
	if pp.Velocity[0] != 6 {
		t.Fatalf("Blob2 v[0] = %v, want 6 (10*0.6)", pp.Velocity[0])
	}
	if pp.Velocity[1] != 12 {
		t.Fatalf("Blob2 v[1] = %v, want 12 (20*0.6)", pp.Velocity[1])
	}
	if pp.Velocity[2] != 26 {
		t.Fatalf("Blob2 v[2] = %v, want 26 (30 - 4)", pp.Velocity[2])
	}
}

func TestRun_PositionIntegratesByVelocity(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{
		Origin:   [3]float32{0, 0, 0},
		Velocity: [3]float32{10, 20, 30},
		Die:      10,
		Type:     ParticleStatic, // Static so no force changes velocity mid-step
	}, 0)
	p.Run(0.5, 0, 1)
	got := p.Particles[0].Origin
	want := [3]float32{5, 10, 15}
	if got != want {
		t.Fatalf("origin = %v, want %v", got, want)
	}
}

func TestRun_ExpiredParticleFreed(t *testing.T) {
	p := NewPool()
	p.Spawn(Particle{Die: 0.5, Type: ParticleStatic}, 0)
	if p.NumAlive != 1 {
		t.Fatalf("NumAlive after spawn = %d, want 1", p.NumAlive)
	}
	// now = 1.0 > Die (0.5) -> expire-before-step branch.
	p.Run(0.1, 0, 1.0)
	if p.Particles[0].Die != 0 {
		t.Fatalf("expired slot Die = %v, want 0", p.Particles[0].Die)
	}
	if p.NumAlive != 0 {
		t.Fatalf("NumAlive after expiry = %d, want 0", p.NumAlive)
	}
}

func TestRun_NumAliveTracksLiveCount(t *testing.T) {
	p := NewPool()
	for i := 0; i < 5; i++ {
		p.Spawn(Particle{Die: 10, Type: ParticleStatic}, 0)
	}
	// Kill two by setting their Die in the past.
	p.Particles[1].Die = 0.1
	p.Particles[3].Die = 0.1
	p.Run(0.01, 0, 1.0)
	if p.NumAlive != 3 {
		t.Fatalf("NumAlive after partial expiry = %d, want 3", p.NumAlive)
	}
}

func TestRun_EmptyPoolNoop(t *testing.T) {
	p := NewPool()
	p.Run(0.1, 800, 1)
	if p.NumAlive != 0 {
		t.Fatalf("empty pool Run touched NumAlive: %d", p.NumAlive)
	}
}

func TestParticleExplosion_AlternatesTypes_DieAtNowPlus5(t *testing.T) {
	p := NewPool()
	// counterRNG is deterministic; we don't care about specific
	// origins/velocities, we just need every Spawn call to succeed
	// until the pool fills.
	ParticleExplosion(p, [3]float32{0, 0, 0}, 100, counterRNG(0))
	// Pool capacity is 2048 < 1024 needed -- ParticleExplosion
	// attempts 1024 spawns, pool absorbs all 1024 (we start empty).
	if p.NumAlive != 1024 {
		t.Fatalf("ParticleExplosion spawned %d, want 1024", p.NumAlive)
	}
	// Slot 0 is iteration i=0 (even) -> Explode2.
	if p.Particles[0].Type != ParticleExplode2 {
		t.Fatalf("slot 0 type = %v, want Explode2 (i=0 even)", p.Particles[0].Type)
	}
	// Slot 1 is iteration i=1 (odd) -> Explode.
	if p.Particles[1].Type != ParticleExplode {
		t.Fatalf("slot 1 type = %v, want Explode (i=1 odd)", p.Particles[1].Type)
	}
	// All particles should have Die = 105, Color = Ramp1[0].
	for i := 0; i < 1024; i++ {
		if p.Particles[i].Die != 105 {
			t.Fatalf("slot %d Die = %v, want 105", i, p.Particles[i].Die)
		}
		if p.Particles[i].Color != Ramp1[0] {
			t.Fatalf("slot %d Color = %#x, want Ramp1[0] = %#x",
				i, p.Particles[i].Color, Ramp1[0])
		}
		if p.Particles[i].Ramp < 0 || p.Particles[i].Ramp > 3 {
			t.Fatalf("slot %d Ramp = %v, want in [0,3]", i, p.Particles[i].Ramp)
		}
	}
}

func TestParticleExplosion_StopsWhenPoolFull(t *testing.T) {
	p := NewPool()
	// Pre-fill the pool so the explosion's Spawn calls immediately
	// fail -- hits the `if !ok return` branch.
	for i := 0; i < MaxParticles; i++ {
		p.Spawn(Particle{Die: 1000, Type: ParticleStatic}, 0)
	}
	before := p.NumAlive
	ParticleExplosion(p, [3]float32{0, 0, 0}, 100, counterRNG(0))
	if p.NumAlive != before {
		t.Fatalf("NumAlive changed despite full pool: %d -> %d", before, p.NumAlive)
	}
}

func TestLavaSplash_Count1024_TypeGrav_VelocityMagnitudeBand(t *testing.T) {
	p := NewPool()
	LavaSplash(p, [3]float32{0, 0, 0}, 50, counterRNG(0))
	// Pool cap (2048) accommodates the full 32*32 = 1024 lava splash.
	if p.NumAlive != 1024 {
		t.Fatalf("LavaSplash count = %d, want 1024", p.NumAlive)
	}
	for i := 0; i < 1024; i++ {
		pp := p.Particles[i]
		if pp.Type != ParticleGrav {
			t.Fatalf("slot %d type = %v, want Grav", i, pp.Type)
		}
		// |vel| = 50 + (rng&63), so in [50, 113].
		mag := math.Sqrt(float64(
			pp.Velocity[0]*pp.Velocity[0] +
				pp.Velocity[1]*pp.Velocity[1] +
				pp.Velocity[2]*pp.Velocity[2]))
		if mag < 49.99 || mag > 113.01 {
			t.Fatalf("slot %d |vel| = %v, want in [50,113]", i, mag)
		}
		// Die in [now+2, now+2.62]. now=50.
		if pp.Die < 52 || pp.Die > 52.62 {
			t.Fatalf("slot %d Die = %v, want in [52, 52.62]", i, pp.Die)
		}
		// Color in [224, 231].
		if pp.Color < 224 || pp.Color > 231 {
			t.Fatalf("slot %d Color = %d, want in [224,231]", i, pp.Color)
		}
	}
}

func TestLavaSplash_StopsWhenPoolFull(t *testing.T) {
	p := NewPool()
	for i := 0; i < MaxParticles; i++ {
		p.Spawn(Particle{Die: 1000, Type: ParticleStatic}, 0)
	}
	before := p.NumAlive
	LavaSplash(p, [3]float32{0, 0, 0}, 50, counterRNG(0))
	if p.NumAlive != before {
		t.Fatalf("NumAlive changed despite full pool: %d -> %d", before, p.NumAlive)
	}
}

func TestTeleportSplash_Count896_TypeGrav_VelocityBand(t *testing.T) {
	p := NewPool()
	// constRNG(0) keeps things deterministic; 8*8*14 = 896 particles.
	TeleportSplash(p, [3]float32{0, 0, 0}, 50, constRNG(0))
	if p.NumAlive != 896 {
		t.Fatalf("TeleportSplash count = %d, want 896", p.NumAlive)
	}
	for i := 0; i < 896; i++ {
		pp := p.Particles[i]
		if pp.Type != ParticleGrav {
			t.Fatalf("slot %d type = %v, want Grav", i, pp.Type)
		}
		// Die in [now+0.2, now+0.34]. now=50.
		if pp.Die < 50.2 || pp.Die > 50.34 {
			t.Fatalf("slot %d Die = %v, want in [50.2, 50.34]", i, pp.Die)
		}
		// Color in [7, 14].
		if pp.Color < 7 || pp.Color > 14 {
			t.Fatalf("slot %d Color = %d, want in [7,14]", i, pp.Color)
		}
	}
}

func TestTeleportSplash_OriginZeroNormalizeGuard(t *testing.T) {
	// When i=j=k=0 the direction vector is (0,0,0) and the normalize
	// divisor is zero -- the implementation guards with inv=0 so the
	// velocity is exactly zero rather than NaN. Verify that branch.
	p := NewPool()
	TeleportSplash(p, [3]float32{0, 0, 0}, 50, constRNG(0))
	// Find the slot for (i,j,k)=(0,0,0). Walking the same loop order:
	// i steps -16,-12,...,0,... (index 4); j same (index 4); k steps
	// -24,-20,...,0,... (index 6). So linear slot = (4*8 + 4)*14 + 6 = 510.
	pp := p.Particles[510]
	if pp.Velocity != [3]float32{0, 0, 0} {
		t.Fatalf("slot 510 (i=j=k=0) velocity = %v, want zero", pp.Velocity)
	}
}

func TestTeleportSplash_StopsWhenPoolFull(t *testing.T) {
	p := NewPool()
	for i := 0; i < MaxParticles; i++ {
		p.Spawn(Particle{Die: 1000, Type: ParticleStatic}, 0)
	}
	before := p.NumAlive
	TeleportSplash(p, [3]float32{0, 0, 0}, 50, counterRNG(0))
	if p.NumAlive != before {
		t.Fatalf("NumAlive changed despite full pool: %d -> %d", before, p.NumAlive)
	}
}

// Ramps must match r_part.c verbatim (lines 40-42).
func TestRamps_MatchTyrquake(t *testing.T) {
	wantRamp1 := [8]byte{0x6f, 0x6d, 0x6b, 0x69, 0x67, 0x65, 0x63, 0x61}
	wantRamp2 := [8]byte{0x6f, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x68, 0x66}
	wantRamp3 := [8]byte{0x6d, 0x6b, 6, 5, 4, 3, 0, 0}
	if Ramp1 != wantRamp1 {
		t.Fatalf("Ramp1 = %v, want %v", Ramp1, wantRamp1)
	}
	if Ramp2 != wantRamp2 {
		t.Fatalf("Ramp2 = %v, want %v", Ramp2, wantRamp2)
	}
	if Ramp3 != wantRamp3 {
		t.Fatalf("Ramp3 = %v, want %v", Ramp3, wantRamp3)
	}
}
