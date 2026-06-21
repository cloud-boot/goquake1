// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backend

import (
	"testing"

	"github.com/go-quake1/engine/sound"
)

func TestRecorder_ImplementsBackend(t *testing.T) {
	var _ Backend = NewRecorder(320, 200)
}

func TestRecorder_Size(t *testing.T) {
	r := NewRecorder(640, 480)
	w, h := r.Size()
	if w != 640 || h != 480 {
		t.Fatalf("Size = %dx%d want 640x480", w, h)
	}
}

func TestRecorder_PresentFrame_Copies(t *testing.T) {
	r := NewRecorder(320, 200)
	src := []byte{1, 2, 3, 4}
	if err := r.PresentFrame(src, 1, 1); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	if len(r.Frames) != 1 {
		t.Fatalf("Frames len = %d want 1", len(r.Frames))
	}
	// Mutating src must NOT affect the captured copy.
	src[0] = 0xFF
	if r.Frames[0][0] != 1 {
		t.Fatalf("Recorder didn't copy frame data")
	}
}

func TestRecorder_PresentFrame_MultipleCalls(t *testing.T) {
	r := NewRecorder(320, 200)
	for i := 0; i < 3; i++ {
		_ = r.PresentFrame([]byte{byte(i)}, 1, 1)
	}
	if len(r.Frames) != 3 {
		t.Fatalf("Frames count = %d want 3", len(r.Frames))
	}
	for i := 0; i < 3; i++ {
		if r.Frames[i][0] != byte(i) {
			t.Fatalf("Frames[%d][0] = %d want %d", i, r.Frames[i][0], i)
		}
	}
}

func TestRecorder_QueueAudio(t *testing.T) {
	r := NewRecorder(320, 200)
	samples := []sound.StereoSample{{L: 100, R: -50}, {L: 200, R: -100}}
	if err := r.QueueAudio(samples); err != nil {
		t.Fatalf("QueueAudio: %v", err)
	}
	if len(r.Audio) != 1 || len(r.Audio[0]) != 2 {
		t.Fatalf("Audio shape = %v", r.Audio)
	}
	// Mutate caller's slice; recorder copy unaffected.
	samples[0].L = 9999
	if r.Audio[0][0].L != 100 {
		t.Fatalf("Recorder didn't copy audio data")
	}
}

func TestRecorder_SampleRate(t *testing.T) {
	r := NewRecorder(320, 200)
	if r.SampleRate() != 22050 {
		t.Fatalf("SampleRate = %d want 22050", r.SampleRate())
	}
}

func TestRecorder_PollInput(t *testing.T) {
	r := NewRecorder(320, 200)
	r.Input = InputSnapshot{
		KeysDown: []KeyCode{KeyW, KeySpace},
		MouseDX:  10,
	}
	in, err := r.PollInput()
	if err != nil {
		t.Fatalf("PollInput: %v", err)
	}
	if len(in.KeysDown) != 2 || in.MouseDX != 10 {
		t.Fatalf("PollInput returned wrong snapshot: %+v", in)
	}
	if r.PollCount != 1 {
		t.Fatalf("PollCount = %d want 1", r.PollCount)
	}
	_, _ = r.PollInput()
	if r.PollCount != 2 {
		t.Fatalf("PollCount = %d want 2", r.PollCount)
	}
}

func TestRecorder_Now(t *testing.T) {
	r := NewRecorder(320, 200)
	r.NowVal = 42.5
	if n := r.Now(); n != 42.5 {
		t.Fatalf("Now = %v want 42.5", n)
	}
}

func TestRecorder_Reset(t *testing.T) {
	r := NewRecorder(320, 200)
	_ = r.PresentFrame([]byte{1}, 1, 1)
	_ = r.QueueAudio([]sound.StereoSample{{L: 1, R: 1}})
	_, _ = r.PollInput()
	r.Reset()
	if len(r.Frames) != 0 || len(r.Audio) != 0 || r.PollCount != 0 {
		t.Fatalf("Reset left state: %+v", r)
	}
	// Width / Height / NowVal preserved.
	if r.Width != 320 || r.Height != 200 {
		t.Fatalf("Reset clobbered Width/Height: %dx%d", r.Width, r.Height)
	}
}
