// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package host is the per-process game-loop coordinator that wires
// the Server + Static + VM + World + per-frame timing together. It
// is the Go port's analogue of NQ/host.c -- the upstream file that
// owns Host_Frame, Host_InitLocal, Host_Cmd's "map" handler, and the
// global host_* timing variables.
//
// The Host type bundles every per-process subsystem the per-tic loop
// touches (so each of them stays decoupled from each other -- only
// the Host knows about them all). Its three public entry points
// mirror the upstream's three load-bearing host hooks:
//
//   - [Host.Frame] -- per-tic SV_Physics + SendClientFrames +
//     CleanupEnts + ClearDatagram (the host_frame loop body).
//   - [Host.SpawnServer] -- the Host_Cmd "map" handler's
//     SV_SpawnServer call, with the host's pre-built
//     [server.SpawnDeps] supplied verbatim.
//   - [Host.ConnectLoopback] -- the implicit local client the
//     upstream's Host_InitLocal hooks up before the first
//     PR_ExecuteProgram dispatch.
//
// The package also installs the QuakeC bridge -- a [server.ThinkCaller]
// that translates a funcID into a [progs.VM.Run] invocation, setting
// the QC self / other / time globals first (when the loaded progs.dat
// declares them). The bridge is a method on [Host] so it has access
// to every subsystem the per-think dispatch needs, without leaking
// the wiring into either the server or the progs package.
package host
