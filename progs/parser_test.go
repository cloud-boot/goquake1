// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"errors"
	"strings"
	"testing"
)

// progsForParser returns a stub Progs whose string table + field
// definitions cover every code path in parseEpair / ParseEdict.
func progsForParser() *Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	classnameName := add("classname")
	healthName := add("health")
	originName := add("origin")
	anglesName := add("angles")
	lightLevName := add("light_lev")
	targetName := add("target")
	useName := add("use")
	otherFieldName := add("other_field")
	enemyName := add("enemy")
	doorFnName := add("door_think")
	return &Progs{
		Header:  Header{EntityFields: 32}, // 32 * 4 = 128-byte block, plenty of room
		Strings: strs,
		FieldDefs: []Def{
			{Type: uint16(EvString), Ofs: 1, SName: classnameName},
			{Type: uint16(EvFloat), Ofs: 2, SName: healthName},
			{Type: uint16(EvVector), Ofs: 4, SName: originName}, // takes ofs 4,5,6
			{Type: uint16(EvVector), Ofs: 8, SName: anglesName},
			{Type: uint16(EvFloat), Ofs: 12, SName: lightLevName},
			{Type: uint16(EvString), Ofs: 13, SName: targetName},
			{Type: uint16(EvFunction), Ofs: 14, SName: useName},
			{Type: uint16(EvField) | uint16(DefSaveGlobal), Ofs: 15, SName: otherFieldName},
			{Type: uint16(EvEntity), Ofs: 16, SName: enemyName},
		},
		Functions: []Function{
			{FirstStatement: 1, SName: doorFnName},
		},
	}
}

func newEnt(p *Progs) *Edict {
	a := NewEdictArena(p, 4)
	a.Reset()
	e, _, _ := a.Alloc()
	return e
}

// --- happy path -------------------------------------------------------------

func TestParseEdict_BasicFields(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	intern := func(s string) int32 { return int32(len(s)) } // toy interner: returns string length

	data := `{
		"classname" "monster_zombie"
		"health" "60"
		"origin" "1 2 3"
		"angles" "10 20 30"
	}`
	rest, err := p.ParseEdict(data, e, intern)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(rest) != "" {
		t.Errorf("rest should be empty/whitespace; got %q", rest)
	}
	if e.Free {
		t.Error("parsed-with-init edict should NOT be Free")
	}

	got, _ := e.FieldInt(1)
	if got != int32(len("monster_zombie")) {
		t.Errorf("classname intern: got %d", got)
	}
	if h, _ := e.FieldFloat(2); h != 60 {
		t.Errorf("health: %v", h)
	}
	if v, _ := e.FieldVector(4); v != [3]float32{1, 2, 3} {
		t.Errorf("origin: %v", v)
	}
	if a, _ := e.FieldVector(8); a != [3]float32{10, 20, 30} {
		t.Errorf("angles: %v", a)
	}
}

// QuakeEd "angle" -> "angles" vector hack with a single scalar.
func TestParseEdict_AngleHack(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	data := `{ "angle" "90" }`
	if _, err := p.ParseEdict(data, e, nil); err != nil {
		t.Fatal(err)
	}
	if a, _ := e.FieldVector(8); a != [3]float32{0, 90, 0} {
		t.Errorf("angle hack: got %v want {0,90,0}", a)
	}
}

// QuakeEd "light" -> "light_lev" rename hack.
func TestParseEdict_LightHack(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	data := `{ "light" "200" }`
	if _, err := p.ParseEdict(data, e, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.FieldFloat(12); v != 200 {
		t.Errorf("light hack: %v", v)
	}
}

// Leading-underscore keys are silently dropped.
func TestParseEdict_LeadingUnderscoreDropped(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	data := `{ "_comment" "QuakeEd comment" "health" "10" }`
	if _, err := p.ParseEdict(data, e, nil); err != nil {
		t.Fatal(err)
	}
	if h, _ := e.FieldFloat(2); h != 10 {
		t.Errorf("health: %v", h)
	}
}

// Suppressed-warning keys (wad/sounds/mapversion) parse silently.
func TestParseEdict_SuppressedKeysSilent(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	data := `{ "wad" "gfx.wad" "sounds" "4" "mapversion" "220" "health" "1" }`
	if _, err := p.ParseEdict(data, e, nil); err != nil {
		t.Fatal(err)
	}
}

// Unknown non-suppressed keys are silently skipped (tyrquake logs;
// we keep going). Make sure the parser still finishes the block.
func TestParseEdict_UnknownKeyKeepsGoing(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	data := `{ "completely_unknown" "x" "health" "5" }`
	if _, err := p.ParseEdict(data, e, nil); err != nil {
		t.Fatal(err)
	}
	if h, _ := e.FieldFloat(2); h != 5 {
		t.Error("known field should still be set after skip")
	}
}

func TestParseEdict_TrailingSpacesInKey(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	// qparse.Token already trims at whitespace, so engineering a
	// trailing-space-in-token case requires the value separator to
	// produce one; we cover the strings.TrimRight path with a
	// direct construction.
	data := `{ "health  " "5" }`
	if _, err := p.ParseEdict(data, e, nil); err != nil {
		t.Fatal(err)
	}
	if h, _ := e.FieldFloat(2); h != 5 {
		t.Errorf("health (trailing spaces stripped): %v", h)
	}
}

// Empty block marks the edict Free (the upstream `if (!init)
// ent->free = true` postcondition).
func TestParseEdict_EmptyBlockMarksFree(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	if _, err := p.ParseEdict("{ }", e, nil); err != nil {
		t.Fatal(err)
	}
	if !e.Free {
		t.Error("empty-block edict should be Free")
	}
}

// --- ev_string / ev_entity / ev_field / ev_function specifics --------------

func TestParseEdict_StringNeedsInterner(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	if _, err := p.ParseEdict(`{ "classname" "x" }`, e, nil); !errors.Is(err, ErrNoStringInterner) {
		t.Errorf("got %v want ErrNoStringInterner", err)
	}
}

func TestParseEdict_EntityField(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	if _, err := p.ParseEdict(`{ "enemy" "42" }`, e, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.FieldInt(16); v != 42 {
		t.Errorf("enemy: %v", v)
	}
}

func TestParseEdict_FieldField(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	if _, err := p.ParseEdict(`{ "other_field" "health" }`, e, nil); err != nil {
		t.Fatal(err)
	}
	// other_field stores the OFS of the named field.
	got, _ := e.FieldInt(15)
	if got != 2 { // health.Ofs in our stub
		t.Errorf("other_field stored: %v want 2 (health.Ofs)", got)
	}
}

func TestParseEdict_FieldFieldUnknown(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`{ "other_field" "no_such_field" }`, e, nil)
	if !errors.Is(err, ErrBadFieldType) {
		t.Errorf("got %v want ErrBadFieldType", err)
	}
}

func TestParseEdict_FunctionField(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	if _, err := p.ParseEdict(`{ "use" "door_think" }`, e, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.FieldInt(14); got != 0 {
		t.Errorf("use: got %v want 0 (index of door_think)", got)
	}
}

func TestParseEdict_FunctionFieldUnknown(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`{ "use" "no_such_function" }`, e, nil)
	if !errors.Is(err, ErrUnknownFunction) {
		t.Errorf("got %v want ErrUnknownFunction", err)
	}
}

// --- error paths -----------------------------------------------------------

func TestParseEdict_MissingOpenBrace(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`"health" "5"`, e, nil)
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v want *ParseError", err)
	}
}

func TestParseEdict_EOFInBlock(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`{ "health" `, e, nil)
	if !errors.Is(err, ErrUnterminatedBlock) {
		t.Errorf("got %v want ErrUnterminatedBlock", err)
	}
}

func TestParseEdict_EOFOnKey(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`{ "health" "5"`, e, nil) // closes value but never sees }
	if !errors.Is(err, ErrUnterminatedBlock) {
		t.Errorf("got %v want ErrUnterminatedBlock", err)
	}
}

func TestParseEdict_BraceWhereValueExpected(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`{ "health" }`, e, nil)
	if !errors.Is(err, ErrMissingValue) {
		t.Errorf("got %v want ErrMissingValue", err)
	}
}

// --- vector parse edge cases ----------------------------------------------

func TestParseVec3_Happy(t *testing.T) {
	v, err := parseVec3("1 -2.5 3")
	if err != nil || v != [3]float32{1, -2.5, 3} {
		t.Errorf("got %v %v", v, err)
	}
}

func TestParseVec3_NotEnoughComponents(t *testing.T) {
	for _, s := range []string{"", "1", "1 2", "  "} {
		if _, err := parseVec3(s); !errors.Is(err, ErrBadVectorParse) {
			t.Errorf("%q: got %v", s, err)
		}
	}
}

func TestParseVec3_SurplusComponentsTolerated(t *testing.T) {
	v, err := parseVec3("1 2 3 4 5")
	if err != nil || v != [3]float32{1, 2, 3} {
		t.Errorf("got %v %v", v, err)
	}
}

// qstr.Atof returns +Inf for huge decimal-integer literals (verified
// empirically). Use that to drive parseVec3's IsInf guard.
func TestParseVec3_InfRejected(t *testing.T) {
	huge := "999999999999999999999999999999999999999"
	if _, err := parseVec3("1 " + huge + " 3"); !errors.Is(err, ErrBadVectorParse) {
		t.Errorf("got %v want ErrBadVectorParse (Inf component)", err)
	}
}

// parseEpair's EvVector arm must propagate parseVec3 errors as
// ParseError -- covers line 163-165's `if err != nil { return err }`.
func TestParseEdict_VectorParseFailure(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	_, err := p.ParseEdict(`{ "origin" "only-one-component" }`, e, nil)
	if !errors.Is(err, ErrBadVectorParse) {
		t.Errorf("got %v want ErrBadVectorParse", err)
	}
}

// --- ParseError wrapper ---------------------------------------------------

func TestParseError_Error(t *testing.T) {
	e := &ParseError{Op: "ParseEdict", Key: "health", Rest: "long-tail-text-that-runs-on-and-on", Err: errors.New("boom")}
	got := e.Error()
	if !strings.Contains(got, "ParseEdict") || !strings.Contains(got, "health") || !strings.Contains(got, "boom") || !strings.Contains(got, "...") {
		t.Errorf("Error rendering: %q", got)
	}
	if !errors.Is(e, e.Err) {
		t.Error("ParseError should unwrap to inner err")
	}
}

func TestParseError_ShortRestNoEllipsis(t *testing.T) {
	e := &ParseError{Op: "x", Rest: "tiny", Err: errors.New("y")}
	if strings.Contains(e.Error(), "...") {
		t.Errorf("short rest should not ellipsis: %q", e.Error())
	}
}

// --- parseEpair default branch (unknown Etype) -----------------------------

func TestParseEpair_DefaultBranch(t *testing.T) {
	p := progsForParser()
	e := newEnt(p)
	// Concoct a Def with an unsupported type (ev_pointer = 7) -- the
	// parseEpair switch's default arm returns nil silently.
	def := &Def{Type: uint16(EvPointer), Ofs: 0}
	if err := p.parseEpair(e, def, "anything", nil); err != nil {
		t.Errorf("default branch should be silent; got %v", err)
	}
}

// --- isSuppressedKey -----------------------------------------------------

func TestIsSuppressedKey(t *testing.T) {
	for _, k := range suppressWarningKeys {
		if !isSuppressedKey(k) {
			t.Errorf("%q should be suppressed", k)
		}
	}
	if isSuppressedKey("random_other_key") {
		t.Error("non-listed key should not be suppressed")
	}
}
