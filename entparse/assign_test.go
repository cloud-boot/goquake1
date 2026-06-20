// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package entparse

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-quake1/engine/progs"
)

// progsForAssign builds a stub Progs with one field of every Etype
// AssignFields cares about plus the "angles" vector used by the
// "angle" QuakeEd-shortcut path and a "light_lev" float used by the
// "light" alias. The interner toggles WriteString round-tripping.
//
// Field layout (4-byte slots inside a 32-slot EntityFields block):
//
//	ofs   1  EvFloat    health
//	ofs   2  EvVector   angles   (consumes 2,3,4)
//	ofs   5  EvVector   origin   (consumes 5,6,7)
//	ofs   8  EvEntity   enemy
//	ofs   9  EvField    chain
//	ofs  10  EvFunction think
//	ofs  11  EvString   classname
//	ofs  12  EvString   model
//	ofs  13  EvVoid     noisedata
//	ofs  14  EvFloat    light_lev
//	ofs  15  EvFloat    badfloat   (badly-placed -- write triggers ErrFieldOffset)
//	ofs  16  EvVector   badvec     (badly-placed)
//	ofs  17  EvEntity   badent     (badly-placed)
//	ofs  18  EvField    badfield   (badly-placed)
//	ofs  19  EvFunction badfunc    (badly-placed)
//	ofs  20  EvString   badstring  (badly-placed)
//	ofs  21  EvPointer  ptrfield   (default branch of the type switch)
//
// We also stash one EvField TARGET ("chaintarget") + one Function
// ("think") so the EvField + EvFunction branches have something to
// resolve.
func progsForAssign(entityFieldSlots int32) *progs.Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	healthName := add("health")
	anglesName := add("angles")
	originName := add("origin")
	enemyName := add("enemy")
	chainName := add("chain")
	thinkName := add("think")
	classnameName := add("classname")
	modelName := add("model")
	noiseName := add("noisedata")
	lightLevName := add("light_lev")
	badFloat := add("badfloat")
	badVec := add("badvec")
	badEnt := add("badent")
	badField := add("badfield")
	badFunc := add("badfunc")
	badString := add("badstring")
	ptrName := add("ptrfield")
	chainTarget := add("chaintarget") // resolvable ev_field target

	p := &progs.Progs{
		Header:  progs.Header{EntityFields: entityFieldSlots},
		Strings: strs,
		FieldDefs: []progs.Def{
			{Type: uint16(progs.EvFloat), Ofs: 1, SName: healthName},
			{Type: uint16(progs.EvVector), Ofs: 2, SName: anglesName},
			{Type: uint16(progs.EvVector), Ofs: 5, SName: originName},
			{Type: uint16(progs.EvEntity), Ofs: 8, SName: enemyName},
			{Type: uint16(progs.EvField), Ofs: 9, SName: chainName},
			{Type: uint16(progs.EvFunction), Ofs: 10, SName: thinkName},
			{Type: uint16(progs.EvString), Ofs: 11, SName: classnameName},
			{Type: uint16(progs.EvString), Ofs: 12, SName: modelName},
			{Type: uint16(progs.EvVoid), Ofs: 13, SName: noiseName},
			{Type: uint16(progs.EvFloat), Ofs: 14, SName: lightLevName},
			// The "bad*" defs sit at slot offsets past the configured
			// EntityFields block size so any Write* against them fires
			// progs.ErrFieldOffset.
			{Type: uint16(progs.EvFloat), Ofs: 9999, SName: badFloat},
			{Type: uint16(progs.EvVector), Ofs: 9999, SName: badVec},
			{Type: uint16(progs.EvEntity), Ofs: 9999, SName: badEnt},
			{Type: uint16(progs.EvField), Ofs: 9999, SName: badField},
			{Type: uint16(progs.EvFunction), Ofs: 9999, SName: badFunc},
			{Type: uint16(progs.EvString), Ofs: 9999, SName: badString},
			{Type: uint16(progs.EvPointer), Ofs: 1, SName: ptrName},
			// EvField resolution target.
			{Type: uint16(progs.EvFloat), Ofs: 7, SName: chainTarget},
		},
		Functions: []progs.Function{
			{SName: thinkName}, // index 0 -- BUT FindFunction returns
			// 0-based index... we want a non-zero index for
			// "think" so the WriteInt32 has something distinguishable;
			// add a sentinel function at index 0 instead.
		},
	}
	// Insert a sentinel function at index 0 so "think" resolves to index 1.
	sentinel := add("__sentinel")
	p.Strings = strs
	p.Functions = []progs.Function{
		{SName: sentinel},
		{SName: thinkName},
	}
	return p
}

// allocEdict returns a fresh edict from a 2-slot arena (slot 1).
func allocEdict(t *testing.T, p *progs.Progs) *progs.Edict {
	t.Helper()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	return e
}

// stringInterner returns an interner that appends to p.Strings and
// returns the offset where the bytes landed. Sufficient for the
// EvString round-trip tests.
func stringInterner(p *progs.Progs) progs.StringInterner {
	return func(s string) int32 {
		ofs := int32(len(p.Strings))
		p.Strings = append(p.Strings, []byte(s)...)
		p.Strings = append(p.Strings, 0)
		return ofs
	}
}

// --- AssignFields happy paths -----------------------------------------------

func TestAssignFields_Float(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"health": "75"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, err := v.ReadFloat("health")
	if err != nil || got != 75 {
		t.Errorf("health = %v err=%v want 75", got, err)
	}
}

func TestAssignFields_Vec3(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"origin": "10 20 30"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, err := v.ReadVec3("origin")
	if err != nil || got != ([3]float32{10, 20, 30}) {
		t.Errorf("origin = %v err=%v want {10,20,30}", got, err)
	}
}

func TestAssignFields_Entity(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"enemy": "13"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, _ := v.ReadInt32("enemy")
	if got != 13 {
		t.Errorf("enemy = %v want 13", got)
	}
}

func TestAssignFields_EvField_Resolved(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	// "chaintarget" is an EvFloat at Ofs=7 in the stub; the EvField
	// branch should store that offset (7) into chain.
	if err := AssignFields(EntityFields{"chain": "chaintarget"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, _ := v.ReadInt32("chain")
	if got != 7 {
		t.Errorf("chain = %v want 7 (chaintarget Ofs)", got)
	}
}

func TestAssignFields_EvFunction_Resolved(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"think": "think"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, _ := v.ReadInt32("think")
	if got != 1 {
		t.Errorf("think = %v want 1 (FindFunction index)", got)
	}
}

func TestAssignFields_Classname(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"classname": "monster_zombie"}, p, ent, stringInterner(p)); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	v.Interner = stringInterner(p) // not needed for Read
	got, err := v.ReadString("classname")
	if err != nil || got != "monster_zombie" {
		t.Errorf("classname = %q err=%v want monster_zombie", got, err)
	}
}

func TestAssignFields_Angle_QuakeEdShortcut(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"angle": "90"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, err := v.ReadVec3("angles")
	if err != nil {
		t.Fatal(err)
	}
	want := [3]float32{0, 90, 0}
	if got != want {
		t.Errorf("angles = %v want %v", got, want)
	}
}

func TestAssignFields_Light_QuakeEdAlias(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	if err := AssignFields(EntityFields{"light": "200"}, p, ent, nil); err != nil {
		t.Fatalf("AssignFields: %v", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, err := v.ReadFloat("light_lev")
	if err != nil || got != 200 {
		t.Errorf("light_lev = %v err=%v want 200", got, err)
	}
}

func TestAssignFields_UnknownKey_SilentSkip(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	// "no_such_field" silently skipped; "health" still assigned.
	err := AssignFields(EntityFields{
		"no_such_field": "junk",
		"health":        "42",
	}, p, ent, nil)
	if err != nil {
		t.Fatalf("AssignFields err = %v, want nil", err)
	}
	v, _ := progs.NewEntVars(p, ent)
	got, _ := v.ReadFloat("health")
	if got != 42 {
		t.Errorf("health = %v want 42", got)
	}
}

func TestAssignFields_EvVoid_SilentSkip(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	// EvVoid + EvPointer hit the default branch of the type switch
	// and are silently skipped; no error.
	err := AssignFields(EntityFields{
		"noisedata": "anything",
		"ptrfield":  "anything",
	}, p, ent, nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// --- AssignFields error paths -----------------------------------------------

func TestAssignFields_NilProgs(t *testing.T) {
	err := AssignFields(EntityFields{"x": "y"}, nil, &progs.Edict{}, nil)
	if !errors.Is(err, progs.ErrNilArg) {
		t.Errorf("err = %v want ErrNilArg", err)
	}
}

func TestAssignFields_NilEdict(t *testing.T) {
	p := progsForAssign(32)
	err := AssignFields(EntityFields{"x": "y"}, p, nil, nil)
	if !errors.Is(err, progs.ErrNilArg) {
		t.Errorf("err = %v want ErrNilArg", err)
	}
}

func TestAssignFields_MalformedFloat(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"health": "garbage"}, p, ent, nil)
	var ea *ErrAssign
	if !errors.As(err, &ea) || ea.Key != "health" || !errors.Is(err, ErrBadFloat) {
		t.Errorf("err = %v want ErrAssign{health, ErrBadFloat}", err)
	}
	// ensure the Error() formatting includes the key
	if !strings.Contains(ea.Error(), "health") {
		t.Errorf("Error()=%q missing key", ea.Error())
	}
}

func TestAssignFields_MalformedVec3(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"origin": "1 2 garbage"}, p, ent, nil)
	if !errors.Is(err, ErrBadVec3) {
		t.Errorf("err = %v want ErrBadVec3", err)
	}
}

func TestAssignFields_MalformedEntity(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"enemy": "garbage"}, p, ent, nil)
	if !errors.Is(err, ErrBadEntity) {
		t.Errorf("err = %v want ErrBadEntity", err)
	}
}

func TestAssignFields_MalformedAngle(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"angle": "garbage"}, p, ent, nil)
	if !errors.Is(err, ErrBadFloat) {
		t.Errorf("err = %v want ErrBadFloat", err)
	}
}

func TestAssignFields_EvField_UnknownReference(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"chain": "nothere"}, p, ent, nil)
	if err == nil || !strings.Contains(err.Error(), "ev_field") {
		t.Errorf("err = %v want unknown-field error", err)
	}
}

func TestAssignFields_EvFunction_UnknownReference(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"think": "nothere"}, p, ent, nil)
	if err == nil || !strings.Contains(err.Error(), "ev_function") {
		t.Errorf("err = %v want unknown-function error", err)
	}
}

// --- AssignFields Write* failure paths (Ofs out of range) -------------------

func TestAssignFields_WriteFloatFails(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"badfloat": "1.5"}, p, ent, nil)
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteVec3Fails(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"badvec": "1 2 3"}, p, ent, nil)
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteEntityFails(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"badent": "5"}, p, ent, nil)
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteEvFieldFails(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	// "badfield" is at Ofs=9999 so the WriteInt32 inside the EvField
	// branch trips ErrFieldOffset. Use a resolvable target name
	// ("chaintarget") so we exercise the WriteInt32 failure path, not
	// the "unknown ev_field reference" branch.
	err := AssignFields(EntityFields{"badfield": "chaintarget"}, p, ent, nil)
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteEvFunctionFails(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"badfunc": "think"}, p, ent, nil)
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteStringFails(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"badstring": "foo"}, p, ent, stringInterner(p))
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteAngleHackFails(t *testing.T) {
	// Force the WriteVec3 inside the "angle" QuakeEd shortcut to fail
	// by shrinking the edict's field block below the angles vector's
	// 3-float span. The stub places "angles" at Ofs=2 (slots 2,3,4);
	// a 2-slot edict field block leaves only slots 0..1 valid.
	p := progsForAssign(2)
	ent := allocEdict(t, p)
	err := AssignFields(EntityFields{"angle": "45"}, p, ent, nil)
	if !errors.Is(err, progs.ErrFieldOffset) {
		t.Errorf("err = %v want ErrFieldOffset", err)
	}
}

func TestAssignFields_WriteStringNoInterner(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	// EvString with no interner -- WriteString returns ErrNoInterner.
	err := AssignFields(EntityFields{"model": "progs/zombie.mdl"}, p, ent, nil)
	if !errors.Is(err, progs.ErrNoInterner) {
		t.Errorf("err = %v want ErrNoInterner", err)
	}
}

// --- AssignFields multi-error ordering --------------------------------------

func TestAssignFields_FirstErrorWins_OthersStillProcessed(t *testing.T) {
	p := progsForAssign(32)
	ent := allocEdict(t, p)
	// Two malformed entries + one good one. The good one MUST still
	// be assigned (best-effort semantics); the returned error is
	// SOME ErrAssign (map iteration is unordered, so we don't pin
	// which key fired first).
	err := AssignFields(EntityFields{
		"health":     "75",     // ok
		"enemy":      "broken", // ErrBadEntity
		"badnothere": "junk",   // unknown -- silently skipped
	}, p, ent, nil)
	if err == nil {
		t.Fatal("err = nil, want some error")
	}
	v, _ := progs.NewEntVars(p, ent)
	got, _ := v.ReadFloat("health")
	if got != 75 {
		t.Errorf("health = %v want 75 (best-effort assignment)", got)
	}
}

// --- ErrAssign Unwrap -------------------------------------------------------

func TestErrAssign_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	ea := &ErrAssign{Key: "k", Err: inner}
	if !errors.Is(ea, inner) {
		t.Fatal("errors.Is failed to unwrap")
	}
}

// --- SpawnEntities ----------------------------------------------------------

func TestSpawnEntities_HappyPath(t *testing.T) {
	p := progsForAssign(32)
	a := progs.NewEdictArena(p, 8)
	a.Reset()
	// edictAt allocates a fresh slot per index.
	edictAt := func(i int) *progs.Edict {
		e, _, err := a.Alloc()
		if err != nil {
			t.Fatalf("Alloc(%d): %v", i, err)
		}
		_ = i
		return e
	}
	var seen []string
	spawn := func(ent *progs.Edict, classname string) {
		_ = ent
		seen = append(seen, classname)
	}
	entities := []EntityFields{
		{"classname": "worldspawn"},
		{"classname": "monster_zombie", "health": "60"},
		{"classname": "info_player_start"},
	}
	if err := SpawnEntities(entities, p, edictAt, stringInterner(p), spawn); err != nil {
		t.Fatalf("SpawnEntities: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %v want 3 spawns", seen)
	}
	for i, want := range []string{"worldspawn", "monster_zombie", "info_player_start"} {
		if seen[i] != want {
			t.Errorf("seen[%d] = %q want %q", i, seen[i], want)
		}
	}
}

func TestSpawnEntities_EmptyList(t *testing.T) {
	p := progsForAssign(32)
	called := 0
	spawn := func(*progs.Edict, string) { called++ }
	edictAt := func(int) *progs.Edict {
		t.Fatal("edictAt should not be called on empty list")
		return nil
	}
	if err := SpawnEntities(nil, p, edictAt, nil, spawn); err != nil {
		t.Fatalf("err = %v", err)
	}
	if called != 0 {
		t.Errorf("spawn called %d times, want 0", called)
	}
}

func TestSpawnEntities_NilSpawnHook(t *testing.T) {
	// spawn=nil => assignment runs but no callback invocation.
	p := progsForAssign(32)
	a := progs.NewEdictArena(p, 4)
	a.Reset()
	edictAt := func(int) *progs.Edict {
		e, _, _ := a.Alloc()
		return e
	}
	entities := []EntityFields{
		{"classname": "worldspawn", "health": "1"},
	}
	if err := SpawnEntities(entities, p, edictAt, stringInterner(p), nil); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestSpawnEntities_MissingClassname_StillProcessesRest(t *testing.T) {
	p := progsForAssign(32)
	a := progs.NewEdictArena(p, 8)
	a.Reset()
	edictAt := func(int) *progs.Edict {
		e, _, _ := a.Alloc()
		return e
	}
	var seen []string
	spawn := func(_ *progs.Edict, classname string) {
		seen = append(seen, classname)
	}
	entities := []EntityFields{
		{"health": "1"},                    // no classname -- spawn skipped
		{"classname": "monster_ogre"},      // spawn called
		{"classname": ""},                  // empty classname -- spawn skipped
		{"classname": "info_intermission"}, // spawn called
	}
	if err := SpawnEntities(entities, p, edictAt, stringInterner(p), spawn); err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []string{"monster_ogre", "info_intermission"}
	if len(seen) != len(want) {
		t.Fatalf("seen = %v want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("seen[%d] = %q want %q", i, seen[i], want[i])
		}
	}
}

func TestSpawnEntities_NilProgs(t *testing.T) {
	err := SpawnEntities(nil, nil, func(int) *progs.Edict { return nil }, nil, nil)
	if !errors.Is(err, progs.ErrNilArg) {
		t.Errorf("err = %v want ErrNilArg", err)
	}
}

func TestSpawnEntities_NilEdictAt(t *testing.T) {
	p := progsForAssign(32)
	err := SpawnEntities(nil, p, nil, nil, nil)
	if !errors.Is(err, progs.ErrNilArg) {
		t.Errorf("err = %v want ErrNilArg", err)
	}
}

func TestSpawnEntities_EdictAtReturnsNil(t *testing.T) {
	p := progsForAssign(32)
	edictAt := func(int) *progs.Edict { return nil }
	called := 0
	spawn := func(*progs.Edict, string) { called++ }
	entities := []EntityFields{{"classname": "x"}, {"classname": "y"}}
	err := SpawnEntities(entities, p, edictAt, nil, spawn)
	if err == nil || !strings.Contains(err.Error(), "edictAt") {
		t.Errorf("err = %v want edictAt nil error", err)
	}
	if called != 0 {
		t.Errorf("spawn called %d times, want 0 (edicts all nil)", called)
	}
}

func TestSpawnEntities_AssignErrorPropagates(t *testing.T) {
	// One entity has a malformed value; SpawnEntities surfaces the
	// FIRST AssignFields error but keeps processing later entities.
	p := progsForAssign(32)
	a := progs.NewEdictArena(p, 4)
	a.Reset()
	edictAt := func(int) *progs.Edict {
		e, _, _ := a.Alloc()
		return e
	}
	var seen []string
	spawn := func(_ *progs.Edict, classname string) {
		seen = append(seen, classname)
	}
	entities := []EntityFields{
		{"classname": "worldspawn", "health": "garbage"}, // malformed
		{"classname": "info_player_start"},
	}
	err := SpawnEntities(entities, p, edictAt, stringInterner(p), spawn)
	if !errors.Is(err, ErrBadFloat) {
		t.Errorf("err = %v want ErrBadFloat", err)
	}
	// Both spawn calls still happen.
	if len(seen) != 2 {
		t.Errorf("seen = %v want 2 spawns", seen)
	}
}
