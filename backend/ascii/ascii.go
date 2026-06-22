// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ascii

import (
	"errors"
	"fmt"
	"io"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/sound"
)

// Ramp is the per-luminance ASCII character mapping. The first entry
// is darkest (0); the last is brightest (255). 10-step ramp is the
// canonical "from-low-to-high contrast" sequence used in ASCII art.
var Ramp = []byte(" .:-=+*#%@")

// TickIncrement is the per-Now() time delta (seconds). 1/20 mirrors
// tyrquake's default tic rate.
const TickIncrement = 1.0 / 20.0

var (
	ErrASCIINilWriter   = errors.New("ascii: nil writer")
	ErrASCIIBadDim      = errors.New("ascii: cols and rows must be positive")
	ErrASCIIRGBASize    = errors.New("ascii: rgba buffer size doesn't match width*height*4")
	ErrASCIIWriteFailed = errors.New("ascii: PresentFrame writer failed")
)

// Backend is the ASCII-art backend. Wraps an io.Writer + a target
// character grid resolution.
type Backend struct {
	Width, Height int // pixel framebuffer dimensions
	Cols, Rows    int // target ASCII grid
	W             io.Writer

	tick          int
	framesWritten int
}

// New returns a Backend that downsamples width x height pixels into
// cols x rows characters and writes them to w. Returns ErrASCIINilWriter
// if w == nil; ErrASCIIBadDim if any dim is non-positive.
func New(width, height, cols, rows int, w io.Writer) (*Backend, error) {
	if w == nil {
		return nil, ErrASCIINilWriter
	}
	if width <= 0 || height <= 0 || cols <= 0 || rows <= 0 {
		return nil, ErrASCIIBadDim
	}
	return &Backend{
		Width:  width,
		Height: height,
		Cols:   cols,
		Rows:   rows,
		W:      w,
	}, nil
}

// PresentFrame writes the ASCII representation of one RGBA frame:
// for each output character cell, averages the luminance of the
// corresponding source-pixel block + picks the matching Ramp char.
// Frame is terminated with a form-feed + newline so consumers can
// split on FF.
func (b *Backend) PresentFrame(rgba []byte, width, height int) error {
	if len(rgba) != b.Width*b.Height*4 {
		return ErrASCIIRGBASize
	}
	// Per-character cell dimensions in source pixels.
	cellW := b.Width / b.Cols
	cellH := b.Height / b.Rows
	if cellW < 1 {
		cellW = 1
	}
	if cellH < 1 {
		cellH = 1
	}

	line := make([]byte, b.Cols+1) // +1 for trailing newline
	for cy := 0; cy < b.Rows; cy++ {
		for cx := 0; cx < b.Cols; cx++ {
			// Average luminance over the source-pixel block.
			var sum int
			pxCount := 0
			y0 := cy * cellH
			x0 := cx * cellW
			for dy := 0; dy < cellH && y0+dy < b.Height; dy++ {
				for dx := 0; dx < cellW && x0+dx < b.Width; dx++ {
					off := ((y0+dy)*b.Width + (x0 + dx)) * 4
					r := int(rgba[off])
					g := int(rgba[off+1])
					bb := int(rgba[off+2])
					// Standard luminance weights x256 to avoid floats.
					sum += (77*r + 150*g + 29*bb) >> 8
					pxCount++
				}
			}
			rampIdx := 0 // default for empty cell (source region out of bounds)
			if pxCount > 0 {
				// Max possible lum (all-white pixels) = 255; the
				// Ramp clamp at idx == len(Ramp)-1 is unreachable
				// since 255 * len(Ramp) / 256 < len(Ramp).
				lum := sum / pxCount
				rampIdx = lum * len(Ramp) / 256
			}
			line[cx] = Ramp[rampIdx]
		}
		line[b.Cols] = '\n'
		if _, err := b.W.Write(line); err != nil {
			return fmt.Errorf("%w: %w", ErrASCIIWriteFailed, err)
		}
	}
	// Frame separator.
	if _, err := b.W.Write([]byte{'\f', '\n'}); err != nil {
		return fmt.Errorf("%w: %w", ErrASCIIWriteFailed, err)
	}
	b.framesWritten++
	return nil
}

// Size returns Width, Height.
func (b *Backend) Size() (int, int) { return b.Width, b.Height }

// QueueAudio drops the samples (terminal backend has no audio).
func (b *Backend) QueueAudio(_ []sound.StereoSample) error { return nil }

// SampleRate returns 22050 (unused; backend has no audio).
func (b *Backend) SampleRate() int { return 22050 }

// PollInput returns an empty snapshot (terminal backend has no input).
func (b *Backend) PollInput() (backend.InputSnapshot, error) {
	return backend.InputSnapshot{}, nil
}

// Now returns a monotonic tick clock: TickIncrement per call.
func (b *Backend) Now() float64 {
	now := float64(b.tick) * TickIncrement
	b.tick++
	return now
}

// FramesWritten returns the count of successful PresentFrame calls.
func (b *Backend) FramesWritten() int { return b.framesWritten }

// Compile-time check that Backend satisfies the full interface.
var _ backend.Backend = (*Backend)(nil)
