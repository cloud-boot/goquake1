// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ppm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/sound"
)

// Sentinel errors returned by the package.
var (
	ErrPPMNilWriter   = errors.New("ppm: nil writer factory")
	ErrPPMWriteFailed = errors.New("ppm: PresentFrame writer failed")
	ErrPPMRGBASize    = errors.New("ppm: rgba buffer size doesn't match width*height*4")
)

// WriterFactory is called once per PresentFrame to obtain the
// io.WriteCloser that receives the next frame's PPM bytes. Caller
// supplies it so the backend doesn't dictate file naming or
// container format (a test can supply an in-memory buffer factory;
// production code supplies a numbered-file factory).
//
// frameIdx is the 0-indexed frame counter (incremented after each
// successful Write+Close).
type WriterFactory func(frameIdx int) (io.WriteCloser, error)

// Backend is the PPM-file-writer Backend implementation.
// Constructor takes the framebuffer dimensions + a WriterFactory.
// Audio is captured into an in-memory slice (so tests can inspect).
// Input always returns empty (the PPM backend doesn't read input).
// Now returns a monotonic integer-tick clock (TickIncrement seconds
// per call), so the engine's dt is deterministic frame-to-frame.
type Backend struct {
	Width, Height int
	NewWriter     WriterFactory

	// Captured audio: each QueueAudio appends a copy here.
	Audio [][]sound.StereoSample

	// Monotonic tick clock state.
	tick int

	// PresentFrame counter (matches the frameIdx passed to NewWriter).
	framesWritten int
}

// TickIncrement is the per-Now() time delta (seconds). 1/60 = ~60Hz.
const TickIncrement = 1.0 / 60.0

// sampleRate is the canonical default sample rate the backend advertises.
const sampleRate = 22050

// Compile-time check: *Backend satisfies the full Backend contract.
var _ backend.Backend = (*Backend)(nil)

// New returns a fresh Backend.
func New(width, height int, newWriter WriterFactory) (*Backend, error) {
	if newWriter == nil {
		return nil, ErrPPMNilWriter
	}
	return &Backend{
		Width:     width,
		Height:    height,
		NewWriter: newWriter,
	}, nil
}

// PresentFrame writes one PPM file via NewWriter(framesWritten);
// strips the A channel from the input RGBA buffer.
// Returns ErrPPMRGBASize / ErrPPMWriteFailed on size / IO errors.
func (b *Backend) PresentFrame(rgba []byte, width, height int) error {
	if len(rgba) != width*height*4 {
		return ErrPPMRGBASize
	}
	w, err := b.NewWriter(b.framesWritten)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPPMWriteFailed, err)
	}
	header := fmt.Sprintf("P6\n%d %d\n255\n", width, height)
	if _, err := w.Write([]byte(header)); err != nil {
		_ = w.Close()
		return fmt.Errorf("%w: %w", ErrPPMWriteFailed, err)
	}
	// Strip alpha: build a single RGB-only buffer + one Write call.
	body := make([]byte, width*height*3)
	for i, j := 0, 0; i < len(rgba); i, j = i+4, j+3 {
		body[j] = rgba[i]
		body[j+1] = rgba[i+1]
		body[j+2] = rgba[i+2]
	}
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return fmt.Errorf("%w: %w", ErrPPMWriteFailed, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrPPMWriteFailed, err)
	}
	b.framesWritten++
	return nil
}

// Size returns Width / Height.
func (b *Backend) Size() (int, int) { return b.Width, b.Height }

// QueueAudio captures the input samples into b.Audio.
func (b *Backend) QueueAudio(samples []sound.StereoSample) error {
	cp := make([]sound.StereoSample, len(samples))
	copy(cp, samples)
	b.Audio = append(b.Audio, cp)
	return nil
}

// SampleRate returns 22050.
func (b *Backend) SampleRate() int { return sampleRate }

// PollInput returns an empty snapshot (no input source).
func (b *Backend) PollInput() (backend.InputSnapshot, error) {
	return backend.InputSnapshot{}, nil
}

// Now returns the monotonic clock: each call advances by TickIncrement
// seconds.
func (b *Backend) Now() float64 {
	t := float64(b.tick) * TickIncrement
	b.tick++
	return t
}

// FramesWritten returns the count of successful PresentFrame calls.
func (b *Backend) FramesWritten() int { return b.framesWritten }

// ResetClock zeroes the Now() counter (useful between test scenarios).
func (b *Backend) ResetClock() { b.tick = 0 }

// NumberedFileFactory is a convenience WriterFactory that opens
// "<prefix><N>.<ext>" with the frame index zero-padded to `digits`
// (e.g. NumberedFileFactory("/tmp/frame_", "ppm", 4) produces
// "/tmp/frame_0000.ppm", "/tmp/frame_0001.ppm", ...). The parent
// directory of prefix must already exist; os.Create handles the file
// itself.
func NumberedFileFactory(prefix, ext string, digits int) WriterFactory {
	format := fmt.Sprintf("%%s%%0%dd.%%s", digits)
	return func(frameIdx int) (io.WriteCloser, error) {
		name := fmt.Sprintf(format, prefix, frameIdx, ext)
		// filepath.Clean keeps the result well-formed across OSes
		// even if prefix contains awkward separators.
		return os.Create(filepath.Clean(name))
	}
}
