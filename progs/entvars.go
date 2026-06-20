// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import "errors"

// EntVars is a typed accessor + mutator for an edict's per-entity
// QC fields. It acts as a name->value bridge: callers do
// v.ReadFloat("health") / v.WriteVec3("origin", o) without touching
// raw field-offset arithmetic or the QC def-table layout.
//
// The Progs reference lets EntVars resolve field names to the
// (offset, type) pair in the QC's field-def table; the Edict
// reference holds the actual per-instance values.
//
// tyrquake: the inline `ent->v.field` macros (the v.field tokens
// expand to (entvars_t *)((byte *)ent + offsetof(edict_t, v))->field).
// The Go port surfaces the same SHAPE explicitly because Go doesn't
// have C's struct-overlay tricks; sv_phys / ED_LoadFromFile /
// SV_CreateBaseline reach for EntVars instead of recomputing the
// offset+type lookup themselves.
//
// Interner, when non-nil, is consulted by WriteString to fold a Go
// string into the runtime string heap and produce the string_t
// offset stored in the field. The progs package itself ships only
// the StringInterner contract (see parser.go); concrete heaps live
// in the surrounding engine packages, so callers wire one in
// explicitly. With Interner == nil, WriteString fails with
// ErrNoInterner.
type EntVars struct {
	Progs    *Progs
	Edict    *Edict
	Interner StringInterner
}

// Sentinel errors.
var (
	ErrNilArg            = errors.New("progs: nil Progs or Edict")
	ErrFieldNotFound     = errors.New("progs: entvars field not found")
	ErrFieldTypeMismatch = errors.New("progs: entvars field type mismatch")
	ErrNoInterner        = errors.New("progs: entvars WriteString needs a StringInterner")
)

// NewEntVars binds an Edict to its Progs for typed field access.
// Both arguments must be non-nil; returns ErrNilArg otherwise. The
// resulting EntVars has no Interner -- set v.Interner directly when
// the caller needs WriteString.
func NewEntVars(p *Progs, e *Edict) (*EntVars, error) {
	if p == nil || e == nil {
		return nil, ErrNilArg
	}
	return &EntVars{Progs: p, Edict: e}, nil
}

// fieldOfType resolves name to a field def and checks its type tag
// (with the DefSaveGlobal bit stripped) against want.
func (v *EntVars) fieldOfType(name string, want Etype) (*Def, error) {
	def := v.Progs.FindField(name)
	if def == nil {
		return nil, ErrFieldNotFound
	}
	if Etype(def.Type&^uint16(DefSaveGlobal)) != want {
		return nil, ErrFieldTypeMismatch
	}
	return def, nil
}

// ReadFloat returns the value of the EvFloat field named name.
// Errors with ErrFieldNotFound when no such field is defined and
// ErrFieldTypeMismatch when the field is not EvFloat.
// tyrquake: ent->v.<name> for float-typed fields.
func (v *EntVars) ReadFloat(name string) (float32, error) {
	def, err := v.fieldOfType(name, EvFloat)
	if err != nil {
		return 0, err
	}
	return v.Edict.FieldFloat(int(def.Ofs))
}

// WriteFloat stores value into the EvFloat field named name.
// Same error semantics as ReadFloat.
// tyrquake: ent->v.<name> = value for float-typed fields.
func (v *EntVars) WriteFloat(name string, value float32) error {
	def, err := v.fieldOfType(name, EvFloat)
	if err != nil {
		return err
	}
	return v.Edict.FieldSetFloat(int(def.Ofs), value)
}

// ReadVec3 returns the value of the EvVector field named name. QC
// vectors are three contiguous floats; the helper packs them as a
// single [3]float32.
// tyrquake: ent->v.<name>[0..2] for vector fields.
func (v *EntVars) ReadVec3(name string) ([3]float32, error) {
	def, err := v.fieldOfType(name, EvVector)
	if err != nil {
		return [3]float32{}, err
	}
	return v.Edict.FieldVector(int(def.Ofs))
}

// WriteVec3 stores value into the EvVector field named name.
// tyrquake: VectorCopy(value, ent->v.<name>).
func (v *EntVars) WriteVec3(name string, value [3]float32) error {
	def, err := v.fieldOfType(name, EvVector)
	if err != nil {
		return err
	}
	return v.Edict.FieldSetVector(int(def.Ofs), value)
}

// ReadInt32 returns the value of an integer-typed field (EvEntity,
// EvField or EvFunction). The QC compiler stores all three as a
// raw int32 in the field block; the callers (server entity-pointer
// chasing, function dispatch) treat the value as an index.
// tyrquake: ent->v.<name> for int/entity/function-typed fields.
func (v *EntVars) ReadInt32(name string) (int32, error) {
	def := v.Progs.FindField(name)
	if def == nil {
		return 0, ErrFieldNotFound
	}
	switch Etype(def.Type &^ uint16(DefSaveGlobal)) {
	case EvEntity, EvField, EvFunction:
	default:
		return 0, ErrFieldTypeMismatch
	}
	return v.Edict.FieldInt(int(def.Ofs))
}

// WriteInt32 stores value into an integer-typed field. Same
// type-acceptance rules as ReadInt32.
func (v *EntVars) WriteInt32(name string, value int32) error {
	def := v.Progs.FindField(name)
	if def == nil {
		return ErrFieldNotFound
	}
	switch Etype(def.Type &^ uint16(DefSaveGlobal)) {
	case EvEntity, EvField, EvFunction:
	default:
		return ErrFieldTypeMismatch
	}
	return v.Edict.FieldSetInt(int(def.Ofs), value)
}

// ReadString returns the resolved string for the EvString field
// named name. The field holds a string_t offset into the Progs
// string table; the helper hands back the actual bytes via
// Progs.String.
//
// FieldInt only errors when its offset would land outside the
// entity field block; def.Ofs came from p.FieldDefs which Load
// validates against EntityFields, so the FieldInt error path is
// unreachable when the invariant holds (and is dropped here,
// bsptrace-style).
//
// tyrquake: PR_GetString(ent->v.<name>).
func (v *EntVars) ReadString(name string) (string, error) {
	def, err := v.fieldOfType(name, EvString)
	if err != nil {
		return "", err
	}
	off, _ := v.Edict.FieldInt(int(def.Ofs))
	return v.Progs.String(off), nil
}

// WriteString stores value into the EvString field named name by
// routing it through v.Interner to obtain a string_t offset. With
// v.Interner == nil the call fails with ErrNoInterner -- the progs
// package does not own the runtime string heap, so callers must
// supply one (see StringInterner in parser.go).
//
// tyrquake: ent->v.<name> = PR_SetString(ED_NewString(value)).
func (v *EntVars) WriteString(name string, value string) error {
	def, err := v.fieldOfType(name, EvString)
	if err != nil {
		return err
	}
	if v.Interner == nil {
		return ErrNoInterner
	}
	return v.Edict.FieldSetInt(int(def.Ofs), v.Interner(value))
}
