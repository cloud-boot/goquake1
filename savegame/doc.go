// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package savegame implements the Quake save/load game format: a
// text-based serialization of the per-server snapshot (map name,
// current skill level, sv.time, per-client spawn parms, globals,
// and per-edict QC fields) and the encode/decode pair the host uses
// to persist + restore play sessions.
//
// tyrquake: Host_SavegameComment + Host_Savegame_f + Host_Loadgame_f
// in NQ/host_cmd.c. The upstream format is a line-oriented dump with
// each edict bracketed by "{" / "}" lines and per-field "key" "value"
// pairs in between.
//
// The Go port keeps the byte-for-byte upstream shape so a save dumped
// by the Go engine can be read by a hex editor + matched against the
// upstream's saves slot-for-slot. Globals and per-edict fields are
// emitted by walking the [progs.Progs] field + global tables; types
// are pretty-printed as floats / quoted strings / vec3 triplets per
// the upstream's ED_Print -> ED_PrintEdicts pipeline.
//
// Bare-metal note: the package is filesystem-free. Encode writes to
// any io.Writer (in-RAM bytes.Buffer in the host's `[10]*Save` slot
// array, optionally tee'd to a serial UART for the operator to copy
// out); Load reads from any io.Reader.
package savegame
