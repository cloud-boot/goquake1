// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package protocol holds the NetQuake wire-protocol constants from
// tyrquake NQ/protocol.h. Every value the server and client trade
// over UDP (or the loop-back demo stream) has a fixed numeric
// identity defined here: svc_* / clc_* message tags, U_* /
// SU_* update bit flags, SND_* sound flags, TE_* temp-entity event
// codes, ENTALPHA_* alpha encoding constants, protocol version
// numbers (NQ 15, FITZ 666, BJP 10000/10001/10002).
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// Scope: only the NQ subset. QuakeWorld (clc_move with usercmd_t
// deltas, full svc_qw_* set) is QW-only and out of scope for the
// Phase Q-1a single-player target.
//
// Helper functions Known + MaxModels + MaxSounds + MaxSoundsStatic
// + MaxSoundsDynamic translate the protocol version to per-version
// limits (the FITZ + BJP extensions raise the model + sound index
// width from 8 to 16 bits). The upstream defines them as inline
// functions in protocol.h; we mirror them so the eventual sv/cl
// packet readers can stay agnostic of which protocol version they
// landed on.
//
// The ENTALPHA_* encoding is preserved verbatim because demo
// replay parity requires the exact roundf + qclamp bit shape
// the upstream uses on the wire.
package protocol
