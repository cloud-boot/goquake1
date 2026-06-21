// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package runloop is the per-frame orchestrator that glues the engine's
// pieces together: the server-side host tick, the client-side tick that
// drains inbound network traffic and emits clc_move, the 2D frame
// composer, the audio mixer, and the platform backend that ultimately
// displays the frame + queues audio + collects input.
//
// tyrquake: this is the Go port's Host_Frame -- the loop body in
// NQ/host.c that calls IN_Frame, SV_Frame, CL_Frame, SCR_UpdateScreen,
// S_Update, in that order, every tic.
//
// The wiring is intentionally a single [Runner.RunFrame] entrypoint so
// every backend (SDL, Wayland, DirectFB, TamaGo display drivers, the
// headless [github.com/go-quake1/engine/backend.Recorder] used by
// tests) calls one function per tic and the orchestration logic stays
// in one place.
//
// Sub-interfaces ([HostFramer]) decouple the runner from the heavy
// per-package types so tests can drive RunFrame with a fake Host that
// has no VM / World / Progs setup; production code still passes the
// real *host.Host since it satisfies HostFramer.
package runloop
