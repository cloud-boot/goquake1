// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package sizebuf is the Go port of tyrquake's sizebuf_t plus the
// SZ_* helpers (SZ_HunkAlloc, SZ_Clear, SZ_GetSpace, SZ_Write,
// SZ_Print) defined in common/common.c.
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//   - C sizebuf_t -> Buffer.
//   - C SZ_HunkAlloc(buf, maxsize) -> New(backing []byte): the caller
//     performs the hunk allocation upstream and hands the backing
//     slice to the constructor.
//   - C qboolean allowoverflow / overflowed -> Buffer.AllowOverflow
//     (exported field) and Buffer.Overflowed() (accessor, since the
//     flag is set by GetSpace and callers only read it).
//   - C SZ_GetSpace's Sys_Error("overflow without allowoverflow") path
//     becomes ErrSizeBufOverflow returned from GetSpace; the caller
//     decides whether to panic or recover. The "length > maxsize"
//     path becomes ErrSizeBufRequestTooLarge.
//   - C SZ_Print's "overwrite trailing null" trick is preserved
//     byte-for-byte so the produced wire bytes match tyrquake on
//     consecutive Print calls.
package sizebuf
