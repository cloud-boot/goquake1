// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasm

import (
	"fmt"
	"syscall/js"
)

// CanvasFramebuffer is a Framebuffer backed by a HTMLCanvasElement
// + a 2D rendering context. The ImageData and Uint8ClampedArray that
// receives each frame's pixels are pre-allocated ONCE at construction
// so PresentRGBA's per-frame cost is one js.CopyBytesToJS + one
// ctx.putImageData call (plus an ImageData.data property lookup that
// the JS runtime caches).
type CanvasFramebuffer struct {
	width, height int
	canvas        js.Value
	ctx           js.Value
	imageData     js.Value
	pixelArray    js.Value // ImageData.data, a Uint8ClampedArray
}

// NewCanvasFramebuffer looks up document.querySelector(selector),
// resizes the matched <canvas> element to (width, height) so the
// backing store matches the engine's framebuffer, and pre-allocates
// the ImageData + Uint8ClampedArray reused by every PresentRGBA call.
//
// Returns ErrFBSelectorMissing if the selector does not match a DOM
// element, or a wrapped error if the matched element does not expose
// a 2D context.
func NewCanvasFramebuffer(selector string, width, height int) (*CanvasFramebuffer, error) {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil, fmt.Errorf("wasm: document is not available")
	}
	canvas := doc.Call("querySelector", selector)
	if !canvas.Truthy() {
		return nil, fmt.Errorf("%w: %q", ErrFBSelectorMissing, selector)
	}
	canvas.Set("width", width)
	canvas.Set("height", height)
	ctx := canvas.Call("getContext", "2d")
	if !ctx.Truthy() {
		return nil, fmt.Errorf("wasm: canvas %q has no 2D context", selector)
	}
	// Disable smoothing so 320x240 nearest-neighbor scales up crisply
	// when CSS stretches the canvas.
	ctx.Set("imageSmoothingEnabled", false)
	imageData := ctx.Call("createImageData", width, height)
	pixelArray := imageData.Get("data")
	return &CanvasFramebuffer{
		width:      width,
		height:     height,
		canvas:     canvas,
		ctx:        ctx,
		imageData:  imageData,
		pixelArray: pixelArray,
	}, nil
}

// PresentRGBA copies one RGBA frame into the pre-allocated ImageData
// + paints it onto the canvas at (0, 0). Frame must be exactly
// width*height*4 bytes.
func (f *CanvasFramebuffer) PresentRGBA(rgba []byte) error {
	if len(rgba) != f.width*f.height*4 {
		return ErrWASMRGBASize
	}
	js.CopyBytesToJS(f.pixelArray, rgba)
	f.ctx.Call("putImageData", f.imageData, 0, 0)
	return nil
}

// Width returns the canvas width in pixels.
func (f *CanvasFramebuffer) Width() int { return f.width }

// Height returns the canvas height in pixels.
func (f *CanvasFramebuffer) Height() int { return f.height }

// Canvas returns the underlying canvas DOM element. Useful for the
// input adapter, which needs to wire pointer-lock listeners to the
// same canvas the framebuffer paints to.
func (f *CanvasFramebuffer) Canvas() js.Value { return f.canvas }
