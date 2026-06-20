// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// --- fixtures --------------------------------------------------------------

// tossProgs builds a Progs stub with every field PhysicsToss /
// PhysicsBounce reach for, plus an optional `gravity` field whose
// presence is controlled by withGravity. The C upstream's SV_AddGravity
// uses GetEdictFieldValue, which returns NULL when the field is
// absent -> default scale 1.0; we exercise both branches.
//
//	ofs  1     nextthink   (float)
//	ofs  2     think       (function)
//	ofs  3     flags       (float; QC stores the FL_* bitfield as float)
//	ofs  4..6  origin      (vector)
//	ofs  7..9  velocity    (vector)
//	ofs 10..12 angles      (vector)
//	ofs 13..15 avelocity   (vector)
//	ofs 16..18 mins        (vector)
//	ofs 19..21 maxs        (vector)
//	ofs 22     gravity     (float, optional)
//
// EntityFields = 24 reserves enough room.
func tossProgs(withGravity bool) *progs.Progs {
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
	origin := add("origin")
	velocity := add("velocity")
	angles := add("angles")
	avelocity := add("avelocity")
	mins := add("mins")
	maxs := add("maxs")
	gravity := add("gravity")
	defs := []progs.Def{
		{Type: uint16(progs.EvFloat), Ofs: 1, SName: nextthink},
		{Type: uint16(progs.EvFunction), Ofs: 2, SName: think},
		{Type: uint16(progs.EvFloat), Ofs: 3, SName: flags},
		{Type: uint16(progs.EvVector), Ofs: 4, SName: origin},
		{Type: uint16(progs.EvVector), Ofs: 7, SName: velocity},
		{Type: uint16(progs.EvVector), Ofs: 10, SName: angles},
		{Type: uint16(progs.EvVector), Ofs: 13, SName: avelocity},
		{Type: uint16(progs.EvVector), Ofs: 16, SName: mins},
		{Type: uint16(progs.EvVector), Ofs: 19, SName: maxs},
	}
	if withGravity {
		defs = append(defs, progs.Def{Type: uint16(progs.EvFloat), Ofs: 22, SName: gravity})
	}
	return &progs.Progs{
		Header:    progs.Header{EntityFields: 24},
		Strings:   strs,
		FieldDefs: defs,
	}
}

// newTossEnt allocates a fresh Edict on the standard tossProgs stub
// and hands back the matching EntVars + Progs. The field block is
// zeroed by Alloc -- every field starts at zero.
func newTossEnt(t *testing.T, withGravity bool) (*progs.Edict, *progs.EntVars, *progs.Progs) {
	t.Helper()
	p := tossProgs(withGravity)
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

// newTossEntFromProgs allocates a fresh Edict from a caller-supplied
// Progs (used by the missing-field / corrupt-field tests so the
// field set can be customised).
func newTossEntFromProgs(t *testing.T, p *progs.Progs) (*progs.Edict, *progs.EntVars) {
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

// tossDropField clones the standard tossProgs stub minus one field
// def by name -- so the corresponding EntVars read returns
// ErrFieldNotFound. The Progs string table is preserved verbatim.
func tossDropField(omit string) *progs.Progs {
	p := tossProgs(true)
	kept := p.FieldDefs[:0]
	for _, d := range p.FieldDefs {
		if tossReadName(p, d.SName) == omit {
			continue
		}
		kept = append(kept, d)
	}
	p.FieldDefs = kept
	return p
}

// tossReplaceFieldType clones the standard tossProgs stub with a
// single field's QC type tag rewritten -- used by the type-mismatch
// path on `gravity` (a non-EvFloat gravity field is a corrupt-progs
// signal that the readGravityFactor branch surfaces verbatim).
func tossReplaceFieldType(name string, t Etype) *progs.Progs {
	p := tossProgs(true)
	for i := range p.FieldDefs {
		if tossReadName(p, p.FieldDefs[i].SName) == name {
			p.FieldDefs[i].Type = uint16(t)
		}
	}
	return p
}

// Etype is a local alias for progs.Etype so the test fixture above
// can stay terse without importing the constant block twice.
type Etype = progs.Etype

func tossReadName(p *progs.Progs, ofs int32) string {
	if ofs < 0 || int(ofs) >= len(p.Strings) {
		return ""
	}
	end := int(ofs)
	for end < len(p.Strings) && p.Strings[end] != 0 {
		end++
	}
	return string(p.Strings[ofs:end])
}

// tossVec3ApproxEq compares two [3]float32 within tol -- gravity +
// ClipVelocity accumulate ULP error past exact compare.
func tossVec3ApproxEq(a, b [3]float32, tol float32) bool {
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

// tossNoThink returns a ThinkCaller that t.Errorf's if invoked --
// used in scenarios where nextthink is zero so the caller should
// never see a dispatch.
func tossNoThink(t *testing.T) server.ThinkCaller {
	t.Helper()
	return func(*progs.Edict, int32) error {
		t.Errorf("ThinkCaller invoked unexpectedly")
		return nil
	}
}

// tossEmptyWorld returns a brushmodel whose hull 0 is "every leaf
// empty" -- the projectile flies unimpeded through every trace.
func tossEmptyWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// tossFloorWorld returns a brushmodel whose hull 0 has a horizontal
// floor at z=0: z >= 0 is empty, z < 0 is solid. A down-trace from
// z > 0 impacts at z=0 with normal[2]=1 (a horizontal floor that
// PhysicsToss latches on to via FL_ONGROUND).
func tossFloorWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
		},
		Planes: []bspfile.Plane{
			{Normal: [3]float32{0, 0, 1}, Dist: 0, Type: bspfile.PlaneZ},
		},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// tossWallWorld returns a brushmodel whose hull 0 has a vertical
// wall at x=0: x <= 0 is empty, x > 0 is solid. A +x trace impacts
// at x=0 with normal[2]=0 (a vertical wall: ClipVelocity reflects
// horizontally, no FL_ONGROUND latch).
func tossWallWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	bm.Hulls[0] = bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 0, Children: [2]int16{bspfile.ContentsSolid, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	return bm
}

// tossCorruptWorld returns a brushmodel whose hull 0 contains a
// clipnode pointing at a non-existent plane -- any trace returns
// bsptrace.ErrBadPlaneIndex. Used to surface PushEntity errors.
func tossCorruptWorld() *model.BrushModel {
	bm := &model.BrushModel{}
	corrupt := bsptrace.Hull{
		ClipNodes: []bspfile.ClipNode{
			{PlaneNum: 99, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsEmpty}},
		},
		Planes:        []bspfile.Plane{{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX}},
		FirstClipNode: 0,
		LastClipNode:  0,
	}
	bm.Hulls[0] = corrupt
	bm.Hulls[1] = corrupt
	bm.Hulls[2] = corrupt
	return bm
}

// seedTossBaseline writes a standard starting state into ev: tiny
// bounding box centred on the origin, zero velocity, zero angles,
// zero flags. Individual tests overwrite the specific fields they
// care about after calling this helper.
func seedTossBaseline(t *testing.T, ev *progs.EntVars) {
	t.Helper()
	if err := ev.WriteVec3("mins", [3]float32{-1, -1, -1}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("maxs", [3]float32{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
}

// --- PhysicsToss: gravity --------------------------------------------------

// PhysicsToss on a stationary entity adds gravity: velocity[2]
// becomes negative; origin[2] drops by the post-gravity delta over
// the tick.
func TestPhysicsToss_StationaryGetsGravity(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 100}); err != nil {
		t.Fatal(err)
	}
	// Velocity starts at (0,0,0); gravity will pull it down to
	// (0, 0, -800 * 0.1) = (0, 0, -80) post-gravity. PushEntity in
	// an empty world applies that across 0.1s -> origin[2] = 100 + (-80)*0.1 = 92.
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotV, _ := ev.ReadVec3("velocity")
	if gotV[2] >= 0 {
		t.Errorf("velocity[2] should be negative after gravity: got %v", gotV[2])
	}
	wantV := [3]float32{0, 0, -80}
	if !tossVec3ApproxEq(gotV, wantV, 1e-3) {
		t.Errorf("velocity: got %v want %v", gotV, wantV)
	}
	gotO, _ := ev.ReadVec3("origin")
	wantO := [3]float32{0, 0, 92}
	if !tossVec3ApproxEq(gotO, wantO, 1e-3) {
		t.Errorf("origin: got %v want %v", gotO, wantO)
	}
}

// PhysicsToss on a FL_ONGROUND entity skips entirely: velocity +
// origin unchanged, no gravity, no PushEntity. Mirrors the C
// upstream's `if ((int)ent->v.flags & FL_ONGROUND) return;` guard.
func TestPhysicsToss_OnGroundShortCircuits(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{5, 5, 5}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteFloat("flags", float32(int32(server.FlagOnGround))); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossCorruptWorld(), // would error if PushEntity ran
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	if gotV, _ := ev.ReadVec3("velocity"); gotV != ([3]float32{5, 5, 5}) {
		t.Errorf("velocity mutated on ONGROUND skip: got %v", gotV)
	}
	if gotO, _ := ev.ReadVec3("origin"); gotO != ([3]float32{0, 0, 0}) {
		t.Errorf("origin mutated on ONGROUND skip: got %v", gotO)
	}
}

// PhysicsToss with the QC `gravity` field absent: ApplyGravity uses
// the default scale 1.0 (via tossDefaultGravity). Same expected
// post-tick velocity as the standard withGravity=true case with
// gravity=0 (which also falls back to 1.0).
func TestPhysicsToss_AbsentGravityFieldDefaults(t *testing.T) {
	ent, ev, _ := newTossEnt(t, false) // no gravity field at all
	seedTossBaseline(t, ev)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 100}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotV, _ := ev.ReadVec3("velocity")
	wantV := [3]float32{0, 0, -80}
	if !tossVec3ApproxEq(gotV, wantV, 1e-3) {
		t.Errorf("velocity: got %v want %v (gravity field absent -> scale=1.0)", gotV, wantV)
	}
}

// PhysicsToss with QC `gravity` = 0.5: half-gravity, half the
// vertical velocity drop.
func TestPhysicsToss_PerEntityGravityScale(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	if err := ev.WriteFloat("gravity", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 100}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotV, _ := ev.ReadVec3("velocity")
	wantV := [3]float32{0, 0, -40} // 0.5 * 800 * 0.1
	if !tossVec3ApproxEq(gotV, wantV, 1e-3) {
		t.Errorf("velocity: got %v want %v (gravity=0.5)", gotV, wantV)
	}
}

// --- PhysicsToss: impact + ground latch ------------------------------------

// PhysicsToss impacting a horizontal floor: velocity drops to (0,0,0),
// FL_ONGROUND set, avelocity zeroed. Mirrors the C upstream's
// "stop if on ground" branch with movetype != BOUNCE (always true
// for Toss).
func TestPhysicsToss_HorizontalFloorLatchesGround(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	// Start 5 units above the floor, falling fast. Down-trace will
	// hit the floor at z=0 with normal=(0,0,1) -> latch.
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 5}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{0, 0, -1000}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("avelocity", [3]float32{10, 20, 30}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossFloorWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotV, _ := ev.ReadVec3("velocity")
	if gotV != ([3]float32{0, 0, 0}) {
		t.Errorf("velocity: got %v want (0,0,0)", gotV)
	}
	gotF, _ := ev.ReadFloat("flags")
	if (server.EntityFlag(int32(gotF)) & server.FlagOnGround) == 0 {
		t.Errorf("FL_ONGROUND not set: flags=%v", gotF)
	}
	gotAv, _ := ev.ReadVec3("avelocity")
	if gotAv != ([3]float32{0, 0, 0}) {
		t.Errorf("avelocity: got %v want (0,0,0)", gotAv)
	}
}

// PhysicsToss impacting a vertical wall (normal[2]=0): ClipVelocity
// reflects horizontally (no z component to bounce), FL_ONGROUND is
// NOT set (the normal[2] > 0.7 guard fails), and the entity slides
// along the wall.
func TestPhysicsToss_VerticalWallReflectsNoLatch(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	// Trace +x into the wall at x=0.
	if err := ev.WriteVec3("origin", [3]float32{-5, 0, 50}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{1000, 0, 0}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossWallWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotV, _ := ev.ReadVec3("velocity")
	// X is reflected (overbounce=1.0 on a vertical wall absorbs the
	// inbound x velocity component). ClipVelocity for overbounce=1.0
	// against normal=(1,0,0): out = v - 1.0 * dot(v,n) * n -- the
	// projection along the wall normal is removed entirely.
	if gotV[0] > 0 {
		t.Errorf("velocity[0] should be non-positive after wall clip: got %v", gotV[0])
	}
	// Z has the gravity-driven delta (~ -80) -- not zeroed.
	if gotV[2] >= 0 {
		t.Errorf("velocity[2] should retain gravity acceleration: got %v", gotV[2])
	}
	gotF, _ := ev.ReadFloat("flags")
	if (server.EntityFlag(int32(gotF)) & server.FlagOnGround) != 0 {
		t.Errorf("FL_ONGROUND should NOT be set after wall impact: flags=%v", gotF)
	}
}

// --- PhysicsBounce ---------------------------------------------------------

// PhysicsBounce impacting a horizontal floor at HIGH velocity (>60
// vertical): does NOT latch FL_ONGROUND; instead velocity reflects
// with overbounce 1.5 -- the grenade keeps bouncing. The post-clip
// velocity[2] is POSITIVE (a downward velocity reflected off an
// upward normal flips sign).
func TestPhysicsBounce_HighVelocityKeepsBouncing(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 5}); err != nil {
		t.Fatal(err)
	}
	// -1000 z velocity is way past the 60 unit/s "small" threshold.
	if err := ev.WriteVec3("velocity", [3]float32{0, 0, -1000}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossFloorWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsBounce(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotV, _ := ev.ReadVec3("velocity")
	// Reflection with overbounce 1.5: post-clip z = -v - 1.5 * (-v)
	// = 0.5 * |v|. Original v[2] post-gravity ~ -1080; reflected ~ +540.
	if gotV[2] <= 0 {
		t.Errorf("velocity[2] should be positive after bounce: got %v", gotV[2])
	}
	gotF, _ := ev.ReadFloat("flags")
	if (server.EntityFlag(int32(gotF)) & server.FlagOnGround) != 0 {
		t.Errorf("FL_ONGROUND should NOT latch on high-velocity bounce: flags=%v", gotF)
	}
}

// PhysicsBounce impacting a horizontal floor at LOW velocity (post-
// clip velocity[2] < 60): DOES latch FL_ONGROUND, mirroring the C
// upstream's `if (velocity[2] < 60 || movetype != BOUNCE)` guard.
func TestPhysicsBounce_LowVelocityLatchesGround(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	// Origin just above floor; tiny downward velocity. After gravity
	// adds ~80 of downward, post-clip vz = (1.5-1)*(-v) ~ tiny positive,
	// which is < 60 -> latch. Pick |v_in| = 10 -> v_after_gravity ~ -90;
	// post-clip vz ~ 45 which is < 60 -> latches.
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{0, 0, -10}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossFloorWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsBounce(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	gotF, _ := ev.ReadFloat("flags")
	if (server.EntityFlag(int32(gotF)) & server.FlagOnGround) == 0 {
		t.Errorf("FL_ONGROUND should latch on low-velocity bounce: flags=%v", gotF)
	}
	if gotV, _ := ev.ReadVec3("velocity"); gotV != ([3]float32{0, 0, 0}) {
		t.Errorf("velocity should be zeroed on latch: got %v", gotV)
	}
}

// --- think dispatch --------------------------------------------------------

// PhysicsToss with a ThinkCaller that errors: surface the error,
// alive=false, and the entity state must NOT have been touched
// (the algorithm bails before reading velocity / origin).
func TestPhysicsToss_ThinkCallerError(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 100}); err != nil {
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
		Worldmodel: tossCorruptWorld(),
		Now:        1.0,
		Dt:         0.1,
		ThinkCaller: func(*progs.Edict, int32) error {
			return want
		},
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, want) {
		t.Errorf("alive=%v err=%v want false, %v", alive, err, want)
	}
	if gotO, _ := ev.ReadVec3("origin"); gotO != ([3]float32{0, 0, 100}) {
		t.Errorf("origin mutated after think failure: got %v", gotO)
	}
}

// PhysicsToss with a nil ThinkCaller: RunThink returns ErrNoThinkCaller;
// the (alive=false, err != nil) branch fires.
func TestPhysicsToss_NilThinkCaller(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	ctx := PhysicsContext{
		Worldmodel: tossEmptyWorld(),
		Now:        1.0,
		Dt:         0.1,
		// ThinkCaller: nil -- exercises RunThink's ErrNoThinkCaller path.
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, server.ErrNoThinkCaller) {
		t.Errorf("alive=%v err=%v want false, ErrNoThinkCaller", alive, err)
	}
}

// --- !alive (false, nil) branch --------------------------------------------
//
// PhysicsToss DROPS the !alive guard bsptrace-style: RunThink can
// only return (false, nil) via the nil-arg sentinels, all of which
// also set err != nil; the C upstream's "ent->free after think" bail
// has no analogue on the current Edict surface. The alive bool is
// discarded; only err drives early-return. No test is needed for the
// (false, nil) path because the path is structurally unreachable.

// --- missing-field paths ---------------------------------------------------

// Missing flags field.
func TestPhysicsToss_MissingFlags(t *testing.T) {
	p := tossDropField("flags")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// Missing velocity field.
func TestPhysicsToss_MissingVelocity(t *testing.T) {
	p := tossDropField("velocity")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// Missing origin field.
func TestPhysicsToss_MissingOrigin(t *testing.T) {
	p := tossDropField("origin")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// Missing mins field.
func TestPhysicsToss_MissingMins(t *testing.T) {
	p := tossDropField("mins")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// Missing maxs field.
func TestPhysicsToss_MissingMaxs(t *testing.T) {
	p := tossDropField("maxs")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// Missing angles field.
func TestPhysicsToss_MissingAngles(t *testing.T) {
	p := tossDropField("angles")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// Missing avelocity field.
func TestPhysicsToss_MissingAvelocity(t *testing.T) {
	p := tossDropField("avelocity")
	ent, ev := newTossEntFromProgs(t, p)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

// gravity field present but with the WRONG QC type (EvVector instead
// of EvFloat): readGravityFactor surfaces ErrFieldTypeMismatch
// verbatim -- this is the "corrupt-progs" branch that the
// absent-field path does NOT cover.
func TestPhysicsToss_GravityFieldWrongType(t *testing.T) {
	p := tossReplaceFieldType("gravity", progs.EvVector)
	ent, ev := newTossEntFromProgs(t, p)
	seedTossBaseline(t, ev)
	ctx := PhysicsContext{
		Worldmodel:  tossEmptyWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || !errors.Is(err, progs.ErrFieldTypeMismatch) {
		t.Errorf("alive=%v err=%v want false, ErrFieldTypeMismatch", alive, err)
	}
}

// --- PushEntity error path -------------------------------------------------

// PhysicsToss with a corrupt worldmodel: PushEntity errors out;
// PhysicsToss surfaces it without writing origin / velocity.
func TestPhysicsToss_PushEntityError(t *testing.T) {
	ent, ev, _ := newTossEnt(t, true)
	seedTossBaseline(t, ev)
	if err := ev.WriteVec3("origin", [3]float32{0, 0, 100}); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteVec3("velocity", [3]float32{10, 0, 0}); err != nil {
		t.Fatal(err)
	}
	ctx := PhysicsContext{
		Worldmodel:  tossCorruptWorld(),
		Now:         1.0,
		Dt:          0.1,
		ThinkCaller: tossNoThink(t),
	}
	alive, err := PhysicsToss(ent, ev, 0, server.DefaultPhysParams(), ctx)
	if alive || err == nil {
		t.Errorf("alive=%v err=%v want false + non-nil", alive, err)
	}
	if gotO, _ := ev.ReadVec3("origin"); gotO != ([3]float32{0, 0, 100}) {
		t.Errorf("origin mutated after PushEntity error: got %v", gotO)
	}
}

// --- drift detectors -------------------------------------------------------

// The Toss/Bounce stop thresholds drift if someone "fixes" them
// without re-reading the C upstream. Pin the literals.
func TestTossConstants_TyrquakeValues(t *testing.T) {
	if tossOnGroundNormalZ != 0.7 {
		t.Errorf("tossOnGroundNormalZ drift: got %v want 0.7", tossOnGroundNormalZ)
	}
	if tossStopVelocityZ != 60 {
		t.Errorf("tossStopVelocityZ drift: got %v want 60", tossStopVelocityZ)
	}
	if tossDefaultGravity != 1.0 {
		t.Errorf("tossDefaultGravity drift: got %v want 1.0", tossDefaultGravity)
	}
}
