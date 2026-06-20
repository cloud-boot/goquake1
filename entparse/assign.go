// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package entparse

import (
	"errors"
	"fmt"

	"github.com/go-quake1/engine/progs"
)

// classnameKey is the QC field name the entity-spawn dispatcher reads
// from each parsed entity to bind a spawn function. The C upstream
// hard-codes the literal "classname" both at parse-time (ED_ParseEdict
// stores it via the normal ev_string branch) and at dispatch-time
// (ED_LoadFromFile resolves ent->v.classname). The constant exists
// here so the same literal is used in AssignFields + SpawnEntities.
const classnameKey = "classname"

// angleKey + anglesKey + lightKey + lightLevKey are the QuakeEd-era
// shortcut aliases. tyrquake: ED_ParseEdict's "angle"/"angles" +
// "light"/"light_lev" inline rewrites.
const (
	angleKey    = "angle"
	anglesKey   = "angles"
	lightKey    = "light"
	lightLevKey = "light_lev"
)

// ErrAssign wraps a per-field assignment failure with the field name
// for diagnostic context. The wrapped error is one of the entparse
// value-parser sentinels (ErrBadFloat / ErrBadVec3 / ErrBadEntity) or
// a progs.EntVars error (ErrFieldTypeMismatch / ErrNoInterner / ...).
type ErrAssign struct {
	Key string
	Err error
}

func (e *ErrAssign) Error() string {
	return fmt.Sprintf("entparse: assign key=%q: %v", e.Key, e.Err)
}

func (e *ErrAssign) Unwrap() error { return e.Err }

// AssignFields walks one EntityFields map + writes each (key, value)
// pair into the typed edict via EntVars. tyrquake: ED_ParseEdict's
// per-field loop. The single-edict half of ED_LoadFromFile.
//
// Looks up each key in progs.Progs.FindField. If the field exists,
// dispatches on its QC type (Float / Vector / Int / Entity / Function
// / String) to the matching value-parser (entparse.ParseFloat etc.)
// + writer (EntVars.Write*).
//
// Skips unknown fields (the C upstream prints "no field" but doesn't
// fail). Returns the FIRST parse error encountered; subsequent fields
// still get processed (best-effort assignment, matches upstream).
//
// Special-case "classname": stored via WriteString verbatim. Classname
// is the entity-spawn dispatcher key the QC runtime uses to bind a
// spawn function -- we just store it; the spawn function call comes
// later in the QC runtime layer.
//
// Special-case "angle" (legacy QuakeEd shortcut): mapped to
// ent.v.angles[1] = float(value). The C upstream has this hack inline
// in ED_ParseEpair.
//
// Special-case "light" (QuakeEd alias): maps to ent.v.light_lev.
// Inline-documented; behaviour matches C.
func AssignFields(fields EntityFields, p *progs.Progs, ent *progs.Edict, interner progs.StringInterner) error {
	if p == nil || ent == nil {
		return progs.ErrNilArg
	}
	// NewEntVars only fails when either arg is nil; both are guarded
	// above so the error return is unreachable (dropped bsptrace-style).
	v, _ := progs.NewEntVars(p, ent)
	v.Interner = interner

	var firstErr error
	record := func(key string, e error) {
		if firstErr == nil {
			firstErr = &ErrAssign{Key: key, Err: e}
		}
	}

	for key, raw := range fields {
		// QuakeEd shortcuts. "angle" widens the scalar yaw into the
		// 3-component "angles" vector with the yaw on the Y axis;
		// "light" is an alias for "light_lev".
		switch key {
		case angleKey:
			yaw, perr := ParseFloat(raw)
			if perr != nil {
				record(key, perr)
				continue
			}
			if werr := v.WriteVec3(anglesKey, [3]float32{0, yaw, 0}); werr != nil {
				record(key, werr)
			}
			continue
		case lightKey:
			key = lightLevKey
		}

		def := p.FindField(key)
		if def == nil {
			// Unknown field -- silent skip (C upstream Con_Printfs +
			// continues; the Go port drops the printf for this commit
			// since the spec says "no printf is fine").
			continue
		}

		typ := progs.Etype(def.Type &^ uint16(progs.DefSaveGlobal))
		switch typ {
		case progs.EvFloat:
			f, perr := ParseFloat(raw)
			if perr != nil {
				record(key, perr)
				continue
			}
			if werr := v.WriteFloat(key, f); werr != nil {
				record(key, werr)
			}

		case progs.EvVector:
			vec, perr := ParseVec3(raw)
			if perr != nil {
				record(key, perr)
				continue
			}
			if werr := v.WriteVec3(key, vec); werr != nil {
				record(key, werr)
			}

		case progs.EvEntity:
			n, perr := ParseEntity(raw)
			if perr != nil {
				record(key, perr)
				continue
			}
			if werr := v.WriteInt32(key, n); werr != nil {
				record(key, werr)
			}

		case progs.EvField:
			// ev_field stores the OFFSET of the named field; resolve
			// the name and stash its Ofs. Unknown name is an error
			// (matches C upstream's "Can't find field %s" + false).
			ref := p.FindField(raw)
			if ref == nil {
				record(key, errors.New("entparse: ev_field references unknown field "+raw))
				continue
			}
			if werr := v.WriteInt32(key, int32(ref.Ofs)); werr != nil {
				record(key, werr)
			}

		case progs.EvFunction:
			// ev_function stores the (1-based) function index; unknown
			// name is an error (matches C upstream's "Can't find
			// function %s" + false).
			_, idx := p.FindFunction(raw)
			if idx < 0 {
				record(key, errors.New("entparse: ev_function references unknown function "+raw))
				continue
			}
			if werr := v.WriteInt32(key, int32(idx)); werr != nil {
				record(key, werr)
			}

		case progs.EvString:
			// Both "classname" and any other EvString field go through
			// the same WriteString path; the classname literal carries
			// no special parser behaviour beyond being stored verbatim.
			if werr := v.WriteString(key, ParseString(raw)); werr != nil {
				record(key, werr)
			}

		default:
			// ev_void / ev_pointer / out-of-range types are silently
			// skipped, matching ED_ParseEpair's default branch.
		}
	}

	return firstErr
}

// SpawnEntities iterates the parsed entity list + populates the
// edict pool. edictAt is a callback that returns the *Edict for a
// given index (caller's edict-allocator). spawn is the post-assign
// hook (typically the QC "ClassName::spawn" call); if nil, the
// hook is skipped (just the field assignment runs).
//
// The first entity in the list IS the worldspawn (entity 0); the
// caller is responsible for having edictAt(0) return a fresh
// world edict.
//
// tyrquake: ED_LoadFromFile.
func SpawnEntities(entities []EntityFields, p *progs.Progs, edictAt func(int) *progs.Edict, interner progs.StringInterner, spawn func(ent *progs.Edict, classname string)) error {
	if p == nil || edictAt == nil {
		return progs.ErrNilArg
	}

	var firstErr error
	for i, fields := range entities {
		ent := edictAt(i)
		if ent == nil {
			// Allocator declined to hand back a slot; record the
			// failure and keep walking the rest of the list (the C
			// upstream's SV_Error would abort, but the Go port favours
			// best-effort so a malformed map doesn't take the server
			// down before the operator can diagnose it).
			if firstErr == nil {
				firstErr = fmt.Errorf("entparse: SpawnEntities: edictAt(%d) returned nil", i)
			}
			continue
		}
		if err := AssignFields(fields, p, ent, interner); err != nil && firstErr == nil {
			firstErr = err
		}

		// The C upstream skips the spawn call when classname is unset
		// (Con_Printf + ED_Free + continue). The Go port mirrors the
		// "no spawn call" half; cleanup is the caller's job since the
		// edict pool is also caller-owned here.
		if spawn == nil {
			continue
		}
		classname, ok := fields[classnameKey]
		if !ok || classname == "" {
			continue
		}
		spawn(ent, classname)
	}

	return firstErr
}
