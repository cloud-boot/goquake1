// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package qstr is the Go port of tyrquake's quake-specific string-
// parsing helpers from common/common.c (the LIBRARY REPLACEMENT
// FUNCTIONS block, lines 1-365).
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15
// Port conventions: see ../../CONVENTIONS.md
//
// Exported surface:
//
//   - Atoi  -- tyrquake Q_atoi: parse int with hex (0xNN), octal-by-
//     leading-zero is NOT recognised (tyrquake falls through into the
//     decimal path, the leading zero just contributes a zero digit),
//     and the quoted-character literal form 'X' (returns the ASCII
//     codepoint with sign applied).
//   - Atof  -- tyrquake Q_atof: parse float with hex and quoted-char
//     forms identical to Atoi; decimal path supports a single '.' and
//     no exponent (matches upstream literally; tyrquake's parser does
//     not honour 'e'/'E' notation despite the brief).
//   - StrBuf -- tyrquake COM_GetStrBuf: 8-slot ring of byte buffers
//     for transient text formatting. Each slot is COM_STRBUF_LEN
//     (2048) bytes. Callers must not retain a returned buffer across
//     more than 7 further StrBuf calls.
//
// Intentionally skipped from the same C block, with rationale:
//
//   - qvsnprintf, qsnprintf -- the upstream wraps C's vsnprintf to
//     enforce NUL-termination on truncation. Go strings carry their
//     length and fmt.Sprintf returns a complete string; there is no
//     untruncated-write hazard to guard against.
//   - qstrncpy -- the upstream wraps strncpy to enforce NUL-
//     termination. Go has no NUL-terminated string concept; idiomatic
//     copy is `dst := src` for strings or `copy(dst, src)` for slices.
//
// Note on the spec brief: an earlier draft referenced "4 rotating
// 1024-byte buffers". The shipping tyrquake source actually uses
// 8x2048 (COM_STRBUF_LEN = 2048, mask = 7 in the index). We follow
// the source so call-site sizing assumptions stay bit-exact.
package qstr
