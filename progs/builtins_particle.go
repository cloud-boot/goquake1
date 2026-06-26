// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

// SetParticleSink installs the side-effect closure that
// BuiltinFnParticle invokes on every QC `particle(org, dir, color,
// count)` call. The four arguments mirror the wire shape:
//
//	origin    -- world-space anchor for the burst
//	dir       -- per-axis velocity bias the renderer applies to each
//	             spawned particle on top of its randomised jitter
//	color     -- palette base index (renderer keeps the top 5 bits,
//	             jitters the low 3 per-particle)
//	count     -- number of particles to spawn
//
// nil unwires (BuiltinFnParticle becomes a silent no-op again).
//
// The closure is invoked synchronously from inside the OP_CALL
// dispatch of the QC bytecode; embedders that need thread-safety
// must serialise their pool writes themselves (the bring-up tamago
// path is single-goroutine, so this is a no-op concern there).
//
// Rationale for the callback shape (over a render.Pool* dependency):
// the progs package MUST stay free of any render-layer import so the
// VM remains testable in isolation + so a future server-only build
// (dedicated server with no rasterizer) still links. The embedder
// (quake-tamago) owns the *render.Pool and writes a one-line bridge
// closure that calls Pool.Emit -- the call site is the only place
// where progs + render meet.
func (vm *VM) SetParticleSink(fn func(origin, dir [3]float32, color int, count int)) {
	vm.particleSink = fn
}

// RegisterEffectsBuiltins wires the side-effect builtins this file
// owns into the VM at their canonical index numbers. Today the only
// entry is #48 (particle); the function exists so callers register
// the whole family in one line + so future visual side-effects
// (changelevel, centerprint variants) can land here without
// re-touching every callsite.
func (vm *VM) RegisterEffectsBuiltins() {
	vm.RegisterBuiltin(BuiltinParticle, BuiltinFnParticle)
}

// BuiltinFnParticle implements void particle(vector org, vector dir,
// float color, float count) -- QC builtin #48.
//
// tyrquake: PF_particle in NQ/pr_cmds.c.
//
//	void PF_particle(void)
//	{
//	    float       *org, *dir;
//	    float       color, count;
//	    org = G_VECTOR(OFS_PARM0);
//	    dir = G_VECTOR(OFS_PARM1);
//	    color = G_FLOAT(OFS_PARM2);
//	    count = G_FLOAT(OFS_PARM3);
//	    SV_StartParticle(org, dir, color, count);
//	}
//
// SV_StartParticle on a listen-server (which is what the Go port's
// single-process loopback effectively is) just calls
// R_RunParticleEffect directly; the wire-layer svc_particle hop is
// the dedicated-server path. The Go port collapses both into the
// embedder-supplied sink: the bring-up wires the sink to
// render.Pool.Emit, which IS R_RunParticleEffect.
//
// Without a sink the builtin returns nil (silent no-op) -- the
// shareware progs.dat calls particle() during routine gameplay
// (SpawnBlood, gunshot puffs, ...), so an error return would crash
// the VM on the first damage event. tyrquake itself ships a no-op
// PF_particle when the renderer is dormant (dedicated server).
//
// Color is read as a float (per the QC type signature) and truncated
// to int via the C cast equivalent (= Go int conversion truncates
// toward zero). Count is read the same way and clamped to >= 0 so a
// negative count from a buggy progs.dat doesn't underflow the
// embedder's loop counter.
func BuiltinFnParticle(vm *VM) error {
	if vm == nil || vm.particleSink == nil {
		return nil
	}
	origin, _ := vm.GlobalVector(OfsParm0)
	dir, _ := vm.GlobalVector(OfsParm1)
	colorF, _ := vm.GlobalFloat(OfsParm2)
	countF, _ := vm.GlobalFloat(OfsParm3)
	count := int(countF)
	if count < 0 {
		count = 0
	}
	vm.particleSink(origin, dir, int(colorF), count)
	return nil
}
