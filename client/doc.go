// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package client holds the Go port of the NQ client layer -- the
// per-frame state the renderer + UI read to draw a frame, the
// server-message decoder that fills that state, and the per-frame
// input collector that feeds usercmds back to the server.
//
// Three sibling files cover the three concerns:
//
//   - state.go   -- client_state_t + cactive_t lifecycle helpers
//     (CL_ClearState / CL_Disconnect / CL_EstablishConnection).
//     The struct the decoder writes INTO and the renderer reads
//     FROM. tyrquake: NQ/client.h + NQ/cl_main.c.
//
//   - decode.go  -- CL_ParseServerMessage + per-svc handlers. Reads
//     a server message off the wire and mutates [State] fields
//     (precaches, light styles, dlights, clientdata, ...).
//     tyrquake: NQ/cl_parse.c.
//
//   - input.go   -- CL_BaseMove + CL_SendCmd. Polls the kbutton_t
//     state, builds a usercmd_t, and hands it to the netcode for
//     delivery to the server. tyrquake: NQ/cl_input.c.
//
// The split mirrors the upstream file boundary; the three files
// are independently testable because [State] is the only piece of
// shared mutable data and it is constructed via [NewState] +
// mutated through explicit methods.
//
// Upstream pin:
//
//	github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
package client
