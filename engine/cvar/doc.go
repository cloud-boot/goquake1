// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package cvar is the Go port of tyrquake's common/cvar.c +
// include/cvar.h, providing the engine's console-variable registry:
// name-keyed lookup, string/float parsing, change callbacks, and
// archival write-out of flagged variables.
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15 (Q-1a kickoff)
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//   - C cvar_t -> Var. The upstream `flags` bitmask is preserved as a
//     uint32 field (constants Config / Video / Developer / Obsolete)
//     rather than split into bools, so callers can OR flags exactly
//     like the C declarations (`{Name:"foo", String:"1", Flags: Config}`).
//   - C `qboolean server` (NQ_HACK) and `qboolean info` (QW_HACK)
//     stay as discrete bool fields since they're conditional in the C
//     and travel independently of the flags bitmask.
//   - The global `cvar_tree` STree becomes the Registry's per-instance
//     map -- no package-level mutable state, so multiple engines can
//     coexist in one process (useful for tests and for tools that
//     diff two configs).
//   - C cvar_callback (function pointer on the cvar) -> Var.Callback
//     (a `Callback` function value). Called only when Set produces a
//     value change, matching upstream.
//   - tyrquake's Cvar_RegisterVariable temporarily forces developer
//     mode on to let the initial Set bypass the developer-only guard.
//     The Go port mirrors this with Registry.developerOverride.
//   - "Set before Register" semantics: Cvar_Set on an unregistered
//     name in C prints an error and discards the value. tyrquake's
//     RegisterVariable does NOT honour a prior Set; the value comes
//     from the literal `string` field on the struct. We preserve that
//     behaviour but additionally expose Registry.SetPending so the
//     config-file loader (parsed before the cvars are declared by the
//     subsystems) can stash a value that RegisterVariable will pick
//     up at registration time.
//   - Cmd_Exists name-collision check, Con_Printf logging, Z_Malloc
//     allocation, SV_BroadcastPrintf server notification, and
//     Info_SetValueForKey integration are deliberately omitted -- they
//     belong to packages this port does not yet have. The Set logic
//     records the change and fires the callback; downstream packages
//     can wrap Registry to attach the broadcast/info hooks once they
//     land.
//   - Argument-completion (Cvar_ArgCompletions / Cvar_ArgComplete)
//     plus Cvar_NextServerVar are not ported here: they depend on the
//     stree machinery that lives in shell.c, and they have no callers
//     inside cvar.c itself. They will land with the shell port.
//   - WriteVariables takes io.Writer rather than *os.File so the vfs
//     layer (engine/vfs) can wire it to whatever backing store the
//     host provides.
package cvar
