// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// pusherThinkCaller returns a ThinkCaller that bumps *fired and
// optionally returns err.
func pusherThinkCaller(fired *int, err error) server.ThinkCaller {
	return func(_ *progs.Edict, _ int32) error {
		*fired++
		return err
	}
}

// pusherEntity allocates an edict with a scheduled think; mover is
// SOLID_BSP / MOVETYPE_PUSH (= a door) and sits at the origin with a
// 16-unit cube bbox.
func pusherEntity(t *testing.T, p *progs.Progs, a *progs.EdictArena) *progs.Edict {
	t.Helper()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	ev, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	_ = ev.WriteFloat("movetype", float32(server.MoveTypePush))
	_ = ev.WriteFloat("solid", float32(server.SolidBSP))
	_ = ev.WriteFloat("nextthink", 0.5)
	_ = ev.WriteInt32("think", 42)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 0})
	_ = ev.WriteVec3("mins", [3]float32{-16, -16, -16})
	_ = ev.WriteVec3("maxs", [3]float32{16, 16, 16})
	return e
}

// Parked pusher (velocity == 0) -- fast-path: origin unchanged, think
// still dispatches when nextthink is in the past.
func TestPhysicsPusher_ParkedRunsThink(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := pusherEntity(t, p, a)
	ev, _ := progs.NewEntVars(p, e)

	fired := 0
	ctx := PhysicsContext{
		Now:         1.0, // > nextthink (0.5)
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(&fired, nil),
	}
	alive, err := PhysicsPusher(e, ev, Key(1), ctx)
	if err != nil {
		t.Fatalf("PhysicsPusher: %v", err)
	}
	if !alive {
		t.Errorf("alive: got false want true")
	}
	if fired != 1 {
		t.Errorf("think fired: got %d want 1", fired)
	}
	// Origin should be unchanged (zero velocity).
	origin, _ := ev.ReadVec3("origin")
	if origin != ([3]float32{0, 0, 0}) {
		t.Errorf("origin: got %v want (0,0,0)", origin)
	}
}

// Moving pusher: integrate origin += velocity * dt.
func TestPhysicsPusher_IntegratesVelocity(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := pusherEntity(t, p, a)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{100, 0, 50})

	fired := 0
	ctx := PhysicsContext{
		Now:         0.1, // < nextthink (0.5) -> think doesn't fire
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(&fired, nil),
	}
	if _, err := PhysicsPusher(e, ev, Key(1), ctx); err != nil {
		t.Fatalf("PhysicsPusher: %v", err)
	}
	// origin += velocity * dt = (10, 0, 5).
	origin, _ := ev.ReadVec3("origin")
	want := [3]float32{10, 0, 5}
	if origin != want {
		t.Errorf("origin: got %v want %v", origin, want)
	}
}

// Moving pusher with avelocity: integrate angles += avelocity * dt.
func TestPhysicsPusher_IntegratesAVelocity(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := pusherEntity(t, p, a)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("avelocity", [3]float32{0, 90, 0}) // 90 deg/sec yaw

	ctx := PhysicsContext{
		Now:         0.1,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(new(int), nil),
	}
	if _, err := PhysicsPusher(e, ev, Key(1), ctx); err != nil {
		t.Fatalf("PhysicsPusher: %v", err)
	}
	angles, _ := ev.ReadVec3("angles")
	want := [3]float32{0, 9, 0}
	if angles != want {
		t.Errorf("angles: got %v want %v", angles, want)
	}
}

// RunThink dispatch error surfaces verbatim.
func TestPhysicsPusher_ThinkErrorPropagates(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := pusherEntity(t, p, a)
	ev, _ := progs.NewEntVars(p, e)

	sentinel := errors.New("pusher think boom")
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(new(int), sentinel),
	}
	_, err := PhysicsPusher(e, ev, Key(1), ctx)
	if !errors.Is(err, sentinel) {
		t.Errorf("err: got %v want %v", err, sentinel)
	}
}

// Velocity field absent: ReadVec3 error surfaces (test stubs without
// velocity declared would otherwise integrate from a zero default).
func TestPhysicsPusher_VelocityMissingErrors(t *testing.T) {
	p := dispatchProgsNoVelocity()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteFloat("movetype", float32(server.MoveTypePush))
	_ = ev.WriteFloat("solid", float32(server.SolidBSP))

	ctx := PhysicsContext{
		Now:         0,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(new(int), nil),
	}
	_, err := PhysicsPusher(e, ev, Key(1), ctx)
	if err == nil {
		t.Fatalf("PhysicsPusher: want error on missing velocity field")
	}
}

// Origin field absent (moving pusher with missing origin) errors.
func TestPhysicsPusher_OriginMissingErrors(t *testing.T) {
	p := dispatchProgsNoOrigin()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{1, 0, 0})

	ctx := PhysicsContext{
		Now:         0,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(new(int), nil),
	}
	_, err := PhysicsPusher(e, ev, Key(1), ctx)
	if err == nil {
		t.Fatalf("PhysicsPusher: want error on missing origin field")
	}
}

// avelocity field absent: tolerated (defaults to zero rotation).
func TestPhysicsPusher_AVelocityMissingTolerated(t *testing.T) {
	p := dispatchProgsNoAVelocity()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 0})
	_ = ev.WriteVec3("velocity", [3]float32{10, 0, 0})
	// no avelocity field, no angles field

	ctx := PhysicsContext{
		Now:         0,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(new(int), nil),
	}
	if _, err := PhysicsPusher(e, ev, Key(1), ctx); err != nil {
		t.Fatalf("PhysicsPusher: %v (want nil with avelocity absent)", err)
	}
	origin, _ := ev.ReadVec3("origin")
	if origin != ([3]float32{1, 0, 0}) {
		t.Errorf("origin: got %v want (1,0,0)", origin)
	}
}

// RunPhysics propagates a Push-handler error verbatim (the dispatcher
// short-circuits on first error from any handler -- this exercises
// the new MoveTypePush case's error-return path).
func TestRunPhysics_PushErrorPropagates(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypePush, server.SolidBSP)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteFloat("nextthink", 0.1) // ensure think fires
	_ = ev.WriteInt32("think", 1)

	sentinel := errors.New("push think boom")
	fired := 0
	ctx := PhysicsContext{
		Now:         1,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(&fired, sentinel),
	}
	pool := []*progs.Edict{e}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		p,
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("err: got %v want %v", err, sentinel)
	}
}

// RunPhysics dispatches MoveTypePush to PhysicsPusher.
func TestRunPhysics_DispatchesPush(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypePush, server.SolidBSP)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{50, 0, 0})

	ctx := PhysicsContext{
		Now:         0,
		Dt:          0.1,
		ThinkCaller: pusherThinkCaller(new(int), nil),
	}
	pool := []*progs.Edict{e}
	if err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		p,
	); err != nil {
		t.Fatalf("RunPhysics: %v", err)
	}
	got, _ := ev.ReadVec3("origin")
	want := [3]float32{5, 0, 0}
	if got != want {
		t.Errorf("origin: got %v want %v", got, want)
	}
}

// --- field-omission progs variants ----------------------------------------

// dispatchProgsNoField returns a dispatchProgs clone with the named
// field's Def removed from FieldDefs so subsequent ReadVec3/ReadFloat
// calls for that name surface ErrFieldNotFound. Used to assert the
// surface-the-error policy for missing required fields.
func dispatchProgsNoField(omit ...string) *progs.Progs {
	p := dispatchProgs()
	skip := make(map[string]bool, len(omit))
	for _, n := range omit {
		skip[n] = true
	}
	out := make([]progs.Def, 0, len(p.FieldDefs))
	for _, f := range p.FieldDefs {
		if skip[p.String(f.SName)] {
			continue
		}
		out = append(out, f)
	}
	p.FieldDefs = out
	return p
}

func dispatchProgsNoVelocity() *progs.Progs  { return dispatchProgsNoField("velocity") }
func dispatchProgsNoOrigin() *progs.Progs    { return dispatchProgsNoField("origin") }
func dispatchProgsNoAVelocity() *progs.Progs { return dispatchProgsNoField("avelocity", "angles") }
