// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package qargs is the Go port of tyrquake's COM_InitArgv /
// COM_CheckParm / COM_Argv / COM_Argc / COM_AddParm registry from
// common/common.c lines 979-1230.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// The engine consults this registry for boot-time configuration
// switches (-game, -basedir, -hipnotic, -safe, -nosound, -window,
// etc.). The C global state (com_argc / com_argv / safe-mode
// substitution) becomes a per-instance Registry value so test
// fixtures and the bare-metal probe can spin up isolated arg sets
// without mutating package state.
//
// The "-safe" sentinel preserved verbatim: when -safe appears in the
// raw argv, the registry appends safe-mode replacements
// (-nojoy -nomouse -nostdout -nosound -nocdaudio) to argv after
// the initial copy, matching tyrquake's behaviour. The substitution
// is documented in CheckParm's per-test fixture.
package qargs
