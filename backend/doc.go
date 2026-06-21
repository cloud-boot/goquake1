// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package backend defines the integration contract between the
// platform-agnostic engine (server stack, client stack, renderer,
// sound) and a platform-specific backend (SDL, Tart VM display +
// audio, UEFI framebuffer, headless test harness).
//
// The contract has four surfaces:
//
//   - Display:  PresentFrame(rgba []byte, width, height int) error
//   - Audio:    QueueAudio(samples []sound.StereoSample) error
//   - Input:    PollInput() (InputSnapshot, error)
//   - Clock:    Now() float64 (seconds; monotonic clock)
//
// The host loop calls these once per tic. Backends are free to
// buffer/drop frames if the engine produces them faster than the
// display can scan them out.
//
// tyrquake: the role of vid_* / snd_* / in_* / sys_* modules in
// the C tree, collapsed to one interface to keep the
// platform-independent half blissfully ignorant of every backend's
// quirks.
package backend
