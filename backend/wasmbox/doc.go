// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package wasmbox is a Backend implementation that drives the engine as a
// windowed external client of the wasmbox compositor (see
// github.com/wasmdesk/wasmbox/docs/protocol.md, "step B"). The engine wasm
// runs inside a dedicated Web Worker; the wasmbox compositor (on the main
// thread) owns the canvas, the stacking order, focus + input routing.
//
// Differences vs. backend/wasm:
//
//   - Surface is NOT a DOM <canvas>. It's a SharedArrayBuffer allocated on
//     the worker side (4 * w * h bytes, RGBA32, row-major). The compositor
//     reads the SAB on each commit + blits the damage rectangle onto the
//     desktop canvas.
//   - Frame submission is a postMessage({type:"commit", damage}) plus a
//     prior js.CopyBytesToJS into the SAB-backed Uint8ClampedArray view.
//   - Input arrives as postMessage({type:"input", event}) from the
//     compositor (forwarded only when this client window has focus).
//     Decoration hits (titlebar drag, close box) never reach the client.
//   - There is no DOM keyboard/mouse listener: events are routed to us by
//     the compositor through the wire protocol.
//   - There is no canvas-side pointer-lock dance: mouse deltas are derived
//     from successive surface-local x,y values (the compositor's coordinate
//     translation already accounts for window position).
//
// Audio + clock match backend/wasm exactly (Worker AudioContext +
// performance.now()).
//
// Design choices:
//
//   - The protocol is fully testable on host platforms: the JS-touching
//     code lives behind //go:build js && wasm, while the message-decode +
//     mouse-delta + key-map helpers are pure Go and exercised by host
//     tests at 100% coverage.
//   - One SharedArrayBuffer + one Uint8ClampedArray view are allocated
//     ONCE at construction; PresentFrame's per-frame cost is one
//     js.CopyBytesToJS + one postMessage.
//   - The "hello" handshake waits synchronously (channel) for the
//     compositor's "welcome" reply before returning the constructed
//     Backend, so the caller never has to deal with a pre-welcome
//     half-state.
//
// tyrquake parallel: backend/wasm collapsed vid_glx.c / snd_alsa.c /
// in_x11.c onto one Backend; backend/wasmbox collapses the same roles
// onto the wasmbox step-B wire protocol instead of direct DOM access.
package wasmbox
