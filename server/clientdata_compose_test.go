// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
)

// playerProgs builds a Progs with every entvars field
// ComposeClientDataFromEdict reads, at unique offsets inside an
// EntityFields=64 block (the vectors view_ofs/punchangle/velocity
// each consume 3 floats; everything else is one float).
func playerProgs() *progs.Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	type field struct {
		name string
		typ  progs.Etype
		ofs  int32
	}
	all := []field{
		{"view_ofs", progs.EvVector, 1},     // 1,2,3
		{"idealpitch", progs.EvFloat, 4},    //
		{"punchangle", progs.EvVector, 5},   // 5,6,7
		{"velocity", progs.EvVector, 8},     // 8,9,10
		{"items", progs.EvFloat, 11},        //
		{"flags", progs.EvFloat, 12},        //
		{"waterlevel", progs.EvFloat, 13},   //
		{"weaponframe", progs.EvFloat, 14},  //
		{"armorvalue", progs.EvFloat, 15},   //
		{"health", progs.EvFloat, 16},       //
		{"currentammo", progs.EvFloat, 17},  //
		{"ammo_shells", progs.EvFloat, 18},  //
		{"ammo_nails", progs.EvFloat, 19},   //
		{"ammo_rockets", progs.EvFloat, 20}, //
		{"ammo_cells", progs.EvFloat, 21},   //
		{"weapon", progs.EvFloat, 22},       //
	}
	defs := make([]progs.Def, 0, len(all))
	for _, f := range all {
		defs = append(defs, progs.Def{Type: uint16(f.typ), Ofs: uint16(f.ofs), SName: add(f.name)})
	}
	return &progs.Progs{
		Header:    progs.Header{EntityFields: 64},
		Strings:   strs,
		FieldDefs: defs,
	}
}

// allocPlayerEdict reserves one edict from a fresh arena bound to p.
func allocPlayerEdict(t *testing.T, p *progs.Progs) *progs.Edict {
	t.Helper()
	a := progs.NewEdictArena(p, 2)
	a.Reset()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	return e
}

// nil progs -> the helper returns the zero-default state with
// ViewHeightOffset = DefaultViewHeight so the encoder won't emit
// SU_VIEWHEIGHT.
func TestComposeClientDataFromEdict_NilProgs(t *testing.T) {
	s := ComposeClientDataFromEdict(nil, nil)
	if s.ViewHeightOffset != protocol.DefaultViewHeight {
		t.Errorf("ViewHeightOffset: got %v want %v",
			s.ViewHeightOffset, protocol.DefaultViewHeight)
	}
	if s.Health != 0 || s.Items != 0 {
		t.Errorf("non-zero default: %+v", s)
	}
}

// nil edict (with non-nil progs) -> same zero-default state.
func TestComposeClientDataFromEdict_NilEdict(t *testing.T) {
	p := playerProgs()
	s := ComposeClientDataFromEdict(p, nil)
	if s.ViewHeightOffset != protocol.DefaultViewHeight {
		t.Errorf("ViewHeightOffset: got %v want %v",
			s.ViewHeightOffset, protocol.DefaultViewHeight)
	}
}

// Stripped progs (no fields declared) -> every read returns
// ErrFieldNotFound + the compose helper substitutes the zero
// default. ViewHeightOffset stays at DefaultViewHeight.
func TestComposeClientDataFromEdict_StrippedProgs(t *testing.T) {
	stripped := &progs.Progs{Header: progs.Header{EntityFields: 16}, Strings: []byte{0}}
	e := allocPlayerEdict(t, stripped)
	s := ComposeClientDataFromEdict(stripped, e)
	if s.ViewHeightOffset != protocol.DefaultViewHeight {
		t.Errorf("ViewHeightOffset: got %v want %v",
			s.ViewHeightOffset, protocol.DefaultViewHeight)
	}
	if s.Velocity != [3]float32{} {
		t.Errorf("Velocity: got %v want zero", s.Velocity)
	}
}

// Full populated edict -> every field flows through.
func TestComposeClientDataFromEdict_FullSnapshot(t *testing.T) {
	p := playerProgs()
	e := allocPlayerEdict(t, p)
	v, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	mustWriteVec3(t, v, "view_ofs", [3]float32{0, 0, 28})
	mustWriteFloat(t, v, "idealpitch", -5)
	mustWriteVec3(t, v, "punchangle", [3]float32{2, 0, -1})
	mustWriteVec3(t, v, "velocity", [3]float32{160, -80, 32})
	mustWriteFloat(t, v, "items", 4321)
	mustWriteFloat(t, v, "flags", 512) // FlagOnGround
	mustWriteFloat(t, v, "waterlevel", 2)
	mustWriteFloat(t, v, "weaponframe", 7)
	mustWriteFloat(t, v, "armorvalue", 100)
	mustWriteFloat(t, v, "health", 87)
	mustWriteFloat(t, v, "currentammo", 23)
	mustWriteFloat(t, v, "ammo_shells", 50)
	mustWriteFloat(t, v, "ammo_nails", 75)
	mustWriteFloat(t, v, "ammo_rockets", 12)
	mustWriteFloat(t, v, "ammo_cells", 6)
	mustWriteFloat(t, v, "weapon", 8)

	s := ComposeClientDataFromEdict(p, e)
	if s.ViewHeightOffset != 28 {
		t.Errorf("ViewHeightOffset: got %v want 28", s.ViewHeightOffset)
	}
	if s.IdealPitch != -5 {
		t.Errorf("IdealPitch: got %v want -5", s.IdealPitch)
	}
	if s.PunchAngle != [3]float32{2, 0, -1} {
		t.Errorf("PunchAngle: got %v want [2 0 -1]", s.PunchAngle)
	}
	if s.Velocity != [3]float32{160, -80, 32} {
		t.Errorf("Velocity: got %v want [160 -80 32]", s.Velocity)
	}
	if s.Items != 4321 {
		t.Errorf("Items: got %v want 4321", s.Items)
	}
	if !s.OnGround {
		t.Error("OnGround: got false want true (flags = 512)")
	}
	if !s.InWater {
		t.Error("InWater: got false want true (waterlevel >= 2)")
	}
	if s.WeaponFrame != 7 || s.ArmorValue != 100 || s.Health != 87 ||
		s.CurrentAmmo != 23 || s.ActiveWeapon != 8 {
		t.Errorf("scalar fields: %+v", s)
	}
	if s.Ammo != [4]int{50, 75, 12, 6} {
		t.Errorf("Ammo: got %v want [50 75 12 6]", s.Ammo)
	}
}

// flags with the FL_ONGROUND bit cleared -> OnGround stays false.
// Mirrors the gating in the compose helper (set only when the bit
// is set, not on any non-zero flags value).
func TestComposeClientDataFromEdict_OnGroundBitClear(t *testing.T) {
	p := playerProgs()
	e := allocPlayerEdict(t, p)
	v, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	mustWriteFloat(t, v, "flags", 1) // FL_FLY, NOT FL_ONGROUND
	s := ComposeClientDataFromEdict(p, e)
	if s.OnGround {
		t.Errorf("OnGround: got true want false (flags=1, no ONGROUND bit)")
	}
}

// waterlevel < 2 -> InWater stays false.
func TestComposeClientDataFromEdict_WaterLevelBelowSubmerged(t *testing.T) {
	p := playerProgs()
	e := allocPlayerEdict(t, p)
	v, err := progs.NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	mustWriteFloat(t, v, "waterlevel", 1) // wading, not submerged
	s := ComposeClientDataFromEdict(p, e)
	if s.InWater {
		t.Errorf("InWater: got true want false (waterlevel=1)")
	}
}

// --- helpers ----------------------------------------------------------------

func mustWriteFloat(t *testing.T, v *progs.EntVars, name string, val float32) {
	t.Helper()
	if err := v.WriteFloat(name, val); err != nil {
		t.Fatalf("WriteFloat(%q, %v): %v", name, val, err)
	}
}

func mustWriteVec3(t *testing.T, v *progs.EntVars, name string, val [3]float32) {
	t.Helper()
	if err := v.WriteVec3(name, val); err != nil {
		t.Fatalf("WriteVec3(%q, %v): %v", name, val, err)
	}
}
