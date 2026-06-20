// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"errors"
	"testing"
)

// progsForEntVars builds a stub Progs with one field of every type
// EntVars cares about. Field offsets fit in an EntityFields=32
// block (the EvVector at ofs 4 consumes 4..6).
func progsForEntVars() (*Progs, int32) {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	healthName := add("health")
	originName := add("origin")
	enemyName := add("enemy")
	chainName := add("chain")
	thinkName := add("think")
	classnameName := add("classname")
	mdlName := add("model")        // EvString, exercise the suppressed-style key path
	voidName := add("noisedata")   // EvVoid, unsupported by Read/WriteInt32
	helloOfs := add("hello world") // pre-baked string offset for ReadString
	return &Progs{
		Header:  Header{EntityFields: 32}, // 32 * 4 = 128 bytes
		Strings: strs,
		FieldDefs: []Def{
			{Type: uint16(EvFloat), Ofs: 1, SName: healthName},
			{Type: uint16(EvVector), Ofs: 4, SName: originName}, // 4,5,6
			{Type: uint16(EvEntity), Ofs: 8, SName: enemyName},
			{Type: uint16(EvField), Ofs: 9, SName: chainName},
			{Type: uint16(EvFunction), Ofs: 10, SName: thinkName},
			{Type: uint16(EvString) | uint16(DefSaveGlobal), Ofs: 11, SName: classnameName},
			{Type: uint16(EvString), Ofs: 12, SName: mdlName},
			{Type: uint16(EvVoid), Ofs: 13, SName: voidName},
		},
	}, helloOfs
}

func newEntVars(t *testing.T) (*EntVars, *Progs, int32) {
	t.Helper()
	p, helloOfs := progsForEntVars()
	a := NewEdictArena(p, 2)
	a.Reset()
	e, _, err := a.Alloc()
	if err != nil {
		t.Fatalf("Alloc: %v", err)
	}
	v, err := NewEntVars(p, e)
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	return v, p, helloOfs
}

// --- NewEntVars -------------------------------------------------------------

func TestNewEntVars_Success(t *testing.T) {
	p, _ := progsForEntVars()
	a := NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	v, err := NewEntVars(p, e)
	if err != nil || v == nil {
		t.Fatalf("NewEntVars: v=%v err=%v", v, err)
	}
	if v.Progs != p || v.Edict != e {
		t.Errorf("bindings: Progs=%p (want %p) Edict=%p (want %p)", v.Progs, p, v.Edict, e)
	}
}

func TestNewEntVars_NilProgs(t *testing.T) {
	p, _ := progsForEntVars()
	a := NewEdictArena(p, 2)
	a.Reset()
	e, _, _ := a.Alloc()
	if _, err := NewEntVars(nil, e); !errors.Is(err, ErrNilArg) {
		t.Errorf("nil Progs: got %v want ErrNilArg", err)
	}
}

func TestNewEntVars_NilEdict(t *testing.T) {
	p, _ := progsForEntVars()
	if _, err := NewEntVars(p, nil); !errors.Is(err, ErrNilArg) {
		t.Errorf("nil Edict: got %v want ErrNilArg", err)
	}
}

// --- ReadFloat / WriteFloat -------------------------------------------------

func TestEntVars_FloatRoundTrip(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteFloat("health", 100); err != nil {
		t.Fatalf("WriteFloat: %v", err)
	}
	got, err := v.ReadFloat("health")
	if err != nil || got != 100 {
		t.Errorf("ReadFloat: got %v err=%v", got, err)
	}
}

func TestEntVars_ReadFloat_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if _, err := v.ReadFloat("nope"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v want ErrFieldNotFound", err)
	}
}

func TestEntVars_ReadFloat_TypeMismatch(t *testing.T) {
	v, _, _ := newEntVars(t)
	if _, err := v.ReadFloat("origin"); !errors.Is(err, ErrFieldTypeMismatch) {
		t.Errorf("got %v want ErrFieldTypeMismatch", err)
	}
}

func TestEntVars_WriteFloat_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteFloat("nope", 1); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v want ErrFieldNotFound", err)
	}
}

// --- ReadVec3 / WriteVec3 ---------------------------------------------------

func TestEntVars_Vec3RoundTrip(t *testing.T) {
	v, _, _ := newEntVars(t)
	want := [3]float32{1, 2, 3}
	if err := v.WriteVec3("origin", want); err != nil {
		t.Fatalf("WriteVec3: %v", err)
	}
	got, err := v.ReadVec3("origin")
	if err != nil || got != want {
		t.Errorf("ReadVec3: got %v err=%v", got, err)
	}
}

func TestEntVars_ReadVec3_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if _, err := v.ReadVec3("nope"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestEntVars_WriteVec3_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteVec3("nope", [3]float32{}); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v", err)
	}
}

// --- ReadInt32 / WriteInt32 -------------------------------------------------

func TestEntVars_Int32RoundTrip_Entity(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteInt32("enemy", 7); err != nil {
		t.Fatalf("WriteInt32 enemy: %v", err)
	}
	got, err := v.ReadInt32("enemy")
	if err != nil || got != 7 {
		t.Errorf("got %v err=%v", got, err)
	}
}

func TestEntVars_Int32RoundTrip_Field(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteInt32("chain", 42); err != nil {
		t.Fatalf("WriteInt32 chain: %v", err)
	}
	got, _ := v.ReadInt32("chain")
	if got != 42 {
		t.Errorf("got %v want 42", got)
	}
}

func TestEntVars_Int32RoundTrip_Function(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteInt32("think", -3); err != nil {
		t.Fatalf("WriteInt32 think: %v", err)
	}
	got, _ := v.ReadInt32("think")
	if got != -3 {
		t.Errorf("got %v want -3", got)
	}
}

func TestEntVars_ReadInt32_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if _, err := v.ReadInt32("nope"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestEntVars_ReadInt32_TypeMismatch(t *testing.T) {
	v, _, _ := newEntVars(t)
	// "health" is EvFloat, not an int-shaped type.
	if _, err := v.ReadInt32("health"); !errors.Is(err, ErrFieldTypeMismatch) {
		t.Errorf("got %v", err)
	}
	// "noisedata" is EvVoid, also rejected.
	if _, err := v.ReadInt32("noisedata"); !errors.Is(err, ErrFieldTypeMismatch) {
		t.Errorf("void: got %v", err)
	}
}

func TestEntVars_WriteInt32_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteInt32("nope", 0); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestEntVars_WriteInt32_TypeMismatch(t *testing.T) {
	v, _, _ := newEntVars(t)
	if err := v.WriteInt32("health", 0); !errors.Is(err, ErrFieldTypeMismatch) {
		t.Errorf("got %v", err)
	}
}

// --- ReadString / WriteString -----------------------------------------------

func TestEntVars_ReadString_Happy(t *testing.T) {
	v, _, helloOfs := newEntVars(t)
	// Pre-stash the pre-baked "hello world" string_t into the
	// EvString "classname" field via the lower-level writer.
	def := v.Progs.FindField("classname")
	if def == nil {
		t.Fatal("classname missing from stub")
	}
	if err := v.Edict.FieldSetInt(int(def.Ofs), helloOfs); err != nil {
		t.Fatal(err)
	}
	got, err := v.ReadString("classname")
	if err != nil || got != "hello world" {
		t.Errorf("got %q err=%v", got, err)
	}
}

func TestEntVars_ReadString_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	if _, err := v.ReadString("nope"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestEntVars_ReadString_TypeMismatch(t *testing.T) {
	v, _, _ := newEntVars(t)
	if _, err := v.ReadString("health"); !errors.Is(err, ErrFieldTypeMismatch) {
		t.Errorf("got %v", err)
	}
}

func TestEntVars_WriteString_Happy(t *testing.T) {
	v, _, _ := newEntVars(t)
	// Toy interner: append to the Progs string table (which doubles
	// as our string heap for this test) + return the offset where
	// the bytes landed so ReadString can resolve them back.
	v.Interner = func(s string) int32 {
		ofs := int32(len(v.Progs.Strings))
		v.Progs.Strings = append(v.Progs.Strings, []byte(s)...)
		v.Progs.Strings = append(v.Progs.Strings, 0)
		return ofs
	}
	if err := v.WriteString("model", "progs/zombie.mdl"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	got, err := v.ReadString("model")
	if err != nil || got != "progs/zombie.mdl" {
		t.Errorf("round-trip: got %q err=%v", got, err)
	}
}

func TestEntVars_WriteString_NoInterner(t *testing.T) {
	v, _, _ := newEntVars(t)
	// Interner is nil by default.
	if err := v.WriteString("model", "anything"); !errors.Is(err, ErrNoInterner) {
		t.Errorf("got %v want ErrNoInterner", err)
	}
}

func TestEntVars_WriteString_NotFound(t *testing.T) {
	v, _, _ := newEntVars(t)
	v.Interner = func(string) int32 { return 0 }
	if err := v.WriteString("nope", "anything"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestEntVars_WriteString_TypeMismatch(t *testing.T) {
	v, _, _ := newEntVars(t)
	v.Interner = func(string) int32 { return 0 }
	if err := v.WriteString("health", "anything"); !errors.Is(err, ErrFieldTypeMismatch) {
		t.Errorf("got %v", err)
	}
}
