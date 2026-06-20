// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package qpath is the Go port of tyrquake's path-string helpers
// (COM_SkipPath, COM_StripExtension, COM_FileExtension, COM_FileBase,
// COM_DefaultExtension, COM_CheckSuffix from common/common.c lines
// 873-972).
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// Naming: the C "COM_" prefix is dropped; the package qualifier carries
// the intent (qpath.SkipPath, qpath.FileBase). The package is named
// `qpath` rather than `path` so callers cannot accidentally shadow the
// stdlib `path` or `path/filepath` packages whose semantics differ.
//
// Separator: Quake is a forward-slash-only engine. This package never
// looks at '\\' (backslash); operators on Windows must feed already-
// normalised slash-separated paths. We deliberately do NOT import
// stdlib path/filepath here to make that contract enforceable by code
// review.
//
// CheckSuffix is **case-sensitive**, matching tyrquake's strcmp-based
// implementation; do not rely on it for "*.PAK" vs "*.pak" matching.
package qpath
