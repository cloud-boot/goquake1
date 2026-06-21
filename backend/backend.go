// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backend

import (
	"errors"

	"github.com/go-quake1/engine/sound"
)

// KeyCode identifies a key/button event from the input backend.
// Matches Quake's K_* constants (a subset; expand as backends grow).
// tyrquake: keys.h's K_* enum.
type KeyCode int

const (
	KeyEscape KeyCode = iota + 1
	KeyEnter
	KeySpace
	KeyTab
	KeyW
	KeyA
	KeyS
	KeyD
	KeyMouse1 // left mouse
	KeyMouse2 // right mouse
	KeyShift
	KeyCtrl
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
)

// InputSnapshot is one frame's worth of input events. Backends fill
// this every tic; the host loop hands it to the client tick's
// MovementButtons state.
type InputSnapshot struct {
	KeysDown []KeyCode // keys pressed since last poll
	KeysUp   []KeyCode // keys released since last poll
	MouseDX  float32   // pixels horizontal since last poll
	MouseDY  float32   // pixels vertical since last poll
	QuitRequested bool // window close button / signal
}

// Display is the per-frame video surface contract.
type Display interface {
	// PresentFrame hands one RGBA8 frame to the backend. width *
	// height * 4 must equal len(rgba). The backend may copy or
	// reference the buffer; callers must not mutate rgba between
	// PresentFrame and the next call.
	PresentFrame(rgba []byte, width, height int) error

	// Size returns the backend's preferred framebuffer dimensions.
	// The engine sizes its FrameBuffer accordingly at init time.
	Size() (width, height int)
}

// Audio is the per-frame mixer-output contract.
type Audio interface {
	// QueueAudio hands one frame's mixed stereo PCM to the backend.
	// The backend buffers + streams to the hardware.
	QueueAudio(samples []sound.StereoSample) error

	// SampleRate returns the backend's preferred output sample rate
	// in Hz (typically 22050 or 44100; the engine resamples its
	// 11025 Hz internal mix to this rate via a follow-up batch).
	SampleRate() int
}

// Input is the per-frame button + mouse delta contract.
type Input interface {
	// PollInput consumes any queued input events + returns the
	// snapshot. The implementation may block briefly for events
	// or return immediately with empty deltas; the engine doesn't
	// care which.
	PollInput() (InputSnapshot, error)
}

// Clock returns wall-clock-like seconds. Used for frame timing and
// the per-message MsgTime stamps in client.Apply.
type Clock interface {
	Now() float64
}

// Backend bundles all four contracts. Most platforms implement them
// as one struct; the interfaces are split so a test harness can
// mock just one surface (e.g. a headless framebuffer-only renderer
// that doesn't need audio or input).
type Backend interface {
	Display
	Audio
	Input
	Clock
}

// ErrUnsupported is the canonical sentinel for unimplemented backend
// methods (e.g. a headless test backend that doesn't support audio).
// Callers should errors.Is-check before assuming the backend supports
// every method.
var ErrUnsupported = errors.New("backend: feature unsupported on this platform")

// NoAudio is a stub Audio implementation that drops every queued
// frame. Useful for headless tests where audio output isn't
// available or wanted.
type NoAudio struct{}

func (NoAudio) QueueAudio(_ []sound.StereoSample) error { return nil }
func (NoAudio) SampleRate() int                          { return 22050 }

// NoInput is a stub Input that returns empty snapshots forever.
type NoInput struct{}

func (NoInput) PollInput() (InputSnapshot, error) { return InputSnapshot{}, nil }
