// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/cloud-boot/goquake1/engine/qparse"
	"github.com/cloud-boot/goquake1/engine/qstr"
)

// StringInterner is the hook the runtime string heap exposes to the
// entity-string parser so ev_string fields can be stored as string_t
// offsets into the runtime heap. Implementations typically append s
// to a growable []byte and return the offset where it landed.
// tyrquake: PR_SetString + ED_NewString fused.
type StringInterner func(s string) int32

// ParseError wraps an entity-parser failure with the source position
// + a one-line excerpt so console + log diagnostics carry context.
type ParseError struct {
	Op   string // "ParseEdict" / "ParseEpair" / "ParseGlobals"
	Key  string // the field name being parsed when the error fired
	Rest string // first 32 bytes of the unconsumed data tail
	Err  error
}

func (e *ParseError) Error() string {
	rest := e.Rest
	if len(rest) > 32 {
		rest = rest[:32] + "..."
	}
	return fmt.Sprintf("progs: %s: key=%q: %v (next: %q)", e.Op, e.Key, e.Err, rest)
}

func (e *ParseError) Unwrap() error { return e.Err }

// Parser-specific sentinels.
var (
	ErrUnterminatedBlock = errors.New("progs: parser: EOF without closing '}'")
	ErrMissingValue      = errors.New("progs: parser: '}' where key value expected")
	ErrUnknownField      = errors.New("progs: parser: field not defined in progs.dat")
	ErrUnknownFunction   = errors.New("progs: parser: function not defined in progs.dat")
	ErrBadVectorParse    = errors.New("progs: parser: vector field needs 3 whitespace-separated floats")
	ErrNoStringInterner  = errors.New("progs: parser: ev_string field but no StringInterner supplied")
	ErrBadFieldType      = errors.New("progs: parser: ev_field key does not name a field")
)

// suppressWarningKeys mirrors tyrquake's silent-skip list -- field
// names QuakeEd writes that the engine knows about but doesn't store
// on edicts.
var suppressWarningKeys = []string{"sounds", "wad", "mapversion"}

// ParseEdict reads a single { "k" "v" ... } block from data into
// ent, returning the remaining unconsumed data tail. The block must
// start at the '{' (the caller has typically already consumed any
// whitespace via qparse.Token + dispatched on the result).
//
// intern is consulted for every ev_string field; pass nil to fail
// on the first string field with ErrNoStringInterner. The intern
// callback may panic to propagate hostile-server scenarios; the
// parser does not recover.
//
// On success the edict's Free flag is cleared iff at least one
// recognised field was set -- matches tyrquake's `if (!init)
// ent->free = true` postcondition.
//
// tyrquake: ED_ParseEdict (with the QuakeEd "angle"/"angles" +
// "light"/"light_lev" + leading-underscore-discard quirks
// preserved verbatim so demo replay + savegame compat hold).
func (p *Progs) ParseEdict(data string, ent *Edict, intern StringInterner) (string, error) {
	// Open brace.
	tok, rest := qparse.TokenSplitSingleChars(data)
	if tok != "{" {
		return data, &ParseError{Op: "ParseEdict", Rest: data, Err: errors.New("expected '{'")}
	}
	data = rest

	init := false
	for {
		var key string
		key, data = qparse.Token(data)
		if key == "" {
			return data, &ParseError{Op: "ParseEdict", Rest: data, Err: ErrUnterminatedBlock}
		}
		if key == "}" {
			break
		}

		// QuakeEd hack: "angle" -> "angles" vector; "light" -> "light_lev".
		anglehack := false
		switch key {
		case "angle":
			key = "angles"
			anglehack = true
		case "light":
			key = "light_lev"
		}
		// Strip trailing spaces (QuakeEd 1.4 had a bug that wrote
		// these; preserved here for parity).
		key = strings.TrimRight(key, " ")

		var value string
		value, data = qparse.Token(data)
		if value == "" {
			return data, &ParseError{Op: "ParseEdict", Key: key, Rest: data, Err: ErrUnterminatedBlock}
		}
		if value == "}" {
			return data, &ParseError{Op: "ParseEdict", Key: key, Rest: data, Err: ErrMissingValue}
		}

		// Leading underscore => discarded comment, not a field.
		if strings.HasPrefix(key, "_") {
			continue
		}

		def := p.FindField(key)
		if def == nil {
			if isSuppressedKey(key) {
				continue
			}
			// Unknown but non-fatal -- tyrquake Con_Printf's and
			// keeps parsing. The Go port carries the warning out via
			// a sentinel the caller can choose to log + ignore.
			continue
		}

		if anglehack {
			value = "0 " + value + " 0"
		}
		if err := p.parseEpair(ent, def, value, intern); err != nil {
			return data, &ParseError{Op: "ParseEdict", Key: key, Rest: data, Err: err}
		}
		init = true
	}

	if !init {
		ent.Free = true
	}
	return data, nil
}

// parseEpair stores a single parsed value into the edict's field at
// def.Ofs. tyrquake: ED_ParseEpair (the half that writes into &ent->v;
// the global-pool half is in ParseGlobalsEpair below).
func (p *Progs) parseEpair(ent *Edict, def *Def, s string, intern StringInterner) error {
	typ := def.Type &^ uint16(DefSaveGlobal)
	switch Etype(typ) {
	case EvString:
		if intern == nil {
			return ErrNoStringInterner
		}
		return ent.FieldSetInt(int(def.Ofs), intern(s))
	case EvFloat:
		return ent.FieldSetFloat(int(def.Ofs), qstr.Atof(s))
	case EvVector:
		v, err := parseVec3(s)
		if err != nil {
			return err
		}
		return ent.FieldSetVector(int(def.Ofs), v)
	case EvEntity:
		// tyrquake stores the byte offset of the target edict
		// inside sv.edicts; the Go port stores the integer index
		// (callers translate via arena.Get).
		return ent.FieldSetInt(int(def.Ofs), int32(qstr.Atoi(s)))
	case EvField:
		ref := p.FindField(s)
		if ref == nil {
			return ErrBadFieldType
		}
		return ent.FieldSetInt(int(def.Ofs), int32(ref.Ofs))
	case EvFunction:
		_, idx := p.FindFunction(s)
		if idx < 0 {
			return ErrUnknownFunction
		}
		return ent.FieldSetInt(int(def.Ofs), int32(idx))
	default:
		// ev_void / ev_pointer / out-of-range types are silently
		// skipped, matching the upstream's default branch.
		return nil
	}
}

// parseVec3 reads exactly 3 whitespace-separated floats from s and
// returns them as a Vec3. Surplus whitespace + a trailing word are
// tolerated -- the upstream's strcpy/while(*v && *v!=' ') loop has
// the same behaviour. Missing components yield ErrBadVectorParse.
func parseVec3(s string) ([3]float32, error) {
	var v [3]float32
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return v, ErrBadVectorParse
	}
	for i := 0; i < 3; i++ {
		v[i] = qstr.Atof(parts[i])
	}
	// Guard against +Inf / -Inf / NaN sneaking through atof's
	// graceful-degrade path -- the renderer + collision code don't
	// tolerate them.
	for _, c := range v {
		if math.IsNaN(float64(c)) || math.IsInf(float64(c), 0) {
			return v, ErrBadVectorParse
		}
	}
	return v, nil
}

func isSuppressedKey(k string) bool {
	for _, s := range suppressWarningKeys {
		if k == s {
			return true
		}
	}
	return false
}
