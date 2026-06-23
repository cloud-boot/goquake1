// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasmbox

import (
	"syscall/js"
)

// SABSurface is a Surface backed by a wasmbox-protocol SharedArrayBuffer
// + the worker-side `self.postMessage` channel to the compositor. The
// Uint8ClampedArray view is allocated ONCE at construction so
// PresentRGBA's per-frame cost is one js.CopyBytesToJS + one
// self.postMessage carrying the {type:"commit", damage} envelope.
type SABSurface struct {
	width, height int
	windowID      int
	pixels        js.Value // Uint8ClampedArray view over the SAB
	postMessage   js.Value // self.postMessage bound function
	self          js.Value // the worker's `self` (target of postMessage)
	objCtor       js.Value // Object constructor for damage rects
	commitMsg     js.Value // reusable {type:"commit", window_id, damage} stub
	damageObj     js.Value // reusable full-frame damage object
}

// NewSABSurface wraps an already-allocated SAB + the worker's self
// global. The caller has typically already negotiated the welcome
// handshake; we accept windowID + the granted width/height as
// parameters so this constructor is decoupled from the handshake.
//
// pixels must be a Uint8ClampedArray view over the SAB. self must be
// the Worker's globalThis (so postMessage targets the main thread).
func NewSABSurface(self js.Value, pixels js.Value, windowID, width, height int) *SABSurface {
	objCtor := js.Global().Get("Object")
	damage := objCtor.New()
	damage.Set("x", 0)
	damage.Set("y", 0)
	damage.Set("w", width)
	damage.Set("h", height)
	commit := objCtor.New()
	commit.Set("type", "commit")
	commit.Set("window_id", windowID)
	commit.Set("damage", damage)
	return &SABSurface{
		width:       width,
		height:      height,
		windowID:    windowID,
		pixels:      pixels,
		self:        self,
		postMessage: self.Get("postMessage"),
		objCtor:     objCtor,
		commitMsg:   commit,
		damageObj:   damage,
	}
}

// PresentRGBA copies one RGBA frame into the SAB-backed Uint8ClampedArray
// + posts a commit covering the full surface. Frame must be exactly
// width*height*4 bytes.
func (s *SABSurface) PresentRGBA(rgba []byte) error {
	if len(rgba) != s.width*s.height*4 {
		return ErrRGBASize
	}
	js.CopyBytesToJS(s.pixels, rgba)
	// postMessage(commitMsg) — call through the bound function so the
	// `this` binding is the worker's globalThis (required by the
	// postMessage WebIDL). The bound function carries its own this.
	s.postMessage.Invoke(s.commitMsg)
	return nil
}

// Width returns the surface width.
func (s *SABSurface) Width() int { return s.width }

// Height returns the surface height.
func (s *SABSurface) Height() int { return s.height }

// WindowID returns the compositor-granted window id.
func (s *SABSurface) WindowID() int { return s.windowID }
