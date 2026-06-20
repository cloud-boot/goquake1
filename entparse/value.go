// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package entparse

import (
	"errors"
	"strconv"
	"strings"
)

// FieldType is the QC entvars field type tag for one entity field.
// Used to dispatch entparse's per-type value parsers when assigning
// parsed entity-string fields into edicts. Matches the QC etype_t
// values verbatim so a future progs-runtime layer can use these
// constants directly. tyrquake: ev_* in include/pr_comp.h.
type FieldType int

const (
	FieldTypeVoid     FieldType = iota // ev_void -- unused
	FieldTypeString                    // ev_string
	FieldTypeFloat                     // ev_float
	FieldTypeVector                    // ev_vector ("x y z")
	FieldTypeEntity                    // ev_entity (edict index)
	FieldTypeField                     // ev_field (field-name reference)
	FieldTypeFunction                  // ev_function (function-name reference)
	FieldTypePointer                   // ev_pointer -- internal, never appears in entity strings
)

// ErrBadFloat fires when ParseFloat receives a non-empty, non-numeric
// input. tyrquake's atof would silently return 0 for "garbage"; the
// Go port surfaces the malformed value so the caller can decide
// whether to log or substitute.
var ErrBadFloat = errors.New("entparse: bad ev_float value")

// ErrBadVec3 fires when ParseVec3 receives an input that does not
// consist of exactly three whitespace-separated floats. tyrquake's
// strcpy+atof triple silently produces zeros for the missing axes;
// the Go port surfaces the malformed value.
var ErrBadVec3 = errors.New("entparse: bad ev_vector value")

// ErrBadEntity fires when ParseEntity receives a non-empty,
// non-numeric input. tyrquake's atoi(atof(s)) would silently return
// 0 for "garbage"; the Go port surfaces the malformed value.
var ErrBadEntity = errors.New("entparse: bad ev_entity value")

// ParseFloat parses one QC ev_float value -- the C upstream uses
// atof which is forgiving (treats leading whitespace + trailing
// garbage as best-effort; non-numeric becomes 0). The Go port
// uses strconv.ParseFloat with a TRIMMED input + an empty-string
// fallback to 0 to match atof's most-common semantics.
//
// Returns ErrBadFloat on syntactically invalid input that's not
// just whitespace (an empty / whitespace-only input becomes 0).
// tyrquake: atof(s) inside ED_ParseEpair's ev_float branch.
func ParseFloat(raw string) (float32, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, ErrBadFloat
	}
	return float32(v), nil
}

// ParseVec3 parses a QC ev_vector value of the form "x y z" -- 3
// whitespace-separated floats. Extra whitespace + trailing garbage
// in any axis surface as ErrBadVec3. Empty / whitespace-only input
// becomes the zero vector.
//
// tyrquake: the sscanf("%f %f %f", ...) inside ED_ParseEpair's
// ev_vector branch.
func ParseVec3(raw string) ([3]float32, error) {
	var out [3]float32
	s := strings.TrimSpace(raw)
	if s == "" {
		return out, nil
	}
	// strings.Fields collapses runs of whitespace, matching the
	// upstream "skip past separator spaces" loop in ED_ParseEpair's
	// ev_vector branch (which advances v while *v == ' ').
	parts := strings.Fields(s)
	if len(parts) != 3 {
		return [3]float32{}, ErrBadVec3
	}
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 32)
		if err != nil {
			return [3]float32{}, ErrBadVec3
		}
		out[i] = float32(v)
	}
	return out, nil
}

// ParseEntity parses a QC ev_entity value (an int edict index as
// the C upstream does it -- atoi(s) cast through atof so non-
// numeric becomes 0). Returns ErrBadEntity if the input is
// non-empty + non-numeric (the empty-or-whitespace case becomes 0).
//
// tyrquake: int(atof(s)) inside ED_ParseEpair's ev_entity branch.
func ParseEntity(raw string) (int32, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	// Upstream goes through atof then truncates to int, so "1.9"
	// would yield 1. Mirror that via ParseFloat to keep the Go port
	// behaviourally identical for fractional input.
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, ErrBadEntity
	}
	return int32(v), nil
}

// ParseString returns the raw value verbatim -- the C upstream
// passes the value through PR_NewString to intern it into the
// progs strings table. The Go port returns the raw string; the
// caller (future progs-runtime glue) decides interning.
//
// Always succeeds; the function exists for API symmetry with the
// other parsers + for future-proofing if interning rules change.
// tyrquake: the ev_string branch in ED_ParseEpair.
func ParseString(raw string) string {
	return raw
}
