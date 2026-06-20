// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

// EdictEffectsCleaner abstracts the per-edict effect-bits clear.
// SV_CleanupEnts iterates every edict + zeroes the renderer-side
// effect flags (SV_MUZZLEFLASH etc.) that the server set for one
// tick of HUD feedback. The Go port takes a callback so server
// stays decoupled from progs's entvars writer.
//
// The callback receives a 0-based edict index + a bitmask of the
// effect bits to clear. Callers (the future progs-runtime glue)
// implement it by setting ent.v.effects &= ^bits.
type EdictEffectsCleaner func(entIdx int, clearMask int32)

// MuzzleFlashMask is the bit the C upstream's SV_CleanupEnts
// clears every tick: EF_MUZZLEFLASH. tyrquake: the only bit the
// upstream's loop touches.
const MuzzleFlashMask = 2 // EF_MUZZLEFLASH

// CleanupEnts iterates every edict in [1, NumEdicts) (skipping the
// world at index 0) and asks cleaner to clear MuzzleFlashMask on
// each. tyrquake: SV_CleanupEnts.
//
// Used at end of frame to make muzzle flashes one-tick events --
// the QC code sets ef_muzzleflash on weapon fire, the renderer
// draws the flash on the next frame, and CleanupEnts clears the
// bit so the flash doesn't persist.
//
// If cleaner is nil this is a no-op (no panic).
func (s *Server) CleanupEnts(cleaner EdictEffectsCleaner) {
	if cleaner == nil {
		return
	}
	for e := 1; e < s.NumEdicts; e++ {
		cleaner(e, MuzzleFlashMask)
	}
}

// SignonSize reports the current signon-buffer occupancy. Used by
// the lifecycle to decide between SV_SendServerinfo (signon < 4)
// and the per-tick datagram path. tyrquake: indirect via
// sv.signon.cursize reads.
func (s *Server) SignonSize() int {
	return s.Signon.Len()
}

// ClearReliableDatagram resets the per-frame reliable buffer.
// tyrquake: called inline by SV_SendClientMessages each frame.
func (s *Server) ClearReliableDatagram() {
	s.ReliableDatagram.Clear()
}

// CopyReliableDatagramTo appends the server's current
// reliable_datagram into the given per-client message buffer.
// tyrquake: SV_SendClientMessages's "if sv.reliable_datagram.cursize"
// block.
//
// Returns the propagated sizebuf write error or nil. Does NOT clear
// sv.reliable_datagram (caller calls ClearReliableDatagram once at
// end-of-frame after all clients have copied).
func (s *Server) CopyReliableDatagramTo(client *Client) error {
	if s.ReliableDatagram.Len() == 0 {
		return nil
	}
	return client.Message.Write(s.ReliableDatagram.Bytes())
}
