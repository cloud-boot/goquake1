// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package entparse turns the BSP "entities" lump (a NUL-terminated
// ASCII blob the level compiler emits at the end of every .bsp) into
// a slice of key->value field maps, one per entity block.
//
// The entities lump is the human-readable side of the map -- every
// info_player_start, light, monster_army, trigger_multiple, and the
// worldspawn itself live there as a brace-delimited block of
// "key" "value" pairs. tyrquake parses it in ED_LoadFromFile, which
// interleaves the COM_Parse tokenisation loop with field assignment
// into the active edict array. This package ports the tokenisation
// loop ONLY; field assignment needs progs runtime knowledge (ddef
// table, anglehack, _-prefixed comment keys) and stays in the server
// layer where the edicts and progs state live.
//
// Upstream pin: github.com/sezero/tyrquake @ 6531579
// C source:    common/pr_edict.c -- ED_LoadFromFile, ED_ParseEdict
// Hand-port date: 2026-06-20
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping vs tyrquake:
//   - ED_LoadFromFile's outer "parse {" loop -> ParseEntities's
//     outer loop, except the Go port collects each entity into an
//     EntityFields map instead of mutating an edict in place.
//   - ED_ParseEdict's inner "key/value pairs until }" loop -> the
//     inner two-Token loop inside ParseEntities, minus the angle/
//     light/_-prefix/ED_FindField hacks (all progs-specific).
//   - COM_Parse with split_single_chars=true -> qparse.TokenSplitSingleChars
//     so '{' and '}' tokenise on their own.
//
// The errors are sentinels so callers can branch on them with
// errors.Is. Upstream calls SV_Error / Host_Error for malformed
// input; the Go port returns an error instead and lets the caller
// decide whether to abort the map load or skip the broken entity.
package entparse
