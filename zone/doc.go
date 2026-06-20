// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package zone is the Go port of tyrquake's common/zone.c +
// include/zone.h, providing the engine's three internal allocators:
//
//   - Zone: a free-list allocator used for small, short-lived strings
//     and structures. tyrquake: Z_Malloc / Z_Free.
//   - Hunk: a stack-style arena with independent low/high marks plus a
//     temp-allocation slot at the high end. tyrquake: Hunk_AllocName /
//     Hunk_HighAllocName / Hunk_TempAlloc.
//   - Cache: an LRU cache layered on top of the hunk, used by the
//     renderer and model loader. tyrquake: Cache_Alloc.
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//
//   - The backing store is a single pre-allocated []byte per arena
//     (passed to NewZone / NewHunk), not a malloc() chain. This is
//     critical for TamaGo: it avoids per-frame heap churn that would
//     trigger GC pauses inside the 35 Hz main loop.
//   - Alloc methods return slices of that backing buffer. Freeing is
//     bookkeeping only (the slice's underlying array is never released
//     to the Go runtime; the GC sees one []byte for the lifetime of
//     the arena).
//   - Zone header layout (memblock_t in C) is mirrored as an in-band
//     32-byte block header inside the backing buffer; the ZONEID
//     sentinel (0x1d4a11) and 8-byte alignment quantum are preserved.
//   - Hunk header layout (hunk_t in C) is mirrored as an in-band
//     16-byte header; HUNK_SENTINAL (0x1df001ed) and 16-byte alignment
//     quantum are preserved.
//
// Cache status:
//
//   - The full LRU implementation is deferred. The Cache type ships
//     with the upstream struct shape so future ports of the renderer +
//     model loader compile against it, but the methods panic with a
//     clearly-worded "cache not yet ported" message. They will be
//     filled in when their first consumer (model.c) lands.
package zone
