// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"fmt"
	"strconv"

	"github.com/go-quake1/engine/cmd"
	"github.com/go-quake1/engine/cvar"
	"github.com/go-quake1/engine/protocol"
)

// DefaultProtocol is the protocol version SV_SpawnServer hands to
// the per-map state. Modifiable via the "svprotocol" command.
// tyrquake: int sv_protocol = PROTOCOL_VERSION_NQ (NQ/sv_main.c:62
// upstream pins it to FITZ; the Go port keeps the NQ default
// because the rest of the server layer is still NQ-shaped).
var DefaultProtocol = protocol.VersionNQ

// AddCommands registers the engine-side console commands the server
// layer owns into the supplied cmd.Registry. tyrquake: SV_AddCommands
// (NQ/sv_main.c:132-139).
//
// Currently registers:
//
//	svprotocol  -- print or set the DefaultProtocol variable
//	               (no args = print, one int arg = set).
//
// The cmd.Registry API silently no-ops on a duplicate Add (see
// engine/cmd.Registry.Add), so we gate each registration on Exists
// and synthesise an error from the second call. This mirrors the
// RegisterServerCvars pattern: returns the first error from
// registry registration; subsequent commands are still attempted
// (best-effort).
//
// SvProtocolCmd is bound with the registry's [cvar.Registry.Printf]-style
// signature wrapped through a closure that calls fmt.Sprintf and
// drops the formatted line into the console -- but at this layer
// we don't have a console hook yet, so the bound handler discards
// its printf output. Tests exercise SvProtocolCmd directly with
// a capture closure.
func AddCommands(registry *cmd.Registry) error {
	var first error
	register := func(name string, fn cmd.Handler) {
		if registry.Exists(name) {
			if first == nil {
				first = fmt.Errorf("cmd: %s is already registered", name)
			}
			return
		}
		registry.Add(name, fn)
	}
	register("svprotocol", func(args []string) {
		// No console-output hook is wired in at this layer; the
		// handler is invoked via the registry's tokenised path,
		// which has no place to surface text. The discarding
		// closure matches the C upstream's Con_Printf path being
		// a process-global indirection -- tests that care about
		// the output call SvProtocolCmd directly with a capture.
		SvProtocolCmd(func(string, ...any) {}, args)
	})
	return first
}

// Init wires up the server layer: registers cvars (delegates to
// RegisterServerCvars), commands (AddCommands), and any one-shot
// table inits the server needs at engine boot. tyrquake: SV_Init
// (NQ/sv_main.c:177-189).
//
// The C upstream's localmodels[MAX_MODELS] table is generated on
// the fly by [LocalModelName] in spawn.go, so no init-time table
// build is needed here -- the function is documented for parity
// and future-proofed if a precomputed table becomes useful.
//
// Returns the first error from cvar / cmd registration.
func Init(cvars *cvar.Registry, cmds *cmd.Registry) error {
	if err := RegisterServerCvars(cvars); err != nil {
		return err
	}
	return AddCommands(cmds)
}

// SvProtocolCmd is the handler for the "svprotocol" console
// command. Exposed for tests and for callers that want to invoke
// it directly. tyrquake: SV_Protocol_f (NQ/sv_main.c:64-110).
//
// args[0] is the command name; args[1:] are the user-typed
// arguments. Reports state via the printf callback (typically
// the Con_Printf-equivalent).
//
// Behavior:
//
//	no args (len(args) == 1)    -> printf("sv_protocol is %d\n", DefaultProtocol)
//	one arg, parses as int X    -> set DefaultProtocol = X;
//	                               printf("sv_protocol set to %d\n", X)
//	anything else               -> printf usage hint; no state change
//
// Does not validate that X is a known protocol version -- the
// upstream's SV_Protocol_f accepts any int and lets the next
// SV_SpawnServer crash on an unknown version. A follow-up could
// gate via [protocol.Known] (no protocol.Versions slice exists in
// the port today).
func SvProtocolCmd(printf func(string, ...any), args []string) {
	switch len(args) {
	case 1:
		printf("sv_protocol is %d\n", DefaultProtocol)
	case 2:
		v, err := strconv.Atoi(args[1])
		if err != nil {
			printf("Usage: svprotocol [<version>]\n")
			return
		}
		DefaultProtocol = v
		printf("sv_protocol set to %d\n", v)
	default:
		printf("Usage: svprotocol [<version>]\n")
	}
}
