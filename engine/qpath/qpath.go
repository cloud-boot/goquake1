// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qpath

import "strings"

// SkipPath returns the basename portion of s -- everything after the
// last '/'. When no '/' is present, returns s unchanged. tyrquake:
// COM_SkipPath.
func SkipPath(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// StripExtension returns s with its trailing ".ext" removed. The cut
// considers only the basename: a dot earlier in the path (e.g.
// "id1/maps/foo") is left alone. When no extension is present,
// returns s unchanged. tyrquake: COM_StripExtension.
func StripExtension(s string) string {
	base := SkipPath(s)
	dot := strings.LastIndexByte(base, '.')
	if dot < 0 {
		return s
	}
	return s[:len(s)-len(base)+dot]
}

// FileExtension returns the extension of s WITHOUT the leading '.'.
// Returns "" when no extension is present. The cut considers only the
// basename. tyrquake: COM_FileExtension.
func FileExtension(s string) string {
	base := SkipPath(s)
	dot := strings.LastIndexByte(base, '.')
	if dot < 0 {
		return ""
	}
	return base[dot+1:]
}

// FileBase returns the basename of s without directory AND without
// extension. tyrquake's quirk: when the resulting basename would be
// shorter than 2 characters (the empty case or a stray single byte),
// it substitutes the literal "?model?" -- a sentinel the model loader
// uses to flag a missing-name asset. Preserved verbatim for parity.
// tyrquake: COM_FileBase.
func FileBase(s string) string {
	base := SkipPath(s)
	dot := strings.LastIndexByte(base, '.')
	var name string
	if dot < 0 {
		name = base
	} else {
		name = base[:dot]
	}
	if len(name) < 2 {
		return "?model?"
	}
	return name
}

// DefaultExtension returns path with ext appended when path's basename
// has no '.'. When the basename already contains a '.', returns path
// unchanged. tyrquake: COM_DefaultExtension. (The upstream signature
// also returns -1 when the result would not fit in a caller-supplied
// buffer; in Go the caller's string allocation never overflows, so
// that branch is collapsed away.)
func DefaultExtension(path, ext string) string {
	base := SkipPath(path)
	if strings.IndexByte(base, '.') >= 0 {
		return path
	}
	return path + ext
}

// CheckSuffix reports whether path ends in suffix, case-sensitively.
// tyrquake: COM_CheckSuffix (strcmp-based -- we deliberately do NOT
// fold case so "*.PAK" never matches "*.pak"; the engine never stores
// upper-case suffixes in its own paths and operators are expected to
// normalise before calling).
func CheckSuffix(path, suffix string) bool {
	return strings.HasSuffix(path, suffix)
}
