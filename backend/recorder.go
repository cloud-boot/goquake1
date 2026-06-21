// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backend

import "github.com/go-quake1/engine/sound"

// Recorder is a test backend that captures every PresentFrame +
// QueueAudio call into in-memory slices so the test can assert on
// what the engine produced. Implements the full Backend interface
// with NoInput-style empty polling.
//
// Usage:
//
//	rec := backend.NewRecorder(320, 200)
//	host.RunFrame(rec, ...)
//	if len(rec.Frames) != 1 { t.Fatal(...) }
//	if rec.Frames[0][0] != expectedR { t.Fatal(...) }
//
// The Recorder is the canonical headless test harness for the
// autonomous-visual-verification protocol the project enforces
// (programmatic frame capture, never "looks fine to me").
type Recorder struct {
	Width, Height int
	Frames        [][]byte                // each: copy of one rgba presented
	Audio         [][]sound.StereoSample  // each: copy of one queued slice
	NowVal        float64                 // returned by Now(); test sets it explicitly
	Input         InputSnapshot           // returned by PollInput on each call
	PollCount     int
}

// NewRecorder returns a Recorder with the given preferred size.
// Frames + Audio start nil; capacity grows on demand.
func NewRecorder(width, height int) *Recorder {
	return &Recorder{Width: width, Height: height}
}

// PresentFrame copies the rgba buffer (the engine may reuse it) and
// stores the copy in Frames.
func (r *Recorder) PresentFrame(rgba []byte, _, _ int) error {
	cp := make([]byte, len(rgba))
	copy(cp, rgba)
	r.Frames = append(r.Frames, cp)
	return nil
}

// Size returns the configured framebuffer dimensions.
func (r *Recorder) Size() (int, int) { return r.Width, r.Height }

// QueueAudio copies the samples slice and appends the copy.
func (r *Recorder) QueueAudio(samples []sound.StereoSample) error {
	cp := make([]sound.StereoSample, len(samples))
	copy(cp, samples)
	r.Audio = append(r.Audio, cp)
	return nil
}

// SampleRate returns 22050 (a sensible default; tests don't usually
// care since they don't actually decode audio).
func (r *Recorder) SampleRate() int { return 22050 }

// PollInput returns the pre-set Input snapshot + bumps PollCount.
// Tests set Input field before invoking the host loop.
func (r *Recorder) PollInput() (InputSnapshot, error) {
	r.PollCount++
	return r.Input, nil
}

// Now returns NowVal (set by test before each tick).
func (r *Recorder) Now() float64 { return r.NowVal }

// Reset clears the captured Frames + Audio + PollCount but preserves
// Width / Height / NowVal / Input (the test's configured baseline).
func (r *Recorder) Reset() {
	r.Frames = nil
	r.Audio = nil
	r.PollCount = 0
}
