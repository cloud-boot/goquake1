// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasmbox

import (
	"syscall/js"

	"github.com/go-quake1/engine/sound"
)

// engineMixerRate is the sample rate of the engine's internal mixer
// output (sound.Paint emits stereo frames at this rate). Matches
// sound.DefaultSampleRate (11025 Hz).
const engineMixerRate = 11025

// WebAudio is an AudioDevice that streams the engine's mixer output
// through a per-Worker WebAudio AudioContext. Same algorithm as
// backend/wasm.WebAudio: resample to ctx.sampleRate (nearest-neighbor),
// allocate a fresh AudioBuffer per WritePCM, schedule it on
// AudioBufferSourceNode.start(playhead), advance the playhead.
//
// AudioContext is supported in Web Workers in current Chromium
// (chrome 105+). The Worker has no user-gesture handshake of its own;
// the compositor (main thread) gates the gesture, but the resume()
// state propagates to AudioContexts spawned from any context once the
// page is unmuted. If the constructor isn't available we return
// ErrAudioNoContext and the Backend degrades to silent (NewClient
// swallows the error).
type WebAudio struct {
	ctx        js.Value
	sampleRate int
	playhead   float64
}

// NewWebAudio constructs a WebAudio sink. Returns ErrAudioNoContext if
// the worker scope does not expose AudioContext or webkitAudioContext.
func NewWebAudio() (*WebAudio, error) {
	ctxCtor := js.Global().Get("AudioContext")
	if !ctxCtor.Truthy() {
		ctxCtor = js.Global().Get("webkitAudioContext")
	}
	if !ctxCtor.Truthy() {
		return nil, ErrAudioNoContext
	}
	ctx := ctxCtor.New()
	rate := ctx.Get("sampleRate").Int()
	return &WebAudio{
		ctx:        ctx,
		sampleRate: rate,
	}, nil
}

// WritePCM resamples + schedules one chunk of stereo PCM. Empty inputs
// are a no-op.
func (a *WebAudio) WritePCM(samples []sound.StereoSample) error {
	if len(samples) == 0 {
		return nil
	}
	left, right := ResampleNearest(samples, engineMixerRate, a.sampleRate)
	if len(left) == 0 {
		return nil
	}
	buf := a.ctx.Call("createBuffer", 2, len(left), a.sampleRate)
	jsLeft := makeFloat32Array(left)
	jsRight := makeFloat32Array(right)
	buf.Call("copyToChannel", jsLeft, 0)
	buf.Call("copyToChannel", jsRight, 1)
	src := a.ctx.Call("createBufferSource")
	src.Set("buffer", buf)
	src.Call("connect", a.ctx.Get("destination"))
	currentTime := a.ctx.Get("currentTime").Float()
	if a.playhead < currentTime {
		a.playhead = currentTime
	}
	src.Call("start", a.playhead)
	a.playhead += float64(len(left)) / float64(a.sampleRate)
	return nil
}

// SampleRate returns the AudioContext's negotiated output rate.
func (a *WebAudio) SampleRate() int { return a.sampleRate }

// makeFloat32Array builds a JS Float32Array carrying the supplied
// samples. Reinterprets the float32 backing memory as bytes (wasm is
// little-endian + matches the Float32Array spec) and copies them into
// a Uint8Array, then wraps a Float32Array over the same ArrayBuffer.
func makeFloat32Array(samples []float32) js.Value {
	bytesLen := len(samples) * 4
	u8 := js.Global().Get("Uint8Array").New(bytesLen)
	bytes := float32SliceAsBytes(samples)
	js.CopyBytesToJS(u8, bytes)
	return js.Global().Get("Float32Array").New(u8.Get("buffer"), u8.Get("byteOffset"), len(samples))
}
