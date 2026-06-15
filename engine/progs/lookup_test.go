// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import "testing"

// progsForLookup returns a stub Progs with a string table + def
// tables that exercise every Find/AtOfs path below.
func progsForLookup() *Progs {
	// Strings: offsets matter -- record them.
	// 0: ""        (the convention "first string is null")
	// 1: "origin"
	// 8: "health"
	// 15: "monster_zombie"
	// 30: "world"
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	originName := add("origin")
	healthName := add("health")
	monsterName := add("monster_zombie")
	worldName := add("world")
	return &Progs{
		Strings: strs,
		FieldDefs: []Def{
			{Type: uint16(EvVector), Ofs: 4, SName: originName},
			{Type: uint16(EvFloat), Ofs: 7, SName: healthName},
		},
		GlobalDefs: []Def{
			{Type: uint16(EvString), Ofs: 1, SName: worldName},
		},
		Functions: []Function{
			{FirstStatement: 1, SName: monsterName},
		},
	}
}

func TestFindField_Hit(t *testing.T) {
	p := progsForLookup()
	d := p.FindField("origin")
	if d == nil || d.Type != uint16(EvVector) {
		t.Fatalf("FindField origin: %+v", d)
	}
}

func TestFindField_Miss(t *testing.T) {
	p := progsForLookup()
	if d := p.FindField("nope"); d != nil {
		t.Errorf("FindField nope: %+v", d)
	}
}

func TestFindGlobal_Hit(t *testing.T) {
	p := progsForLookup()
	d := p.FindGlobal("world")
	if d == nil || d.Ofs != 1 {
		t.Fatalf("FindGlobal world: %+v", d)
	}
}

func TestFindGlobal_Miss(t *testing.T) {
	p := progsForLookup()
	if d := p.FindGlobal("nope"); d != nil {
		t.Errorf("FindGlobal nope: %+v", d)
	}
}

func TestFindFunction_Hit(t *testing.T) {
	p := progsForLookup()
	f, idx := p.FindFunction("monster_zombie")
	if f == nil || idx != 0 {
		t.Fatalf("FindFunction monster_zombie: f=%+v idx=%d", f, idx)
	}
}

func TestFindFunction_Miss(t *testing.T) {
	p := progsForLookup()
	if f, idx := p.FindFunction("nope"); f != nil || idx != -1 {
		t.Errorf("FindFunction nope: f=%+v idx=%d", f, idx)
	}
}

func TestFieldAtOfs(t *testing.T) {
	p := progsForLookup()
	if d := p.FieldAtOfs(4); d == nil || p.String(d.SName) != "origin" {
		t.Errorf("FieldAtOfs(4): %+v", d)
	}
	if d := p.FieldAtOfs(99); d != nil {
		t.Errorf("FieldAtOfs(99): %+v", d)
	}
}

func TestGlobalAtOfs(t *testing.T) {
	p := progsForLookup()
	if d := p.GlobalAtOfs(1); d == nil || p.String(d.SName) != "world" {
		t.Errorf("GlobalAtOfs(1): %+v", d)
	}
	if d := p.GlobalAtOfs(99); d != nil {
		t.Errorf("GlobalAtOfs(99): %+v", d)
	}
}
