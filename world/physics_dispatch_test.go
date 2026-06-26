// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// --- fixtures --------------------------------------------------------------

// dispatchProgs builds a Progs stub with every field that every
// per-MOVETYPE handler in this package (None / NoClip / Fly / Toss /
// Bounce) plus the dispatcher itself (movetype + solid) reaches for.
//
//	ofs  1     nextthink   (float)
//	ofs  2     think       (function)
//	ofs  3     flags       (float; QC bitfield-as-float)
//	ofs  4     movetype    (float; the MOVETYPE_* enum is a float in QC)
//	ofs  5     solid       (float; same QC encoding)
//	ofs  6..8  origin      (vector)
//	ofs  9..11 velocity    (vector)
//	ofs 12..14 angles      (vector)
//	ofs 15..17 avelocity   (vector)
//	ofs 18..20 mins        (vector)
//	ofs 21..23 maxs        (vector)
//
// EntityFields = 24 reserves a 4-byte slot for each.
func dispatchProgs() *progs.Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	nextthink := add("nextthink")
	think := add("think")
	flags := add("flags")
	movetype := add("movetype")
	solid := add("solid")
	origin := add("origin")
	velocity := add("velocity")
	angles := add("angles")
	avelocity := add("avelocity")
	mins := add("mins")
	maxs := add("maxs")
	gravity := add("gravity")
	vAngle := add("v_angle")
	return &progs.Progs{
		Header:  progs.Header{EntityFields: 28},
		Strings: strs,
		FieldDefs: []progs.Def{
			{Type: uint16(progs.EvFloat), Ofs: 1, SName: nextthink},
			{Type: uint16(progs.EvFunction), Ofs: 2, SName: think},
			{Type: uint16(progs.EvFloat), Ofs: 3, SName: flags},
			{Type: uint16(progs.EvFloat), Ofs: 4, SName: movetype},
			{Type: uint16(progs.EvFloat), Ofs: 5, SName: solid},
			{Type: uint16(progs.EvVector), Ofs: 6, SName: origin},
			{Type: uint16(progs.EvVector), Ofs: 9, SName: velocity},
			{Type: uint16(progs.EvVector), Ofs: 12, SName: angles},
			{Type: uint16(progs.EvVector), Ofs: 15, SName: avelocity},
			{Type: uint16(progs.EvVector), Ofs: 18, SName: mins},
			{Type: uint16(progs.EvVector), Ofs: 21, SName: maxs},
			{Type: uint16(progs.EvFloat), Ofs: 24, SName: gravity},
			{Type: uint16(progs.EvVector), Ofs: 25, SName: vAngle},
		},
	}
}

// dispatchDropField clones the standard dispatch stub minus one field
// def by name -- so the matching EntVars read fails with
// ErrFieldNotFound.
func dispatchDropField(omit string) *progs.Progs {
	p := dispatchProgs()
	kept := p.FieldDefs[:0]
	for _, d := range p.FieldDefs {
		if dispatchReadName(p, d.SName) == omit {
			continue
		}
		kept = append(kept, d)
	}
	p.FieldDefs = kept
	return p
}

func dispatchReadName(p *progs.Progs, ofs int32) string {
	if ofs < 0 || int(ofs) >= len(p.Strings) {
		return ""
	}
	end := int(ofs)
	for end < len(p.Strings) && p.Strings[end] != 0 {
		end++
	}
	return string(p.Strings[ofs:end])
}

// dispatchEntity allocates a fresh Edict on the given Progs, writes
// the requested movetype + solid into its entvars, and returns the
// edict. Tests build a pool of these to drive the dispatcher.
func dispatchEntity(t *testing.T, p *progs.Progs, a *progs.EdictArena, mt server.MoveType, solid server.Solid) *progs.Edict {
	t.Helper()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	ev, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	if err := ev.WriteFloat("movetype", float32(mt)); err != nil {
		t.Fatalf("WriteFloat movetype: %v", err)
	}
	if err := ev.WriteFloat("solid", float32(solid)); err != nil {
		t.Fatalf("WriteFloat solid: %v", err)
	}
	// Seed bounds + zero vectors so the FLY / TOSS handlers' ReadVec3
	// calls land on real fields. Tiny unit bounding box centred on the
	// origin -- matches the toss_test baseline.
	_ = ev.WriteVec3("mins", [3]float32{-1, -1, -1})
	_ = ev.WriteVec3("maxs", [3]float32{1, 1, 1})
	return e
}

// dispatchNoThink is the ThinkCaller every dispatch test ships -- the
// per-handler RunThink path needs a non-nil caller even when no
// nextthink is scheduled (RunThink returns ErrNoThinkCaller on nil).
func dispatchNoThink(t *testing.T) server.ThinkCaller {
	t.Helper()
	return func(*progs.Edict, int32) error {
		t.Errorf("ThinkCaller invoked unexpectedly")
		return nil
	}
}

// dispatchKey is the canonical Key resolver -- slot index == Key.
func dispatchKey(i int) Key { return Key(i) }

// dispatchNoCmd is the canonical UserCmd resolver -- every slot maps
// to a zero command (the dispatcher does not consume cmds in this
// commit; the hook is wired through for future PhysicsWalk parity).
func dispatchNoCmd(int) server.UserCmd { return server.UserCmd{} }

// --- tests -----------------------------------------------------------------

// Empty pool: numEdicts=0 -> no handler invocations, nil error. The
// edictAt / cmdAt / keyAt resolvers t.Errorf on call so we'd catch any
// accidental iteration.
func TestRunPhysics_EmptyPool(t *testing.T) {
	p := dispatchProgs()
	edictAt := func(int) *progs.Edict {
		t.Errorf("edictAt called on empty pool")
		return nil
	}
	cmdAt := func(int) server.UserCmd {
		t.Errorf("cmdAt called on empty pool")
		return server.UserCmd{}
	}
	keyAt := func(int) Key {
		t.Errorf("keyAt called on empty pool")
		return 0
	}
	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	if err := RunPhysics(0, server.DefaultPhysParams(), ctx, edictAt, cmdAt, keyAt, p); err != nil {
		t.Errorf("RunPhysics empty pool: err=%v want nil", err)
	}
}

// nil-edict slots are skipped silently. Build a 3-slot pool where
// slot 1 is nil; the dispatcher must not call any handler for that
// slot and must keep going through the rest.
func TestRunPhysics_NilEdictsSkipped(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 4)
	a.Reset()
	e0 := dispatchEntity(t, p, a, server.MoveTypeNoClip, server.SolidBBox)
	e2 := dispatchEntity(t, p, a, server.MoveTypeNoClip, server.SolidBBox)
	pool := []*progs.Edict{e0, nil, e2}

	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		p,
	)
	if err != nil {
		t.Errorf("RunPhysics: err=%v want nil", err)
	}
}

// Free-entity skip: a (MoveTypeNone, SolidNot) edict has no per-tic
// work and must NOT route into PhysicsNone (which would invoke
// RunThink). We use a nil ThinkCaller to assert the skip: if
// PhysicsNone ran, RunThink would return ErrNoThinkCaller.
func TestRunPhysics_FreeEntitySkipped(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeNone, server.SolidNot)
	pool := []*progs.Edict{e}

	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: nil}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		p,
	)
	if err != nil {
		t.Errorf("RunPhysics free-entity: err=%v want nil (skip without dispatch)", err)
	}
}

// MoveTypeNone (non-free, e.g. SolidBSP world/static): dispatched
// into PhysicsNone, which calls RunThink. We inject a counting
// ThinkCaller and a nextthink scheduled to fire to confirm the route.
func TestRunPhysics_DispatchesNone(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeNone, server.SolidBSP)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteFloat("nextthink", 1.05)
	_ = ev.WriteInt32("think", 42)

	var seen int32
	ctx := PhysicsContext{
		Now: 1, Dt: 0.1,
		ThinkCaller: func(_ *progs.Edict, fid int32) error {
			seen = fid
			return nil
		},
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
	if err != nil {
		t.Fatalf("RunPhysics: err=%v want nil", err)
	}
	if seen != 42 {
		t.Errorf("PhysicsNone not dispatched: ThinkCaller funcID=%d want 42", seen)
	}
}

// MoveTypeNoClip: dispatched into PhysicsNoClip, which advances
// origin by dt*velocity. Confirm the post-pass origin reflects the
// integration step.
func TestRunPhysics_DispatchesNoClip(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeNoClip, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 0})
	_ = ev.WriteVec3("velocity", [3]float32{10, 0, 0})

	ctx := PhysicsContext{Now: 1, Dt: 0.5, ThinkCaller: dispatchNoThink(t)}
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
		t.Fatalf("RunPhysics: err=%v want nil", err)
	}
	got, _ := ev.ReadVec3("origin")
	want := [3]float32{5, 0, 0} // 10 * 0.5
	if got != want {
		t.Errorf("origin: got %v want %v", got, want)
	}
}

// MoveTypeFly: dispatched into PhysicsFly, which runs RunThink +
// FlyMove. Confirm origin advances against an empty world.
func TestRunPhysics_DispatchesFly(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeFly, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 0})
	_ = ev.WriteVec3("velocity", [3]float32{4, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.5,
		ThinkCaller: dispatchNoThink(t),
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
		t.Fatalf("RunPhysics: err=%v want nil", err)
	}
	got, _ := ev.ReadVec3("origin")
	want := [3]float32{2, 0, 0}
	if !physSimpleVec3ApproxEq(got, want, 1e-4) {
		t.Errorf("origin: got %v want %v", got, want)
	}
}

// MoveTypeToss: dispatched into PhysicsToss. Without an FL_ONGROUND
// short-circuit, PhysicsToss applies gravity to velocity[2]. Confirm
// the post-pass velocity[2] has been decremented by gravity*dt.
func TestRunPhysics_DispatchesToss(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeToss, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 100})
	_ = ev.WriteVec3("velocity", [3]float32{1, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
		t.Fatalf("RunPhysics: err=%v want nil", err)
	}
	got, _ := ev.ReadVec3("velocity")
	// gravity=800, dt=0.1 -> v[2] -= 80 (starting from 0).
	want := [3]float32{1, 0, -80}
	if !physSimpleVec3ApproxEq(got, want, 1e-3) {
		t.Errorf("velocity after Toss dispatch: got %v want %v", got, want)
	}
}

// MoveTypeBounce: dispatched into PhysicsBounce. Same gravity check
// as the Toss test confirms the routing (Toss and Bounce share their
// gravity step verbatim; the dispatcher hits the BOUNCE-specific
// case arm because the movetype value differs).
func TestRunPhysics_DispatchesBounce(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeBounce, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 100})
	_ = ev.WriteVec3("velocity", [3]float32{1, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
		t.Fatalf("RunPhysics: err=%v want nil", err)
	}
	got, _ := ev.ReadVec3("velocity")
	want := [3]float32{1, 0, -80}
	if !physSimpleVec3ApproxEq(got, want, 1e-3) {
		t.Errorf("velocity after Bounce dispatch: got %v want %v", got, want)
	}
}

// MoveTypeFlyMissile: routed to PhysicsToss (the dispatch table maps
// the two together because the kinematics are identical -- the C
// upstream merges them in the same arm). Confirm the same gravity
// behaviour as Toss.
func TestRunPhysics_DispatchesFlyMissile(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeFlyMissile, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{1, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
		t.Fatalf("RunPhysics: err=%v want nil", err)
	}
	got, _ := ev.ReadVec3("velocity")
	want := [3]float32{1, 0, -80}
	if !physSimpleVec3ApproxEq(got, want, 1e-3) {
		t.Errorf("velocity after FlyMissile dispatch: got %v want %v", got, want)
	}
}

// Unknown / unsupported movetypes are silently skipped: AngleClip,
// AngleNoClip, and any out-of-enum value all go through the default
// arm. Use a nil ThinkCaller to assert no handler routed (any of
// None/NoClip/Fly/Toss/Bounce/Push would have surfaced
// ErrNoThinkCaller via their RunThink call).
func TestRunPhysics_SkipsUnsupportedMovetypes(t *testing.T) {
	cases := []server.MoveType{
		// Walk + Step + Push now WIRED through PhysicsWalk +
		// PhysicsStep + PhysicsPusher; only Angle* / unknown stay in
		// the silent-skip default arm.
		server.MoveTypeAngleClip,
		server.MoveTypeAngleNoClip,
		server.MoveType(99), // genuinely-unknown
	}
	for _, mt := range cases {
		mt := mt
		t.Run(fmt.Sprintf("mt=%d", int32(mt)), func(t *testing.T) {
			p := dispatchProgs()
			a := progs.NewEdictArena(p, 2)
			a.Reset()
			e := dispatchEntity(t, p, a, mt, server.SolidBBox)
			pool := []*progs.Edict{e}

			ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: nil}
			err := RunPhysics(
				len(pool),
				server.DefaultPhysParams(),
				ctx,
				func(i int) *progs.Edict { return pool[i] },
				dispatchNoCmd,
				dispatchKey,
				p,
			)
			if err != nil {
				t.Errorf("movetype %v: err=%v want nil (silent skip)", mt, err)
			}
		})
	}
}

// First handler error short-circuits the loop: the dispatcher must
// NOT process subsequent edicts. We seed two NoClip edicts and a
// ThinkCaller that errors on the first call; the second edict's
// origin must stay untouched.
func TestRunPhysics_ShortCircuitsOnError(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 3)
	a.Reset()
	e0 := dispatchEntity(t, p, a, server.MoveTypeNoClip, server.SolidBBox)
	e1 := dispatchEntity(t, p, a, server.MoveTypeNoClip, server.SolidBBox)
	ev0, _ := progs.NewEntVars(p, e0)
	ev1, _ := progs.NewEntVars(p, e1)
	_ = ev0.WriteVec3("origin", [3]float32{100, 0, 0})
	_ = ev1.WriteVec3("origin", [3]float32{200, 0, 0})
	_ = ev0.WriteVec3("velocity", [3]float32{10, 0, 0})
	_ = ev1.WriteVec3("velocity", [3]float32{10, 0, 0})
	// Schedule a think on edict 0 so RunThink dispatches there +
	// surfaces the caller's error. Edict 1 has no nextthink so it
	// would NOT trigger the caller -- but we never reach it.
	_ = ev0.WriteFloat("nextthink", 1.05)
	_ = ev0.WriteInt32("think", 1)

	want := errors.New("first edict explodes")
	ctx := PhysicsContext{
		Now: 1, Dt: 0.5,
		ThinkCaller: func(*progs.Edict, int32) error { return want },
	}
	pool := []*progs.Edict{e0, e1}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		p,
	)
	if !errors.Is(err, want) {
		t.Errorf("RunPhysics: err=%v want %v", err, want)
	}
	// Edict 1's origin must not have been integrated.
	got, _ := ev1.ReadVec3("origin")
	if got != ([3]float32{200, 0, 0}) {
		t.Errorf("edict 1 origin mutated after short-circuit: got %v", got)
	}
}

// NewEntVars-bind error path: progsHandle = nil -> progs.NewEntVars
// returns ErrNilArg; the dispatcher surfaces it.
func TestRunPhysics_NilProgsHandle(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeNone, server.SolidBSP)
	pool := []*progs.Edict{e}

	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		nil,
	)
	if !errors.Is(err, progs.ErrNilArg) {
		t.Errorf("RunPhysics nil progs: err=%v want ErrNilArg", err)
	}
}

// Missing "movetype" field: the EntVars ReadFloat fails with
// ErrFieldNotFound; the dispatcher surfaces it.
func TestRunPhysics_MissingMovetypeField(t *testing.T) {
	// Build the entity on the FULL progs so dispatchEntity can write
	// movetype + solid, then re-bind the dispatcher against a
	// movetype-less progs so the read fails.
	full := dispatchProgs()
	a := progs.NewEdictArena(full, 2)
	a.Reset()
	e := dispatchEntity(t, full, a, server.MoveTypeNoClip, server.SolidBBox)

	broken := dispatchDropField("movetype")
	pool := []*progs.Edict{e}

	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		broken,
	)
	if !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("RunPhysics missing movetype: err=%v want ErrFieldNotFound", err)
	}
}

// Missing "solid" field: ReadFloat fails after the movetype read
// succeeds; the dispatcher surfaces it.
func TestRunPhysics_MissingSolidField(t *testing.T) {
	full := dispatchProgs()
	a := progs.NewEdictArena(full, 2)
	a.Reset()
	e := dispatchEntity(t, full, a, server.MoveTypeNoClip, server.SolidBBox)

	broken := dispatchDropField("solid")
	pool := []*progs.Edict{e}

	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		broken,
	)
	if !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("RunPhysics missing solid: err=%v want ErrFieldNotFound", err)
	}
}

// Per-handler error from PhysicsFly: a corrupt worldmodel causes
// FlyMove to error; the dispatcher surfaces it verbatim.
func TestRunPhysics_FlyHandlerError(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeFly, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{10, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  flyMoveCorruptWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
	if err == nil {
		t.Errorf("RunPhysics fly corrupt world: err=nil want non-nil")
	}
}

// Per-handler error from PhysicsToss: a corrupt worldmodel makes
// PushEntity error; the dispatcher surfaces it.
func TestRunPhysics_TossHandlerError(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeToss, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{10, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  tossCorruptWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
	if err == nil {
		t.Errorf("RunPhysics toss corrupt world: err=nil want non-nil")
	}
}

// Per-handler error from PhysicsBounce: same shape as the Toss
// handler-error test, asserts the Bounce-arm dispatch surfaces
// errors too.
func TestRunPhysics_BounceHandlerError(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeBounce, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("velocity", [3]float32{10, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  tossCorruptWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
	if err == nil {
		t.Errorf("RunPhysics bounce corrupt world: err=nil want non-nil")
	}
}

// PhysicsNoClip surfaces an error path too: a missing "origin" field
// makes the handler ReadVec3 fail; the dispatcher surfaces it.
func TestRunPhysics_NoClipHandlerError(t *testing.T) {
	full := dispatchProgs()
	a := progs.NewEdictArena(full, 2)
	a.Reset()
	e := dispatchEntity(t, full, a, server.MoveTypeNoClip, server.SolidBBox)

	// Rebuild a Progs that has movetype + solid (so the dispatcher
	// gets past its own reads) but is missing "origin" (so the
	// NoClip handler errors when it tries to read it).
	broken := dispatchDropField("origin")
	pool := []*progs.Edict{e}

	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		broken,
	)
	if !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("RunPhysics noclip missing origin: err=%v want ErrFieldNotFound", err)
	}
}

// PhysicsNone error path: surface a ThinkCaller error from the
// dispatched handler.
func TestRunPhysics_NoneHandlerError(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeNone, server.SolidBSP)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteFloat("nextthink", 1.05)
	_ = ev.WriteInt32("think", 1)

	want := errors.New("none think boom")
	ctx := PhysicsContext{
		Now: 1, Dt: 0.1,
		ThinkCaller: func(*progs.Edict, int32) error { return want },
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
	if !errors.Is(err, want) {
		t.Errorf("RunPhysics None handler error: err=%v want %v", err, want)
	}
}

// Mixed pool: every supported movetype in a single pass. Exercises
// the dispatch table end-to-end and asserts no handler short-
// circuits on the happy path.
func TestRunPhysics_MixedPool(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 8)
	a.Reset()

	eNone := dispatchEntity(t, p, a, server.MoveTypeNone, server.SolidBSP)
	eNoClip := dispatchEntity(t, p, a, server.MoveTypeNoClip, server.SolidBBox)
	eFly := dispatchEntity(t, p, a, server.MoveTypeFly, server.SolidBBox)
	eToss := dispatchEntity(t, p, a, server.MoveTypeToss, server.SolidBBox)
	eBounce := dispatchEntity(t, p, a, server.MoveTypeBounce, server.SolidBBox)
	eFM := dispatchEntity(t, p, a, server.MoveTypeFlyMissile, server.SolidBBox)
	eFree := dispatchEntity(t, p, a, server.MoveTypeNone, server.SolidNot)

	pool := []*progs.Edict{eNone, eNoClip, eFly, eToss, eBounce, eFM, eFree}

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: func(*progs.Edict, int32) error { return nil },
	}
	err := RunPhysics(
		len(pool),
		server.DefaultPhysParams(),
		ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd,
		dispatchKey,
		p,
	)
	if err != nil {
		t.Errorf("RunPhysics mixed pool: err=%v want nil", err)
	}
}

// MoveTypeStep + MoveTypeWalk are wired (post-batch10). These tests
// just confirm the new dispatch arms route to the handlers without
// erroring; the handlers' detailed behaviour is exercised in
// physics_ground_test.go.

func TestRunPhysics_DispatchesStep(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeStep, server.SolidBBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 100})
	_ = ev.WriteVec3("velocity", [3]float32{0, 0, 0})

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
		t.Fatalf("RunPhysics Step: err=%v want nil", err)
	}
}
func TestRunPhysics_DispatchesWalk(t *testing.T) {
	p := dispatchProgs()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeWalk, server.SolidSlideBox)
	ev, _ := progs.NewEntVars(p, e)
	_ = ev.WriteVec3("origin", [3]float32{0, 0, 100})
	_ = ev.WriteVec3("velocity", [3]float32{0, 0, 0})
	_ = ev.WriteVec3("v_angle", [3]float32{0, 0, 0})
	_ = ev.WriteFloat("gravity", 1)
	_ = ev.WriteFloat("flags", float32(server.FlagOnGround))

	ctx := PhysicsContext{
		Worldmodel:  flyMoveEmptyWorld(),
		Now:         1,
		Dt:          0.1,
		ThinkCaller: dispatchNoThink(t),
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
		t.Fatalf("RunPhysics Walk: err=%v want nil", err)
	}
}

// Step + Walk error propagation: drop a required field so the
// handler's read fails, verify the dispatcher surfaces the error.
func TestRunPhysics_StepErrorPropagates(t *testing.T) {
	p := dispatchDropField("velocity")
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeStep, server.SolidBBox)
	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	pool := []*progs.Edict{e}
	err := RunPhysics(len(pool), server.DefaultPhysParams(), ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd, dispatchKey, p)
	if err == nil {
		t.Error("expected error from PhysicsStep")
	}
}

func TestRunPhysics_WalkErrorPropagates(t *testing.T) {
	p := dispatchDropField("velocity")
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e := dispatchEntity(t, p, a, server.MoveTypeWalk, server.SolidSlideBox)
	ctx := PhysicsContext{Now: 1, Dt: 0.1, ThinkCaller: dispatchNoThink(t)}
	pool := []*progs.Edict{e}
	err := RunPhysics(len(pool), server.DefaultPhysParams(), ctx,
		func(i int) *progs.Edict { return pool[i] },
		dispatchNoCmd, dispatchKey, p)
	if err == nil {
		t.Error("expected error from PhysicsWalk")
	}
}
