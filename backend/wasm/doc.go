// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package wasm is a Backend implementation that drives the engine
// inside a browser via GOOS=js GOARCH=wasm. The four backend.Backend
// surfaces map to standard web APIs:
//
//   - backend.Display.PresentFrame → HTMLCanvasElement + 2D context
//     (CanvasRenderingContext2D.putImageData with a pre-allocated
//     ImageData; pixels are RGBA, no swizzle needed).
//   - backend.Input.PollInput → DOM events (keydown/keyup +
//     mousemove/mousedown/mouseup) registered ONCE at construction.
//     Mouse-look uses the Pointer Lock API: clicking the canvas
//     requests pointer lock, after which mousemove deltas are
//     accumulated as relative motion.
//   - backend.Audio.QueueAudio → WebAudio AudioContext. Each
//     QueueAudio call resamples the engine's 11025 Hz stereo mix to
//     AudioContext.sampleRate (nearest-neighbor) and schedules an
//     AudioBufferSourceNode at the running playhead.
//   - backend.Clock.Now → performance.now() / 1000.
//
// Design choices:
//
//   - The backend is split into three sub-device interfaces (similar
//     to backend/virtio) so the JS-side adapters can be plugged in at
//     construction time. The interfaces live in pure Go and are 100%
//     testable on host platforms; the JS-bound implementations live
//     behind a //go:build js && wasm tag so the package itself
//     compiles for host too (constructors return ErrUnsupportedHost).
//   - JS-side objects (canvas, 2D context, ImageData, Uint8ClampedArray
//     window into the pixel buffer, AudioContext) are pre-allocated
//     ONCE at construction. PresentFrame's only per-frame JS round-
//     trips are js.CopyBytesToJS into the ImageData's data array +
//     ctx.putImageData(imageData, 0, 0). No per-frame allocations.
//   - QueueAudio creates one AudioBuffer per call (unavoidable: the
//     WebAudio API has no in-place ring buffer for non-worklet code).
//     Source nodes are scheduled monotonically; the next start time
//     is clamped to currentTime to avoid drift if the engine ever
//     stalls.
//
// tyrquake: the role of vid_glx.c / snd_alsa.c / in_x11.c collapsed
// onto the abstract backend.Backend surface.
package wasm
