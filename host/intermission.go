// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/server"
)

// EmitIntermission writes a svc_intermission marker into the
// server's [server.Server.ReliableDatagram] buffer. The next per-tic
// SV_SendClientMessages flush mirrors the datagram into every
// active client's per-frame message buffer, so every connected
// client lands the intermission marker exactly once per call.
//
// Also pushes a fresh svc_updatestat for each of the four
// intermission-scoreboard slots (StatSecrets / StatTotalSecrets /
// StatMonsters / StatTotalMonsters) sourced from
// [Host.LastIntermissionStats]. Without the per-slot push the
// client's stat bank carries whatever the last svc_clientdata
// emitted (which may pre-date the secrets/monsters tally), so the
// scoreboard renders stale numbers.
//
// nil host = silent no-op (matches the rest of the host-package
// builtin shape). Server.ReliableDatagram nil = silent no-op
// (production NewHost wires it via [server.NewServer]; only test
// stubs hit this branch).
//
// tyrquake: the SV_BroadcastPrintf("svc_intermission") inside
// SV_SpawnServer / Host_FindMaxClients during a changelevel + the
// PF_Intermission QC builtin's
// MSG_WriteByte(svc_intermission) into sv.reliable_datagram.
//
// Returns the first error from any underlying encoder write
// (typically [sizebuf.ErrSizeBufOverflow] if the reliable datagram
// is over capacity).
func (h *Host) EmitIntermission() error {
	if h == nil || h.Server == nil || h.Server.ReliableDatagram == nil {
		return nil
	}
	stats := h.LastIntermissionStats
	if err := server.EncodeUpdateStat(h.Server.ReliableDatagram, protocol.StatSecrets, stats.FoundSecrets); err != nil {
		return err
	}
	if err := server.EncodeUpdateStat(h.Server.ReliableDatagram, protocol.StatTotalSecrets, stats.TotalSecrets); err != nil {
		return err
	}
	if err := server.EncodeUpdateStat(h.Server.ReliableDatagram, protocol.StatMonsters, stats.KilledMonsters); err != nil {
		return err
	}
	if err := server.EncodeUpdateStat(h.Server.ReliableDatagram, protocol.StatTotalMonsters, stats.TotalMonsters); err != nil {
		return err
	}
	return server.EncodeIntermission(h.Server.ReliableDatagram)
}

// HarvestIntermissionStats refreshes [Host.LastIntermissionStats]
// from the QC named globals `total_secrets` / `found_secrets` /
// `total_monsters` / `killed_monsters`. Missing globals silently
// keep their existing zero (test stubs with a stripped progs may
// not declare them; production Q1 progs.dat always does).
//
// Idempotent: callers may invoke per-frame or only on the
// changelevel boundary; the cost is four FindGlobal lookups + four
// GlobalFloat reads. nil host = no-op.
func (h *Host) HarvestIntermissionStats() {
	if h == nil {
		return
	}
	p := h.findProgs()
	if p == nil {
		return
	}
	if def := p.FindGlobal("total_secrets"); def != nil {
		if v, err := h.VM.GlobalFloat(int(def.Ofs)); err == nil {
			h.LastIntermissionStats.TotalSecrets = int32(v)
		}
	}
	if def := p.FindGlobal("found_secrets"); def != nil {
		if v, err := h.VM.GlobalFloat(int(def.Ofs)); err == nil {
			h.LastIntermissionStats.FoundSecrets = int32(v)
		}
	}
	if def := p.FindGlobal("total_monsters"); def != nil {
		if v, err := h.VM.GlobalFloat(int(def.Ofs)); err == nil {
			h.LastIntermissionStats.TotalMonsters = int32(v)
		}
	}
	if def := p.FindGlobal("killed_monsters"); def != nil {
		if v, err := h.VM.GlobalFloat(int(def.Ofs)); err == nil {
			h.LastIntermissionStats.KilledMonsters = int32(v)
		}
	}
}

// IntermissionStats is the per-map progression tally the
// intermission scoreboard reads off the cumulative server state.
// Populated by the embedder (typically from named-global QC reads
// of `total_secrets` / `found_secrets` / `total_monsters` /
// `killed_monsters` against [Host.findProgs] after the map's spawn
// pass + the per-frame increments inside the QC `.touch` of
// trigger_secret + the monster `.death` callbacks). The Go port
// caches them on the host so [Host.EmitIntermission] can push them
// onto the wire without re-walking the QC globals.
//
// tyrquake: pr_global_struct->total_secrets / found_secrets /
// total_monsters / killed_monsters -- the QC named globals
// SV_WriteClientdataToMessage harvests for STAT_TOTALSECRETS /
// STAT_SECRETS / STAT_TOTALMONSTERS / STAT_MONSTERS each frame.
type IntermissionStats struct {
	TotalSecrets   int32
	FoundSecrets   int32
	TotalMonsters  int32
	KilledMonsters int32
}
