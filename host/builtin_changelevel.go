// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"github.com/go-quake1/engine/progs"
)

// BuiltinChangeLevelIdx is the pr_builtin[] slot the QC `changelevel`
// function lives at in vanilla Q1 progs.dat. tyrquake:
// pr_cmds.c, pr_builtin[70] = PF_changelevel.
const BuiltinChangeLevelIdx = 70

// BuiltinChangeLevel returns the QC `changelevel(string mapname)`
// builtin closure for vm. tyrquake: PF_changelevel in pr_cmds.c.
//
// The C upstream sets pr_global_struct->nextmap (the named global
// `nextmap`) to the requested map name, increments a "spawn_parms_
// written" guard, and posts a "changelevel <map>\n" stuff-cmd onto
// every client's reliable buffer so the next tic Cbuf_Execute runs
// SV_SpawnServer with the new map. The host-side mechanic is then
// `host_changelevel <map>` -- a console command that calls
// SV_SaveSpawnparms + SV_SpawnServer + every-client-reconnect.
//
// The Go port collapses the multi-step C ritual into:
//
//  1. Write the requested map slug into the QC `nextmap` global (a
//     string field; the value is the same string-table offset the
//     builtin received in OFS_PARM0). Mods that read `nextmap` from
//     QC (most info_intermission setups do) see the value verbatim.
//  2. Flip Host.PendingChangelevel = true + record the map slug in
//     Host.NextMap so the embedder's main loop can poll the
//     transition + drive the re-spawn (SpawnServer + post-spawn
//     wiring). The host itself does NOT re-spawn -- it has no
//     visibility into the per-embedder wiring (sound pool, sound
//     loader, OnArenaReady hook).
//
// nil host is a tolerated no-op (matches the rest of the
// host-package builtin shape; the QC `changelevel` call still
// returns but the embedder gets no flag flip + the re-spawn never
// fires). An OFS_PARM0 of 0 (empty string) is treated as "no
// changelevel requested" -- the flag does not flip + NextMap stays
// at its previous value. This mirrors the C upstream's `if
// (!*s) return` early-out for an empty mapname.
//
// SIMPLIFICATIONS vs the C upstream's PF_changelevel:
//
//   - The "if (svs.changelevel_issued) return" re-entry guard is
//     dropped: PendingChangelevel itself is the equivalent flag --
//     a second changelevel call before the embedder polls + clears
//     PendingChangelevel just overwrites NextMap with the newer
//     request, which is the same observable outcome (the embedder's
//     poll sees only the latest map).
//   - The per-client stuffcmd ("changelevel <map>\n") is not
//     emitted: the Go port has no Cbuf_Execute equivalent + the
//     embedder polls the flag directly, so the round-trip is
//     short-circuited.
//   - SV_SaveSpawnparms (the per-client persisted-stat snapshot the
//     vanilla coop chain carries across the level transition) is
//     deferred: a single-player bring-up doesn't depend on it +
//     the multiplayer save-parms format would pull in client.qc
//     SetChangeParms QC + the per-client parm float array, all of
//     which is a follow-up batch.
func BuiltinChangeLevel(h *Host) progs.Builtin {
	return func(vm *progs.VM) error {
		if h == nil {
			return nil
		}
		off, _ := vm.GlobalInt(progs.OfsParm0)
		if off == 0 {
			return nil
		}
		name := vm.String(off)
		if name == "" {
			return nil
		}
		// Write the QC `nextmap` named global if the progs declares it
		// (every shareware progs.dat does; minimalist test stubs may
		// not). The write is silent-skip on missing global -- matches
		// the rest of the named-global hand-off pattern in this
		// package (see [Host.thinkCaller]).
		if p := h.findProgs(); p != nil {
			if def := p.FindGlobal("nextmap"); def != nil {
				_ = vm.SetGlobalInt(int(def.Ofs), off)
			}
		}
		h.PendingChangelevel = true
		h.NextMap = name
		return nil
	}
}

// ConsumeChangelevel returns (true, mapSlug) on the first call after
// PendingChangelevel was set + clears the flag, or (false, "") when
// no changelevel is pending. The embedder's main loop polls this
// post-[Host.Frame] to drive the re-spawn into the new map:
//
//	if pending, m := h.ConsumeChangelevel(); pending {
//	    // tear down the current server, re-build with `m`, reconnect
//	    // the local loopback client, etc.
//	}
//
// Single-shot semantics: a second call without an intervening
// changelevel returns (false, ""). This is the explicit "I've
// handled it" contract that keeps the per-tic log from emitting a
// level-change line every tic until the next changelevel.
func (h *Host) ConsumeChangelevel() (bool, string) {
	if !h.PendingChangelevel {
		return false, ""
	}
	name := h.NextMap
	h.PendingChangelevel = false
	h.NextMap = ""
	return true, name
}
