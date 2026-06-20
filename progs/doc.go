// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package progs is the Go port of tyrquake's QuakeC bytecode runtime.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// Scope of THIS commit: only the progs.dat file-format parser and the
// shared opcode + ev_* + DEF_SAVEGLOBAL constants from
// include/pr_comp.h. The interpreter (pr_exec.c, ~739 LoC) and the
// edict/builtin machinery (pr_edict.c + pr_cmds.c, ~3300 LoC C) land
// in follow-up commits as separate cohesive units.
//
// progs.dat layout (little-endian throughout):
//
//   header (dprograms_t) = 28 int32 fields:
//     version (must be 6)
//     crc           CRC of the matching C-side progdefs.h
//     ofs_statements + numstatements  (8 bytes each: u16 op + 3x i16)
//     ofs_globaldefs + numglobaldefs  (8 bytes each: u16 type + u16 ofs + i32 s_name)
//     ofs_fielddefs  + numfielddefs   (8 bytes each: same shape as ddef_t)
//     ofs_functions  + numfunctions   (36 bytes each: dfunction_t)
//     ofs_strings    + strings_size   (the string table; NUL-separated;
//                                       string_t is a byte offset into this)
//     ofs_globals    + numglobals     (the global value pool; 4 bytes per slot)
//     entityfields                    number of 4-byte slots per edict
//
// The parser surfaces every section verbatim as a typed slice so the
// interpreter port can index into them with byte-equality to the C
// upstream. CRC verification is exposed but not enforced -- the
// caller decides (the engine warns; a test fixture may bypass).
package progs
