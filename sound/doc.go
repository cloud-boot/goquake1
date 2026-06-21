// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sound is the engine's audio mixer + channel pool. It owns:
//
//   - the sample format constants (8-bit signed PCM, default 11025 Hz)
//   - the per-sample buffer wrapper (Sample)
//   - the per-channel mixer state (Channel)
//   - the fixed-cap channel pool (Pool) with priority-based allocation
//   - the per-tic mix accumulator (Mix) -- accumulates active channels
//     into a 16-bit signed output buffer, then clamps to 8-bit
//
// This package owns only the simulation half: no PCM playback, no
// I/O. Backends (SDL audio, Tart VM audio, UEFI buzzer) consume the
// mix output buffer as their per-frame audio frame.
//
// tyrquake: snd_dma.c (channel + pool) + snd_mix.c (mix loop) +
// snd_mem.c (sample loader, deferred to a later batch).
package sound
