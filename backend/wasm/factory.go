// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasm

// NewDefault constructs a Backend wired to the standard browser
// adapters:
//
//   - CanvasFramebuffer over document.querySelector(canvasSelector)
//     sized at width x height (typically 320 x 240 to match the
//     engine framebuffer).
//   - DOMInput on the matched canvas + window.
//   - WebAudio on a freshly-constructed AudioContext (will need the
//     caller to resume() on first user gesture).
//   - PerformanceNow clock.
//
// Any subdevice failure short-circuits + returns the error; the
// partially-constructed adapters are dropped (the Go GC reclaims the
// retained js.Func handles since no JS-side reference is yet wired).
//
// Pass "#quake" for the typical "<canvas id=\"quake\">" markup.
func NewDefault(canvasSelector string, width, height int) (*Backend, error) {
	fb, err := NewCanvasFramebuffer(canvasSelector, width, height)
	if err != nil {
		return nil, err
	}
	in := NewDOMInput(fb.Canvas())
	au, err := NewWebAudio()
	if err != nil {
		// Audio is optional; degrade gracefully so the visual loop
		// still runs even if the AudioContext constructor is missing.
		return New(fb, in, nil, PerformanceNow())
	}
	return New(fb, in, au, PerformanceNow())
}
