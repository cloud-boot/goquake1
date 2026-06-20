// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// --- fixtures --------------------------------------------------------------

// physSimpleProgs builds a Progs stub with the fields every per-
// MOVETYPE handler in this file reaches for:
//
//	nextthink  (EvFloat)     -- consumed by server.RunThink
//	think      (EvFunction)  -- consumed by server.RunThink
//	origin     (EvVector)    -- consumed by NoClip + Fly
//	velocity   (EvVector)    -- consumed by NoClip + Fly
//	angles     (EvVector)    -- consumed by NoClip
//	avelocity  (EvVector)    -- consumed by NoClip
//	mins       (EvVector)    -- consumed by Fly
//	maxs       (EvVector)    -- consumed by Fly
//
// EntityFields = 24 reserves enough 4-byte slots: each vector takes
// 3 slots, the two scalars take 1 each. Lay them out non-overlapping
// starting at offset 1 (offset 0 is reserved for the "world" entity
// padding the C upstream leaves blank at the head of the field block).
//
//	ofs  1     nextthink   (float)
//	ofs  2     think       (function)
//	ofs  3..5  origin      (vector)
//	ofs  6..8  velocity    (vector)
//	ofs  9..11 angles      (vector)
//	ofs 12..14 avelocity   (vector)
//	ofs 15..17 mins        (vector)
//	ofs 18..20 maxs        (vector)
func physSimpleProgs() *progs.Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	nextthink := add("nextthink")
	think := add("think")
	origin := add("origin")
	velocity := add("velocity")
	angles := add("angles")
	avelocity := add("avelocity")
	mins := add("mins")
	maxs := add("maxs")
	return &progs.Progs{
		Header:  progs.Header{EntityFields: 24},
		Strings: strs,
		FieldDefs: []progs.Def{
			{Type: uint16(progs.EvFloat), Ofs: 1, SName: nextthink},
			{Type: uint16(progs.EvFunction), Ofs: 2, SName: think},
			{Type: uint16(progs.EvVector), Ofs: 3, SName: origin},
			{Type: uint16(progs.EvVector), Ofs: 6, SName: velocity},
			{Type: uint16(progs.EvVector), Ofs: 9, SName: angles},
			{Type: uint16(progs.EvVector), Ofs: 12, SName: avelocity},
			{Type: uint16(progs.EvVector), Ofs: 15, SName: mins},
			{Type: uint16(progs.EvVector), Ofs: 18, SName: maxs},
		},
	}
}

// newPhysSimpleEnt allocates an Edict on a fresh arena bound to the
// physSimpleProgs stub and hands back the matching EntVars. The
// field block is zeroed by Alloc -- every vector / scalar starts at
// zero.
func newPhysSimpleEnt(t *testing.T) (*progs.Edict, *progs.EntVars, *progs.Progs) {
	t.Helper()
	p := physSimpleProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	v, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	return e, v, p
}

// physSimpleEntFromProgs allocates a fresh Edict from a caller-
// supplied Progs (used by the missing-field tests so the field set
// can be customised).
func physSimpleEntFromProgs(t *testing.T, p *progs.Progs) (*progs.Edict, *progs.EntVars) {
	t.Helper()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	v, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	return e, v
}

// physSimpleDropField clones the standard Progs stub minus one field
// def by name -- so the corresponding EntVars read or write returns
// ErrFieldNotFound. The Progs string table is preserved verbatim.
func physSimpleDropField(omit string) *progs.Progs {
	p := physSimpleProgs()
	kept := p.FieldDefs[:0]
	for _, d := range p.FieldDefs {
		if physSimpleReadName(p, d.SName) == omit {
			continue
		}
		kept = append(kept, d)
	}
	p.FieldDefs = kept
	return p
}

func physSimpleReadName(p *progs.Progs, ofs int32) string {
	if ofs < 0 || int(ofs) >= len(p.Strings) {
		return ""
	}
	end := int(ofs)
	for end < len(p.Strings) && p.Strings[end] != 0 {
		end++
	}
	return string(p.Strings[ofs:end])
}

// physSimpleVec3ApproxEq compares two [3]float32 component-wise within
// tol -- float32 multiply-add accumulates ULP error past exact compare.
func physSimpleVec3ApproxEq(a, b [3]float32, tol float32) bool {
	for i := 0; i < 3; i++ {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > tol {
			return false
		}
	}
	return true
}

// physSimpleNoThink is a ThinkCaller that fails the test if invoked
// -- used in scenarios where nextthink is zero so the caller should
// never see a dispatch.
func physSimpleNoThink(t *testing.T) server.ThinkCaller {
	t.Helper()
	return func(*progs.Edict, int32) error {
		t.Errorf("ThinkCaller invoked unexpectedly")
		return nil
	}
}

// --- PhysicsNone -----------------------------------------------------------

// PhysicsNone with no nextthink scheduled: RunThink returns
// (true, nil), no dispatch happens, PhysicsNone forwards the same
// pair.
func TestPhysicsNone_NoThinkScheduled(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsNone(ent, ev, 7, ctx)
	if !alive || err != nil {
		t.Errorf("alive=%v err=%v want true, nil", alive, err)
	}
}

// PhysicsNone with a nextthink in-range: ThinkCaller fires; the
// dispatch result is surfaced.
func TestPhysicsNone_ThinkFires(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteFloat("nextthink", 1.05); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteInt32("think", 99); err != nil {
		t.Fatal(err)
	}
	var seenFunc int32
	ctx := PhysicsContext{
		Now: 1.0,
		Dt:  0.1,
		ThinkCaller: func(_ *progs.Edict, fid int32) error {
			seenFunc = fid
			return nil
		},
	}
	alive, err := PhysicsNone(ent, ev, 0, ctx)
	if !alive || err != nil {
		t.Errorf("alive=%v err=%v want true, nil", alive, err)
	}
	if seenFunc != 99 {
		t.Errorf("ThinkCaller got funcID=%d want 99", seenFunc)
	}
}

// PhysicsNone with a ThinkCaller that returns an error: the error
// is surfaced verbatim, alive is false.
func TestPhysicsNone_ThinkCallerError(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteFloat("nextthink", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteInt32("think", 1); err != nil {
		t.Fatal(err)
	}
	want := errors.New("boom")
	ctx := PhysicsContext{
		Now: 1.0,
		Dt:  0.1,
		ThinkCaller: func(*progs.Edict, int32) error {
			return want
		},
	}
	alive, err := PhysicsNone(ent, ev, 0, ctx)
	if alive || !errors.Is(err, want) {
		t.Errorf("alive=%v err=%v want false, %v", alive, err, want)
	}
}

// --- PhysicsNoClip ---------------------------------------------------------

// PhysicsNoClip happy path: no think scheduled; origin advances by
// dt*velocity, angles advances by dt*avelocity. Velocity + avelocity
// themselves are unchanged.
func TestPhysicsNoClip_AdvancesOriginAndAngles(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteVec3("origin", [3]float32{10, 20, 30}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("angles", [3]float32{0, 90, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("avelocity", [3]float32{0, 10, 0}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.5,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsNoClip(ent, ev, 0, ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotOrigin, _ := ev.ReadVec3("origin")
	wantOrigin := [3]float32{10.5, 21, 31.5}
	if !physSimpleVec3ApproxEq(gotOrigin, wantOrigin, 1e-5) {
		t.Errorf("origin: got %v want %v", gotOrigin, wantOrigin)
	}
	gotAngles, _ := ev.ReadVec3("angles")
	wantAngles := [3]float32{0, 95, 0}
	if !physSimpleVec3ApproxEq(gotAngles, wantAngles, 1e-5) {
		t.Errorf("angles: got %v want %v", gotAngles, wantAngles)
	}
	// Velocity + avelocity are read-only -- assert they survive.
	if got, _ := ev.ReadVec3("velocity"); got != ([3]float32{1, 2, 3}) {
		t.Errorf("velocity mutated: got %v", got)
	}
	if got, _ := ev.ReadVec3("avelocity"); got != ([3]float32{0, 10, 0}) {
		t.Errorf("avelocity mutated: got %v", got)
	}
}

// PhysicsNoClip with a think that errors: surface the error; origin
// must NOT be updated (we bail before the integrate step).
func TestPhysicsNoClip_ThinkCallerError(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteVec3("origin", [3]float32{10, 20, 30}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteFloat("nextthink", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteInt32("think", 1); err != nil {
		t.Fatal(err)
	}
	want := errors.New("think failed")
	ctx := PhysicsContext{
		Now: 1.0,
		Dt:  0.5,
		ThinkCaller: func(*progs.Edict, int32) error {
			return want
		},
	}
	alive, err := PhysicsNoClip(ent, ev, 0, ctx)
	if alive || !errors.Is(err, want) {
		t.Errorf("alive=%v err=%v want false, %v", alive, err, want)
	}
	if got, _ := ev.ReadVec3("origin"); got != ([3]float32{10, 20, 30}) {
		t.Errorf("origin mutated after think failure: got %v", got)
	}
}

// PhysicsNoClip with the "origin" field missing from Progs: the
// EntVars read of origin returns ErrFieldNotFound; PhysicsNoClip
// surfaces it.
func TestPhysicsNoClip_MissingOriginField(t *testing.T) {
	p := physSimpleDropField("origin")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsNoClip(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsNoClip with the "velocity" field missing: ReadVec3 fails on
// velocity (after the successful origin read).
func TestPhysicsNoClip_MissingVelocityField(t *testing.T) {
	p := physSimpleDropField("velocity")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsNoClip(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsNoClip with "angles" missing.
func TestPhysicsNoClip_MissingAnglesField(t *testing.T) {
	p := physSimpleDropField("angles")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsNoClip(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsNoClip with "avelocity" missing.
func TestPhysicsNoClip_MissingAvelocityField(t *testing.T) {
	p := physSimpleDropField("avelocity")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsNoClip(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// --- PhysicsFly ------------------------------------------------------------

// PhysicsFly happy path: no think scheduled, FlyMove integrates
// origin += velocity * dt against an empty world; velocity is
// preserved (clean slide).
func TestPhysicsFly_CleanIntegration(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{10, 5, -3}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("mins", [3]float32{-1, -1, -1}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("maxs", [3]float32{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1.0,
		Dt:          2.0,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	wantOrigin := [3]float32{20, 10, -6}
	gotOrigin, _ := ev.ReadVec3("origin")
	if !physSimpleVec3ApproxEq(gotOrigin, wantOrigin, 1e-4) {
		t.Errorf("origin: got %v want %v", gotOrigin, wantOrigin)
	}
	gotVel, _ := ev.ReadVec3("velocity")
	if gotVel != ([3]float32{10, 5, -3}) {
		t.Errorf("velocity: got %v want %v (unchanged)", gotVel, [3]float32{10, 5, -3})
	}
}

// PhysicsFly with a think that rewrites velocity before FlyMove
// runs: the integrator must use the POST-think velocity. We rewrite
// velocity from (1, 0, 0) to (100, 0, 0) inside the think, then
// confirm origin moves by 100*dt instead of 1*dt.
func TestPhysicsFly_ThinkRewritesVelocity(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("mins", [3]float32{-1, -1, -1}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("maxs", [3]float32{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteFloat("nextthink", 1.05); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteInt32("think", 7); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel: flyMoveEmptyWorld(),
		Now:        1.0,
		Dt:         0.5,
		ThinkCaller: func(*progs.Edict, int32) error {
			return ev.WriteVec3("velocity", [3]float32{100, 0, 0})
		},
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotOrigin, _ := ev.ReadVec3("origin")
	wantOrigin := [3]float32{50, 0, 0} // 100 * 0.5
	if !physSimpleVec3ApproxEq(gotOrigin, wantOrigin, 1e-4) {
		t.Errorf("origin: got %v want %v", gotOrigin, wantOrigin)
	}
}

// PhysicsFly think error: surface it; FlyMove never runs, origin +
// velocity untouched.
func TestPhysicsFly_ThinkCallerError(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteVec3("origin", [3]float32{5, 5, 5}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteFloat("nextthink", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteInt32("think", 1); err != nil {
		t.Fatal(err)
	}
	want := errors.New("think boom")
	ctx := PhysicsContext{
		Worldmodel: flyMoveCorruptWorld(), // would error if FlyMove ran
		Now:        1.0,
		Dt:         0.5,
		ThinkCaller: func(*progs.Edict, int32) error {
			return want
		},
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if alive || !errors.Is(err, want) {
		t.Errorf("alive=%v err=%v want false, %v", alive, err, want)
	}
	if got, _ := ev.ReadVec3("origin"); got != ([3]float32{5, 5, 5}) {
		t.Errorf("origin mutated after think failure: got %v", got)
	}
}

// PhysicsFly with missing "velocity" field: ReadVec3 fails after a
// successful RunThink (nextthink=0 short-circuits). Surface error.
func TestPhysicsFly_MissingVelocityField(t *testing.T) {
	p := physSimpleDropField("velocity")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1.0,
		Dt:          0.5,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsFly with missing "origin" field.
func TestPhysicsFly_MissingOriginField(t *testing.T) {
	p := physSimpleDropField("origin")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1.0,
		Dt:          0.5,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsFly with missing "mins" field.
func TestPhysicsFly_MissingMinsField(t *testing.T) {
	p := physSimpleDropField("mins")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1.0,
		Dt:          0.5,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsFly with missing "maxs" field.
func TestPhysicsFly_MissingMaxsField(t *testing.T) {
	p := physSimpleDropField("maxs")
	ent, ev := physSimpleEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1.0,
		Dt:          0.5,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// PhysicsFly with a corrupt worldmodel: FlyMove returns an error;
// PhysicsFly surfaces it without writing origin/velocity.
func TestPhysicsFly_FlyMoveError(t *testing.T) {
	ent, ev, _ := newPhysSimpleEnt(t)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{10, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("mins", [3]float32{-1, -1, -1}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("maxs", [3]float32{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  flyMoveCorruptWorld(),
		Now:         1.0,
		Dt:          0.5,
		ThinkCaller: physSimpleNoThink(t),
	}
	alive, err := PhysicsFly(ent, ev, 0, ctx)
	if alive || err == nil {
		t.Errorf("alive=%v err=%v want false + non-nil error", alive, err)
	}
}

// The Write-error paths in PhysicsFly + PhysicsNoClip are
// structurally unreachable after the matching ReadVec3 has succeeded
// (same offset + type, same Edict.FieldSetVector range check). They
// are dropped from the production code per the bsptrace pattern of
// removing C-inherited dead code -- see the inline comments in
// physics_simple.go. No write-error tests are needed.
