// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package keys holds the keysym constants tyrquake exposes to the
// engine via include/keys.h. The actual console editor + bind table
// (common/keys.c, ~983 LoC) is a follow-up port -- it touches the
// console rendering + cmd registry surface that we plug in once
// the client comes online.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// What's exposed here:
//
//   - Key numeric IDs: KUnknown, KBackspace ... KMouse1..8, KJoy1..4,
//     KAux1..32, KF1..15, KKp0..9, KP_Period etc. Mirrors the
//     knum_t enum value-for-value so demo replay's bind-state
//     stays byte-equal to upstream.
//
//   - Dest is the keydest_t enum (Game / Console / Message / Menu /
//     None) the engine uses to route key events to the right
//     subsystem.
//
//   - Name(k) returns a human-readable single-token name suitable
//     for `bind` command output (the upstream's Key_KeynumToString),
//     and KeyForName parses it back (Key_StringToKeynum).
//
// ASCII keys (32..126) are mapped to their literal character names
// in lowercase form -- pressing the 'A' key on a US keyboard yields
// K_a = 97 regardless of shift state; the modifier is OR'd into the
// caller's event separately. This matches the upstream's "skip
// uppercase, alpha keys passed as lowercase" comment.
package keys
