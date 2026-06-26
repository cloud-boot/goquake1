// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "testing"

func TestEmit_NilPoolReturnsZero(t *testing.T) {
	var p *Pool
	if got := p.Emit([3]float32{}, [3]float32{}, 0xff, 10, 0, constRNG(0)); got != 0 {
		t.Fatalf("nil pool spawned %d, want 0", got)
	}
}

func TestEmit_NilRNGReturnsZero(t *testing.T) {
	p := NewPool()
	if got := p.Emit([3]float32{}, [3]float32{}, 0xff, 10, 0, nil); got != 0 {
		t.Fatalf("nil rng spawned %d, want 0", got)
	}
}

func TestEmit_ZeroCountReturnsZero(t *testing.T) {
	p := NewPool()
	if got := p.Emit([3]float32{}, [3]float32{}, 0xff, 0, 0, constRNG(0)); got != 0 {
		t.Fatalf("count=0 spawned %d, want 0", got)
	}
	if got := p.Emit([3]float32{}, [3]float32{}, 0xff, -1, 0, constRNG(0)); got != 0 {
		t.Fatalf("count=-1 spawned %d, want 0", got)
	}
}

func TestEmit_RespectsColorMask(t *testing.T) {
	// baseColor 0xC3 = 1100 0011 -- the top 5 bits = 0xC0, the low 3
	// must come from rng. constRNG(0) feeds zeros, so the low 3 bits
	// resolve to 0 and the colour is exactly 0xC0.
	p := NewPool()
	if got := p.Emit([3]float32{}, [3]float32{}, 0xC3, 1, 0, constRNG(0)); got != 1 {
		t.Fatalf("Emit returned %d, want 1", got)
	}
	if c := p.Particles[0].Color; c != 0xC0 {
		t.Fatalf("color = %#x, want 0xC0", c)
	}
}

func TestEmit_TypeSlowGravAndDieInBand(t *testing.T) {
	p := NewPool()
	now := float32(100)
	p.Emit([3]float32{10, 20, 30}, [3]float32{0, 0, 0}, 0xff, 64, now, counterRNG(0))
	if p.NumAlive == 0 {
		t.Fatalf("nothing spawned")
	}
	for i := 0; i < p.NumAlive; i++ {
		pp := p.Particles[i]
		if pp.Type != ParticleSlowGrav {
			t.Fatalf("slot %d type = %v, want ParticleSlowGrav", i, pp.Type)
		}
		// Die in [now, now + 0.4]; the lifetime is 0.1*(rng%5).
		if pp.Die < now-0.0001 || pp.Die > now+0.4001 {
			t.Fatalf("slot %d Die = %v, want in [%v,%v]", i, pp.Die, now, now+0.4)
		}
	}
}

func TestEmit_PoolFullStopsEarly(t *testing.T) {
	p := NewPool()
	for i := 0; i < MaxParticles; i++ {
		p.Spawn(Particle{Die: 1000, Type: ParticleStatic}, 0)
	}
	before := p.NumAlive
	if got := p.Emit([3]float32{}, [3]float32{}, 0xff, 10, 0, constRNG(0)); got != 0 {
		t.Fatalf("Emit into full pool returned %d, want 0", got)
	}
	if p.NumAlive != before {
		t.Fatalf("NumAlive changed: %d -> %d", before, p.NumAlive)
	}
}

func TestEmit_OriginAndVelocityJittered(t *testing.T) {
	// constRNG(8) means low 5 bits = 8, so jitter = 8 - 16 = -8 on every
	// axis. Origin should be center - 8, Velocity should be dir - 8.
	p := NewPool()
	p.Emit([3]float32{100, 200, 300}, [3]float32{10, 20, 30}, 0xff, 1, 0, constRNG(8))
	pp := p.Particles[0]
	want := [3]float32{92, 192, 292}
	if pp.Origin != want {
		t.Fatalf("Origin = %v, want %v", pp.Origin, want)
	}
	wantVel := [3]float32{2, 12, 22}
	if pp.Velocity != wantVel {
		t.Fatalf("Velocity = %v, want %v", pp.Velocity, wantVel)
	}
}

func TestEmitTrail_NilPool(t *testing.T) {
	var p *Pool
	if got := p.EmitTrail([3]float32{}, [3]float32{1, 0, 0}, TrailRocket, 0, constRNG(0)); got != 0 {
		t.Fatalf("nil pool returned %d, want 0", got)
	}
}

func TestEmitTrail_NilRNG(t *testing.T) {
	p := NewPool()
	if got := p.EmitTrail([3]float32{}, [3]float32{30, 0, 0}, TrailRocket, 0, nil); got != 0 {
		t.Fatalf("nil rng returned %d, want 0", got)
	}
}

func TestEmitTrail_ZeroLengthSegmentReturnsZero(t *testing.T) {
	p := NewPool()
	if got := p.EmitTrail([3]float32{5, 5, 5}, [3]float32{5, 5, 5}, TrailRocket, 0, constRNG(0)); got != 0 {
		t.Fatalf("zero-length segment returned %d, want 0", got)
	}
}

func TestEmitTrail_Rocket_StepCountMatchesLength(t *testing.T) {
	// 30-unit segment / 3-unit step = 10 particles.
	p := NewPool()
	got := p.EmitTrail([3]float32{0, 0, 0}, [3]float32{30, 0, 0}, TrailRocket, 0, constRNG(0))
	if got != 10 {
		t.Fatalf("trail particles = %d, want 10", got)
	}
	for i := 0; i < 10; i++ {
		if p.Particles[i].Type != ParticleFire {
			t.Fatalf("slot %d type = %v, want ParticleFire", i, p.Particles[i].Type)
		}
	}
}

func TestEmitTrail_Grenade_RampStartsAt2(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailGrenade, 0, constRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		if p.Particles[i].Ramp != 2 {
			t.Fatalf("slot %d Ramp = %v, want 2", i, p.Particles[i].Ramp)
		}
		if p.Particles[i].Color != Ramp3[2] {
			t.Fatalf("slot %d Color = %#x, want Ramp3[2]=%#x", i, p.Particles[i].Color, Ramp3[2])
		}
	}
}

func TestEmitTrail_Blood_GravTypeAndPaletteBand(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailBlood, 0, counterRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		if p.Particles[i].Type != ParticleGrav {
			t.Fatalf("slot %d type = %v, want ParticleGrav", i, p.Particles[i].Type)
		}
		c := p.Particles[i].Color
		if c < 67 || c > 70 {
			t.Fatalf("slot %d Color = %d, want in [67,70]", i, c)
		}
	}
}

func TestEmitTrail_SlightBlood_SharesBloodInit(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailSlightBlood, 0, counterRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		if p.Particles[i].Type != ParticleGrav {
			t.Fatalf("slot %d type = %v, want ParticleGrav", i, p.Particles[i].Type)
		}
	}
}

func TestEmitTrail_TracerGreen(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailTracer, 0, constRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		if p.Particles[i].Type != ParticleStatic {
			t.Fatalf("slot %d type = %v, want ParticleStatic", i, p.Particles[i].Type)
		}
		c := p.Particles[i].Color
		if c != 52 && c != 54 {
			t.Fatalf("slot %d Color = %d, want 52 or 54", i, c)
		}
		if p.Particles[i].Die != 0.5 {
			t.Fatalf("slot %d Die = %v, want 0.5", i, p.Particles[i].Die)
		}
	}
	// At least one even-index slot has the +30*ny velocity branch,
	// at least one odd-index slot has the -30*ny branch (since ny=0
	// here both resolve to Velocity[0]=0; assert via Velocity[1]
	// being the projection of -30*nx for even slots).
	// nx = 1, so even slots: Velocity[1] = -30; odd slots: +30.
	if p.Particles[0].Velocity[1] != -30 {
		t.Fatalf("slot 0 Vel[1] = %v, want -30", p.Particles[0].Velocity[1])
	}
	if p.Particles[1].Velocity[1] != 30 {
		t.Fatalf("slot 1 Vel[1] = %v, want 30", p.Particles[1].Velocity[1])
	}
}

func TestEmitTrail_Tracer2Orange(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailTracer2, 0, constRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		c := p.Particles[i].Color
		if c != 230 && c != 232 {
			t.Fatalf("slot %d Color = %d, want 230 or 232", i, c)
		}
	}
}

func TestEmitTrail_Voor(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailVoor, 0, counterRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		c := p.Particles[i].Color
		if c < 152 || c > 155 {
			t.Fatalf("slot %d Color = %d, want in [152,155]", i, c)
		}
		if p.Particles[i].Type != ParticleStatic {
			t.Fatalf("slot %d type = %v, want ParticleStatic", i, p.Particles[i].Type)
		}
	}
}

func TestEmitTrail_UnknownKindFallsThroughToRocket(t *testing.T) {
	p := NewPool()
	p.EmitTrail([3]float32{0, 0, 0}, [3]float32{9, 0, 0}, TrailKind(999), 0, counterRNG(0))
	for i := 0; i < p.NumAlive; i++ {
		if p.Particles[i].Type != ParticleFire {
			t.Fatalf("slot %d type = %v, want ParticleFire (rocket fallback)", i, p.Particles[i].Type)
		}
	}
}

func TestEmitTrail_PoolFullStopsEarly(t *testing.T) {
	p := NewPool()
	for i := 0; i < MaxParticles; i++ {
		p.Spawn(Particle{Die: 1000, Type: ParticleStatic}, 0)
	}
	before := p.NumAlive
	if got := p.EmitTrail([3]float32{0, 0, 0}, [3]float32{30, 0, 0}, TrailRocket, 0, constRNG(0)); got != 0 {
		t.Fatalf("EmitTrail into full pool returned %d, want 0", got)
	}
	if p.NumAlive != before {
		t.Fatalf("NumAlive changed: %d -> %d", before, p.NumAlive)
	}
}
