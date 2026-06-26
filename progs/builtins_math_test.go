// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"errors"
	"math"
	"testing"
)

// Each builtin is a (*VM) error -- exercise them by:
//  1. seeding OfsParm0..OfsParm0+2 with the input
//  2. calling the builtin
//  3. reading OfsReturn / OfsReturn+2 for the result

func builtinVM() *VM { return NewVM(progsForVM(nil)) }

// --- NORMALIZE -------------------------------------------------------------

func TestBuiltin_Normalize_Unit(t *testing.T) {
	vm := builtinVM()
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{3, 4, 0})
	if err := BuiltinFnNormalize(vm); err != nil {
		t.Fatal(err)
	}
	got, _ := vm.GlobalVector(OfsReturn)
	want := [3]float32{0.6, 0.8, 0}
	if got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestBuiltin_Normalize_ZeroVector(t *testing.T) {
	vm := builtinVM()
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{0, 0, 0})
	if err := BuiltinFnNormalize(vm); err != nil {
		t.Fatal(err)
	}
	got, _ := vm.GlobalVector(OfsReturn)
	if got != [3]float32{0, 0, 0} {
		t.Errorf("zero input: got %v", got)
	}
}

// --- VLEN ------------------------------------------------------------------

func TestBuiltin_VLen(t *testing.T) {
	vm := builtinVM()
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{3, 4, 0})
	if err := BuiltinFnVLen(vm); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(OfsReturn); v != 5 {
		t.Errorf("VLen 3-4-5: got %v want 5", v)
	}
}

func TestBuiltin_VLen_Zero(t *testing.T) {
	vm := builtinVM()
	if err := BuiltinFnVLen(vm); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(OfsReturn); v != 0 {
		t.Errorf("VLen zero: %v", v)
	}
}

// --- VECTOYAW --------------------------------------------------------------

func TestBuiltin_VecToYaw(t *testing.T) {
	cases := []struct {
		in   [3]float32
		want float32
	}{
		{[3]float32{1, 0, 0}, 0},  // east
		{[3]float32{0, 1, 0}, 90}, // north
		{[3]float32{-1, 0, 0}, 180},
		{[3]float32{0, -1, 0}, 270},
		{[3]float32{0, 0, 0}, 0}, // null
		{[3]float32{0, 0, 5}, 0}, // z-only -> yaw 0
	}
	for _, c := range cases {
		vm := builtinVM()
		_ = vm.SetGlobalVector(OfsParm0, c.in)
		if err := BuiltinFnVecToYaw(vm); err != nil {
			t.Fatal(err)
		}
		got, _ := vm.GlobalFloat(OfsReturn)
		if got != c.want {
			t.Errorf("VecToYaw(%v): got %v want %v", c.in, got, c.want)
		}
	}
}

// --- VECTOANGLES -----------------------------------------------------------

func TestBuiltin_VecToAngles(t *testing.T) {
	cases := []struct {
		in   [3]float32
		want [3]float32 // pitch, yaw, 0
	}{
		{[3]float32{1, 0, 0}, [3]float32{0, 0, 0}},    // forward
		{[3]float32{0, 1, 0}, [3]float32{0, 90, 0}},   // left
		{[3]float32{0, 0, 1}, [3]float32{90, 0, 0}},   // up
		{[3]float32{0, 0, -1}, [3]float32{270, 0, 0}}, // down
	}
	for _, c := range cases {
		vm := builtinVM()
		_ = vm.SetGlobalVector(OfsParm0, c.in)
		if err := BuiltinFnVecToAngles(vm); err != nil {
			t.Fatal(err)
		}
		got, _ := vm.GlobalVector(OfsReturn)
		if got != c.want {
			t.Errorf("VecToAngles(%v): got %v want %v", c.in, got, c.want)
		}
	}
}

// Covers the yaw<0 + pitch<0 wrap branches of VecToAngles -- they
// fire only on (x>0 || y<0) AND (z<0) inside the else arm.
// atan2(-1, 1) = -45 -> yaw wraps to 315; atan2(-1, sqrt(2)) ~ -35
// -> pitch wraps to 325.
func TestBuiltin_VecToAngles_NegativeWraps(t *testing.T) {
	vm := builtinVM()
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{1, -1, -1})
	if err := BuiltinFnVecToAngles(vm); err != nil {
		t.Fatal(err)
	}
	got, _ := vm.GlobalVector(OfsReturn)
	if got[1] != 315 {
		t.Errorf("yaw wrap: got %v want 315", got[1])
	}
	// Pitch is the integer-cast result of atan2 + 360 wrap; ~325.
	if got[0] < 320 || got[0] > 330 {
		t.Errorf("pitch wrap: got %v want ~325", got[0])
	}
}

// --- RANDOM ----------------------------------------------------------------

func TestBuiltin_Random_NoSourceErrors(t *testing.T) {
	vm := builtinVM()
	if err := BuiltinFnRandom(vm); !errors.Is(err, ErrRandomNotSeeded) {
		t.Errorf("got %v want ErrRandomNotSeeded", err)
	}
}

func TestBuiltin_Random_UsesSource(t *testing.T) {
	vm := builtinVM()
	vm.SetRandomSource(func() float32 { return 0.42 })
	if err := BuiltinFnRandom(vm); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(OfsReturn); v != 0.42 {
		t.Errorf("random: got %v want 0.42", v)
	}
}

// --- FABS / RINT / FLOOR / CEIL -------------------------------------------

func TestBuiltin_FAbs(t *testing.T) {
	cases := map[float32]float32{-1.5: 1.5, 0: 0, 2.5: 2.5}
	for in, want := range cases {
		vm := builtinVM()
		_ = vm.SetGlobalFloat(OfsParm0, in)
		if err := BuiltinFnFAbs(vm); err != nil {
			t.Fatal(err)
		}
		if v, _ := vm.GlobalFloat(OfsReturn); v != want {
			t.Errorf("FAbs(%v): got %v want %v", in, v, want)
		}
	}
}

func TestBuiltin_RInt(t *testing.T) {
	// tyrquake half-away-from-zero rounding (NOT bankers').
	cases := map[float32]float32{
		1.4: 1, 1.5: 2, 1.6: 2,
		-1.4: -1, -1.5: -2, -1.6: -2,
		0: 0, 2.5: 3, -2.5: -3,
	}
	for in, want := range cases {
		vm := builtinVM()
		_ = vm.SetGlobalFloat(OfsParm0, in)
		if err := BuiltinFnRInt(vm); err != nil {
			t.Fatal(err)
		}
		if v, _ := vm.GlobalFloat(OfsReturn); v != want {
			t.Errorf("RInt(%v): got %v want %v", in, v, want)
		}
	}
}

func TestBuiltin_Floor(t *testing.T) {
	cases := map[float32]float32{1.7: 1, -0.5: -1, 3: 3}
	for in, want := range cases {
		vm := builtinVM()
		_ = vm.SetGlobalFloat(OfsParm0, in)
		if err := BuiltinFnFloor(vm); err != nil {
			t.Fatal(err)
		}
		if v, _ := vm.GlobalFloat(OfsReturn); v != want {
			t.Errorf("Floor(%v): got %v want %v", in, v, want)
		}
	}
}

func TestBuiltin_Ceil(t *testing.T) {
	cases := map[float32]float32{1.3: 2, -0.5: 0, 3: 3}
	for in, want := range cases {
		vm := builtinVM()
		_ = vm.SetGlobalFloat(OfsParm0, in)
		if err := BuiltinFnCeil(vm); err != nil {
			t.Fatal(err)
		}
		if v, _ := vm.GlobalFloat(OfsReturn); v != want {
			t.Errorf("Ceil(%v): got %v want %v", in, v, want)
		}
	}
}

// --- RegisterMathBuiltins wires all 10 ---

func TestRegisterMathBuiltins(t *testing.T) {
	vm := builtinVM()
	vm.RegisterMathBuiltins()
	required := []int{
		BuiltinMakeVectors,
		BuiltinRandom, BuiltinNormalize, BuiltinVLen,
		BuiltinVecToYaw, BuiltinVecToAngles, BuiltinFAbs,
		BuiltinRInt, BuiltinFloor, BuiltinCeil,
	}
	for _, idx := range required {
		if vm.builtins[idx] == nil {
			t.Errorf("builtin %d not registered", idx)
		}
	}
}

// --- MAKEVECTORS -----------------------------------------------------------

// progsForMakeVectors builds a stub Progs with v_forward / v_right /
// v_up globals declared at distinct vec3-aligned offsets so
// BuiltinFnMakeVectors's FindGlobal lookups land and the writes can
// be observed via GlobalVector.
func progsForMakeVectors() *Progs {
	strs := []byte{0}
	fwdName := int32(len(strs))
	strs = append(strs, []byte("v_forward")...)
	strs = append(strs, 0)
	rgtName := int32(len(strs))
	strs = append(strs, []byte("v_right")...)
	strs = append(strs, 0)
	upName := int32(len(strs))
	strs = append(strs, []byte("v_up")...)
	strs = append(strs, 0)

	const fwdOfs, rgtOfs, upOfs = 30, 33, 36

	return &Progs{
		Header:     Header{EntityFields: 32},
		Strings:    strs,
		Globals:    make([]byte, 256*globalSlotSize),
		Statements: []Statement{{Op: OP_DONE}},
		Functions: []Function{
			{FirstStatement: 0, SName: 0},
		},
		GlobalDefs: []Def{
			{Type: uint16(EvVector), Ofs: fwdOfs, SName: fwdName},
			{Type: uint16(EvVector), Ofs: rgtOfs, SName: rgtName},
			{Type: uint16(EvVector), Ofs: upOfs, SName: upName},
		},
	}
}

// BuiltinFnMakeVectors at the zero rotation: forward = +X, right =
// -Y, up = +Z (matching mathlib.AngleVectors's identity-rotation
// shape).
func TestBuiltin_MakeVectors_Identity(t *testing.T) {
	p := progsForMakeVectors()
	vm := NewVM(p)
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{0, 0, 0})
	if err := BuiltinFnMakeVectors(vm); err != nil {
		t.Fatalf("BuiltinFnMakeVectors: %v", err)
	}
	fwdDef := p.FindGlobal("v_forward")
	if fwdDef == nil {
		t.Fatal("v_forward def missing")
	}
	fwd, _ := vm.GlobalVector(int(fwdDef.Ofs))
	if fwd != ([3]float32{1, 0, 0}) {
		t.Errorf("v_forward got %v want {1,0,0}", fwd)
	}
	rgtDef := p.FindGlobal("v_right")
	rgt, _ := vm.GlobalVector(int(rgtDef.Ofs))
	if rgt != ([3]float32{0, -1, 0}) {
		t.Errorf("v_right got %v want {0,-1,0}", rgt)
	}
	upDef := p.FindGlobal("v_up")
	up, _ := vm.GlobalVector(int(upDef.Ofs))
	if up != ([3]float32{0, 0, 1}) {
		t.Errorf("v_up got %v want {0,0,1}", up)
	}
}

// BuiltinFnMakeVectors at yaw=90 (looking +Y): forward = +Y.
// The 90-degree rotation tests that the AngleVectors plumbing is
// real (not a pass-through of the input).
func TestBuiltin_MakeVectors_Yaw90(t *testing.T) {
	p := progsForMakeVectors()
	vm := NewVM(p)
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{0, 90, 0})
	if err := BuiltinFnMakeVectors(vm); err != nil {
		t.Fatalf("BuiltinFnMakeVectors: %v", err)
	}
	fwdDef := p.FindGlobal("v_forward")
	fwd, _ := vm.GlobalVector(int(fwdDef.Ofs))
	// cos(90deg) ~= 0, sin(90deg) = 1 -- forward = (cp*cy, cp*sy, -sp)
	// = (~0, ~1, 0). Use a coarse tolerance on the near-zero leg
	// because Go's math.Cos(pi/2) returns a tiny non-zero residual.
	if math.Abs(float64(fwd[0])) > 1e-6 || math.Abs(float64(fwd[1]-1)) > 1e-6 || fwd[2] != 0 {
		t.Errorf("yaw=90 v_forward got %v want ~{0,1,0}", fwd)
	}
}

// BuiltinFnMakeVectors with a Progs that declares no v_forward etc:
// silent no-op (writes skip per-name; no error).
func TestBuiltin_MakeVectors_MissingGlobals(t *testing.T) {
	p := progsForVM(nil) // no GlobalDefs at all
	vm := NewVM(p)
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{0, 0, 0})
	if err := BuiltinFnMakeVectors(vm); err != nil {
		t.Errorf("MakeVectors with no globals: %v want nil", err)
	}
}

// BuiltinFnMakeVectors with a nil VM is a no-op no-error guard
// (matches the defensive shape of the rest of the math builtin
// family).
func TestBuiltin_MakeVectors_NilVM(t *testing.T) {
	if err := BuiltinFnMakeVectors(nil); err != nil {
		t.Errorf("MakeVectors(nil): %v want nil", err)
	}
}

// BuiltinFnMakeVectors with a VM whose progs is nil (impossible via
// the public NewVM API but defensible): silent no-op.
func TestBuiltin_MakeVectors_NilProgs(t *testing.T) {
	vm := &VM{}
	if err := BuiltinFnMakeVectors(vm); err != nil {
		t.Errorf("MakeVectors(nil-progs VM): %v want nil", err)
	}
}

// BuiltinFnMakeVectors surfaces ErrGlobalOffset when v_forward's
// declared offset overruns the Globals buffer (defensive: a
// malformed progs.dat). The SetGlobalVector call returns the error;
// the builtin propagates it verbatim.
func TestBuiltin_MakeVectors_OutOfRangeGlobalSurfaceError(t *testing.T) {
	strs := []byte{0}
	fwdName := int32(len(strs))
	strs = append(strs, []byte("v_forward")...)
	strs = append(strs, 0)
	// Globals sized to 32 slots; declare v_forward at slot 100 so
	// the SetGlobalVector at ofs=100 (= 400 bytes) overruns and
	// returns ErrGlobalOffset.
	p := &Progs{
		Header:     Header{EntityFields: 8},
		Strings:    strs,
		Globals:    make([]byte, 32*globalSlotSize),
		Statements: []Statement{{Op: OP_DONE}},
		Functions:  []Function{{FirstStatement: 0, SName: 0}},
		GlobalDefs: []Def{
			{Type: uint16(EvVector), Ofs: 100, SName: fwdName},
		},
	}
	vm := NewVM(p)
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{0, 0, 0})
	if err := BuiltinFnMakeVectors(vm); !errors.Is(err, ErrGlobalOffset) {
		t.Errorf("got %v want ErrGlobalOffset", err)
	}
}

// Sanity: the canonical builtin IDs from tyrquake's table are
// stable wire-protocol values -- assert they haven't drifted.
func TestBuiltinIDsInvariants(t *testing.T) {
	if BuiltinRandom != 7 || BuiltinNormalize != 9 || BuiltinVLen != 12 ||
		BuiltinVecToYaw != 13 || BuiltinFAbs != 43 || BuiltinRInt != 36 ||
		BuiltinFloor != 37 || BuiltinCeil != 38 || BuiltinVecToAngles != 51 {
		t.Error("builtin ID layout drift -- demos + mods will break")
	}
}

// Quick sanity that the VLen formula stays in float32 precision.
func TestBuiltin_VLen_LargeMagnitude(t *testing.T) {
	vm := builtinVM()
	_ = vm.SetGlobalVector(OfsParm0, [3]float32{1000, 0, 0})
	if err := BuiltinFnVLen(vm); err != nil {
		t.Fatal(err)
	}
	got, _ := vm.GlobalFloat(OfsReturn)
	if math.Abs(float64(got-1000)) > 0.001 {
		t.Errorf("VLen(1000,0,0): got %v want 1000", got)
	}
}
