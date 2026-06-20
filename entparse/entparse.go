// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package entparse

import (
	"errors"

	"github.com/go-quake1/engine/qparse"
)

// EntityFields is the parsed key->value map for one entity in the
// entities lump. The order of keys is not preserved (it's a map);
// callers that need stable iteration must sort separately.
type EntityFields map[string]string

// ErrUnclosedBrace fires when an entity block opens with '{' but the
// tokeniser reaches end-of-input before the matching '}'. tyrquake:
// ED_ParseEdict's "EOF without closing brace" SV_Error path.
var ErrUnclosedBrace = errors.New("entparse: '{' without matching '}'")

// ErrUnmatchedClose fires when ParseEntities encounters '}' at the
// top level (i.e. before any '{'). tyrquake: ED_LoadFromFile's
// "found %s when expecting {" SV_Error path when the first non-
// whitespace token is '}'.
var ErrUnmatchedClose = errors.New("entparse: '}' without prior '{'")

// ErrOrphanField fires when an entity block contains an odd number
// of tokens between '{' and '}' -- i.e. a key with no paired value.
// tyrquake: ED_ParseEdict's "closing brace without data" SV_Error
// path when the value-side COM_Parse returns '}'.
var ErrOrphanField = errors.New("entparse: key without paired value")

// ParseEntities tokenizes the entities lump (a NUL-terminated ASCII
// block from bspfile.File.Entities()) into a slice of EntityFields,
// one per entity block. tyrquake: ED_LoadFromFile's COM_Parse loop
// (the field-collection half only; field assignment into edicts is
// the caller's job).
//
// Wire shape per entity: '{' followed by alternating <key> <value>
// tokens, terminated by '}'. Whitespace + comments (// ... \n)
// between tokens is skipped by the tokenizer. Trailing data after
// the last '}' is silently dropped (matches upstream: the outer
// loop's "data = COM_Parse(data); if (!data) break;" exits cleanly
// on EOF regardless of what unrecognised bytes preceded it).
//
// Returns:
//
//	nil, nil                 -- input was empty / all whitespace
//	[]EntityFields, nil      -- happy path
//	nil, ErrUnclosedBrace    -- '{' without matching '}'
//	nil, ErrUnmatchedClose   -- '}' without prior '{'
//	nil, ErrOrphanField      -- a key without a paired value
func ParseEntities(blob []byte) ([]EntityFields, error) {
	// qparse operates on string; the lump is ASCII so the conversion
	// is a no-op semantically. tyrquake: COM_Parse takes const char *,
	// the Go port threads (token, rest) instead of mutating a cursor.
	data := string(blob)

	var out []EntityFields

	// next pulls one token from data and reports whether qparse
	// returned a real token. qparse collapses two cases into the
	// same ("", "") return: true EOF and an empty quoted string `""`
	// sitting at end-of-input. Disambiguate with the post-call rest:
	// EOF leaves data == "" AND tok == ""; an empty quoted token with
	// any trailing input leaves data != ""; an empty quoted token at
	// end-of-input is indistinguishable from EOF (a well-formed
	// entities lump never ends with a bare "" -- the final byte is
	// always '}' -- so the ambiguity does not arise in practice and
	// we treat it as EOF, matching the upstream "loop breaks on the
	// first NULL return").
	next := func() (tok string, ok bool) {
		tok, data = qparse.TokenSplitSingleChars(data)
		return tok, tok != "" || data != ""
	}

	for {
		// Parse the opening brace (or EOF). tyrquake:
		// ED_LoadFromFile's "parse the opening brace" block.
		tok, ok := next()
		if !ok {
			// EOF (or all-whitespace input). Matches the upstream
			// "if (!data) break;" exit.
			return out, nil
		}
		if tok != "{" {
			// Upstream SV_Errors here ("found %s when expecting {").
			// All flavours of "non-brace token sitting outside any
			// entity block" -- including a bare '}' -- collapse to
			// ErrUnmatchedClose since the spec only carves three
			// sentinels and "data appears outside any block" is
			// structurally the same failure as a stray close.
			return nil, ErrUnmatchedClose
		}

		// Inside an entity block: collect key/value pairs until the
		// closing brace. tyrquake: ED_ParseEdict's inner while(1).
		fields := EntityFields{}
		for {
			// Parse key (or closing brace).
			key, ok := next()
			if !ok {
				return nil, ErrUnclosedBrace
			}
			if key == "}" {
				break
			}

			// Parse value. Upstream SV_Errors on EOF or on a '}'
			// where data was expected; the Go port maps the former
			// to ErrUnclosedBrace and the latter to ErrOrphanField.
			val, ok := next()
			if !ok {
				return nil, ErrUnclosedBrace
			}
			if val == "}" {
				return nil, ErrOrphanField
			}

			// Store. Upstream maps both key and value through the
			// progs ddef table here; we just stash the raw token
			// strings since field interpretation is the caller's
			// job. An empty key or value is technically valid in
			// upstream (it just stores ""); a quoted "" produces
			// the empty string from qparse, which we preserve.
			fields[key] = val
		}
		out = append(out, fields)
	}
}
