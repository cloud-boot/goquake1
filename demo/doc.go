// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package demo parses and encodes Quake .dem (demo) files -- the
// recorded server-message stream the engine plays back for
// deterministic replay.
//
// A .dem file is the concatenation of:
//
//  1. An ASCII CD-track manifest: 0..11 characters followed by a
//     single '\n' (the upstream reads at most 12 bytes; if the 12th
//     byte is not '\n', the demo is rejected as invalid). The
//     manifest is a hardware-CD-audio relic; this parser surfaces it
//     as a string but does not interpret it.
//
//  2. Zero or more per-tic records, each laid out as:
//
//     int32  msglen      -- little-endian server-message body length
//     float32 angles[3]  -- pitch / yaw / roll the client recorded
//     byte   body[msglen]-- raw svc_* messages, identical to what
//     would have travelled over a NetConn
//
// The tic stream ends at clean EOF. A msglen > [MaxDemoMessageLen]
// or a negative msglen is corrupt-demo data. A partial trailing tic
// is reported as [ErrDemoShortRead].
//
// Upstream pin (verbatim wire format):
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//	NQ/cl_demo.c: CL_PlayDemo_f (header), CL_GetMessage (per-tic
//	read), CL_WriteDemoMessage (per-tic write).
package demo
