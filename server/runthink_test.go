// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/progs"
)

// progsForRunThink builds a Progs stub with the two fields
// SV_RunThink reaches for: nextthink (EvFloat) and think
// (EvFunction). EntityFields=8 -> 32-byte field block, more than
// enough for two fields at ofs 1 and 2.
func progsForRunThink() *progs.Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	nextthinkName := add("nextthink")
	thinkName := add("think")
	return &progs.Progs{
		Header:  progs.Header{EntityFields: 8},
		Strings: strs,
		FieldDefs: []progs.Def{
			{Type: uint16(progs.EvFloat), Ofs: 1, SName: nextthinkName},
			{Type: uint16(progs.EvFunction), Ofs: 2, SName: thinkName},
		},
	}
}

// newRunThinkEnt allocates an Edict on a fresh arena bound to the
// progsForRunThink stub and hands back the matching EntVars.
func newRunThinkEnt(t *testing.T) (*progs.Edict, *progs.EntVars) {
	t.Helper()
	p := progsForRunThink()
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

// --- nil-argument guards ----------------------------------------------------

func TestRunThink_NilEntity(t *testing.T) {
	_, ev := newRunThinkEnt(t)
	alive, err := RunThink(nil, ev, 0, 0.1, func(*progs.Edict, int32) error { return nil })
	if alive || !errors.Is(err, ErrNilEntity) {
		t.Errorf("alive=%v err=%v want false, ErrNilEntity", alive, err)
	}
}

func TestRunThink_NilEntVars(t *testing.T) {
	ent, _ := newRunThinkEnt(t)
	alive, err := RunThink(ent, nil, 0, 0.1, func(*progs.Edict, int32) error { return nil })
	if alive || !errors.Is(err, ErrNilEntVars) {
		t.Errorf("alive=%v err=%v want false, ErrNilEntVars", alive, err)
	}
}

func TestRunThink_NilThinkCaller(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	alive, err := RunThink(ent, ev, 0, 0.1, nil)
	if alive || !errors.Is(err, ErrNoThinkCaller) {
		t.Errorf("alive=%v err=%v want false, ErrNoThinkCaller", alive, err)
	}
}

// --- skip paths -------------------------------------------------------------

func TestRunThink_SkipsWhenNextthinkZero(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	// Field block is zeroed by Alloc -> nextthink starts at 0.
	called := false
	alive, err := RunThink(ent, ev, 1.0, 0.1, func(*progs.Edict, int32) error {
		called = true
		return nil
	})
	if !alive || err != nil {
		t.Errorf("alive=%v err=%v want true, nil", alive, err)
	}
	if called {
		t.Error("thinkCaller invoked despite nextthink=0")
	}
}

func TestRunThink_SkipsWhenNextthinkNegative(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	if err := ev.WriteFloat("nextthink", -5); err != nil {
		t.Fatal(err)
	}
	called := false
	alive, err := RunThink(ent, ev, 1.0, 0.1, func(*progs.Edict, int32) error {
		called = true
		return nil
	})
	if !alive || err != nil || called {
		t.Errorf("alive=%v err=%v called=%v want true, nil, false", alive, err, called)
	}
}

func TestRunThink_SkipsWhenNextthinkInFuture(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	// now=1.0, dt=0.1 -> deadline 1.1. Nextthink at 2.0 is beyond.
	if err := ev.WriteFloat("nextthink", 2.0); err != nil {
		t.Fatal(err)
	}
	called := false
	alive, err := RunThink(ent, ev, 1.0, 0.1, func(*progs.Edict, int32) error {
		called = true
		return nil
	})
	if !alive || err != nil || called {
		t.Errorf("alive=%v err=%v called=%v want true, nil, false", alive, err, called)
	}
	// nextthink must remain untouched on the skip path.
	got, _ := ev.ReadFloat("nextthink")
	if got != 2.0 {
		t.Errorf("nextthink mutated on skip: got %v want 2.0", got)
	}
}

// --- happy path -------------------------------------------------------------

func TestRunThink_FiresWhenNextthinkWithinTick(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	// now=1.0, dt=0.1, nextthink at 1.05 -> in (0, 1.1], fires.
	if err := ev.WriteFloat("nextthink", 1.05); err != nil {
		t.Fatal(err)
	}
	const wantFunc int32 = 42
	if err := ev.WriteInt32("think", wantFunc); err != nil {
		t.Fatal(err)
	}

	var gotEnt *progs.Edict
	var gotFunc int32
	alive, err := RunThink(ent, ev, 1.0, 0.1, func(e *progs.Edict, fid int32) error {
		gotEnt = e
		gotFunc = fid
		return nil
	})
	if !alive || err != nil {
		t.Fatalf("alive=%v err=%v want true, nil", alive, err)
	}
	if gotEnt != ent {
		t.Errorf("thinkCaller got ent=%p want %p", gotEnt, ent)
	}
	if gotFunc != wantFunc {
		t.Errorf("thinkCaller got funcID=%d want %d", gotFunc, wantFunc)
	}
	// nextthink must be reset to 0 after firing.
	if got, _ := ev.ReadFloat("nextthink"); got != 0 {
		t.Errorf("nextthink not cleared: got %v want 0", got)
	}
}

func TestRunThink_FiresAtExactDeadline(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	// Boundary: nextthink == now + dt is INCLUSIVE per `> now+dt`.
	if err := ev.WriteFloat("nextthink", 1.1); err != nil {
		t.Fatal(err)
	}
	called := false
	alive, err := RunThink(ent, ev, 1.0, 0.1, func(*progs.Edict, int32) error {
		called = true
		return nil
	})
	if !alive || err != nil || !called {
		t.Errorf("alive=%v err=%v called=%v want true, nil, true", alive, err, called)
	}
}

// --- error cascades ---------------------------------------------------------

func TestRunThink_ThinkCallerErrorCascades(t *testing.T) {
	ent, ev := newRunThinkEnt(t)
	if err := ev.WriteFloat("nextthink", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ev.WriteInt32("think", 1); err != nil {
		t.Fatal(err)
	}
	want := errors.New("boom")
	alive, err := RunThink(ent, ev, 1.0, 0.1, func(*progs.Edict, int32) error {
		return want
	})
	if alive || !errors.Is(err, want) {
		t.Errorf("alive=%v err=%v want false, %v", alive, err, want)
	}
	// nextthink WAS cleared before the dispatch attempt -- matches
	// the C upstream's ent->v.nextthink = 0 -> PR_ExecuteProgram
	// ordering.
	if got, _ := ev.ReadFloat("nextthink"); got != 0 {
		t.Errorf("nextthink not cleared on dispatch failure: got %v", got)
	}
}

// --- progs-stub-missing-field paths -----------------------------------------
//
// Progs without "nextthink" / "think" exercise the ReadFloat /
// ReadInt32 error returns. Real Q1 progs.dat always carries them;
// these tests pin the defensive behaviour.

func progsWithoutFields(omit string) *progs.Progs {
	p := progsForRunThink()
	kept := p.FieldDefs[:0]
	for _, d := range p.FieldDefs {
		name := readFieldName(p, d.SName)
		if name == omit {
			continue
		}
		kept = append(kept, d)
	}
	p.FieldDefs = kept
	return p
}

func readFieldName(p *progs.Progs, ofs int32) string {
	if ofs < 0 || int(ofs) >= len(p.Strings) {
		return ""
	}
	end := int(ofs)
	for end < len(p.Strings) && p.Strings[end] != 0 {
		end++
	}
	return string(p.Strings[ofs:end])
}

func TestRunThink_MissingNextthinkField(t *testing.T) {
	p := progsWithoutFields("nextthink")
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	v, _ := progs.NewEntVars(p, e)
	alive, err := RunThink(e, v, 1.0, 0.1, func(*progs.Edict, int32) error { return nil })
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}

func TestRunThink_MissingThinkField(t *testing.T) {
	p := progsWithoutFields("think")
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	v, _ := progs.NewEntVars(p, e)
	// nextthink must be in-range so RunThink reaches the think
	// lookup.
	if err := v.WriteFloat("nextthink", 0.5); err != nil {
		t.Fatal(err)
	}
	alive, err := RunThink(e, v, 1.0, 0.1, func(*progs.Edict, int32) error { return nil })
	if alive || !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("alive=%v err=%v want false, ErrFieldNotFound", alive, err)
	}
}
