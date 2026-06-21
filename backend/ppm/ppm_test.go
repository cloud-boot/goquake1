// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ppm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/sound"
)

// bufferWC adapts *bytes.Buffer to io.WriteCloser.
type bufferWC struct{ *bytes.Buffer }

func (bufferWC) Close() error { return nil }

// nopFactory returns a fresh in-memory writer each call and records
// the buffers in order, so the test can assert on the bytes.
type nopFactory struct {
	buffers []*bytes.Buffer
}

func (f *nopFactory) factory(_ int) (io.WriteCloser, error) {
	b := &bytes.Buffer{}
	f.buffers = append(f.buffers, b)
	return bufferWC{b}, nil
}

// failingWC fails on Write (or Close, configurable).
type failingWC struct {
	failOnWrite bool
	failOnClose bool
	writes      int
	failAtWrite int // 1-indexed Write call to fail at when failOnWrite
}

var errInjected = errors.New("injected writer error")

func (f *failingWC) Write(p []byte) (int, error) {
	f.writes++
	if f.failOnWrite && f.writes == f.failAtWrite {
		return 0, errInjected
	}
	return len(p), nil
}

func (f *failingWC) Close() error {
	if f.failOnClose {
		return errInjected
	}
	return nil
}

func TestBackend_ImplementsBackendInterface(t *testing.T) {
	var _ backend.Backend = (*Backend)(nil)
}

func TestNew_Happy(t *testing.T) {
	f := &nopFactory{}
	b, err := New(320, 200, f.factory)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Width != 320 || b.Height != 200 {
		t.Fatalf("Width/Height = %d/%d want 320/200", b.Width, b.Height)
	}
	if b.NewWriter == nil {
		t.Fatalf("NewWriter not stored")
	}
}

func TestNew_NilWriter(t *testing.T) {
	b, err := New(320, 200, nil)
	if !errors.Is(err, ErrPPMNilWriter) {
		t.Fatalf("err = %v want ErrPPMNilWriter", err)
	}
	if b != nil {
		t.Fatalf("Backend should be nil on error")
	}
}

func TestPresentFrame_Happy(t *testing.T) {
	f := &nopFactory{}
	b, _ := New(4, 4, f.factory)

	// 4x4 RGBA: each pixel = (i, i+1, i+2, 0xFF)
	rgba := make([]byte, 4*4*4)
	for i := 0; i < 16; i++ {
		rgba[i*4+0] = byte(i)
		rgba[i*4+1] = byte(i + 1)
		rgba[i*4+2] = byte(i + 2)
		rgba[i*4+3] = 0xFF
	}

	if err := b.PresentFrame(rgba, 4, 4); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	if b.FramesWritten() != 1 {
		t.Fatalf("FramesWritten = %d want 1", b.FramesWritten())
	}
	if len(f.buffers) != 1 {
		t.Fatalf("factory invoked %d times want 1", len(f.buffers))
	}

	got := f.buffers[0].Bytes()
	wantHeader := []byte("P6\n4 4\n255\n")
	if !bytes.HasPrefix(got, wantHeader) {
		t.Fatalf("header = %q want prefix %q", got, wantHeader)
	}
	body := got[len(wantHeader):]
	if len(body) != 4*4*3 {
		t.Fatalf("body len = %d want %d", len(body), 4*4*3)
	}
	for i := 0; i < 16; i++ {
		if body[i*3+0] != byte(i) || body[i*3+1] != byte(i+1) || body[i*3+2] != byte(i+2) {
			t.Fatalf("pixel %d = %v,%v,%v want %d,%d,%d", i,
				body[i*3+0], body[i*3+1], body[i*3+2], i, i+1, i+2)
		}
	}
}

func TestPresentFrame_MultipleAdvancesIndex(t *testing.T) {
	var got []int
	rgba := make([]byte, 1*1*4)
	b, _ := New(1, 1, func(frameIdx int) (io.WriteCloser, error) {
		got = append(got, frameIdx)
		return bufferWC{&bytes.Buffer{}}, nil
	})
	for i := 0; i < 3; i++ {
		if err := b.PresentFrame(rgba, 1, 1); err != nil {
			t.Fatalf("PresentFrame[%d]: %v", i, err)
		}
	}
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("frameIdx sequence = %v want [0 1 2]", got)
	}
	if b.FramesWritten() != 3 {
		t.Fatalf("FramesWritten = %d want 3", b.FramesWritten())
	}
}

func TestPresentFrame_WrongSize(t *testing.T) {
	f := &nopFactory{}
	b, _ := New(4, 4, f.factory)
	err := b.PresentFrame([]byte{1, 2, 3}, 4, 4) // way too small
	if !errors.Is(err, ErrPPMRGBASize) {
		t.Fatalf("err = %v want ErrPPMRGBASize", err)
	}
	if len(f.buffers) != 0 {
		t.Fatalf("factory should not be invoked on size error")
	}
	if b.FramesWritten() != 0 {
		t.Fatalf("FramesWritten = %d want 0", b.FramesWritten())
	}
}

func TestPresentFrame_FactoryError(t *testing.T) {
	wantErr := errors.New("factory broke")
	b, _ := New(1, 1, func(int) (io.WriteCloser, error) {
		return nil, wantErr
	})
	rgba := make([]byte, 4)
	err := b.PresentFrame(rgba, 1, 1)
	if !errors.Is(err, ErrPPMWriteFailed) {
		t.Fatalf("err = %v want ErrPPMWriteFailed wrap", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v should wrap factory error", err)
	}
}

func TestPresentFrame_HeaderWriteError(t *testing.T) {
	fwc := &failingWC{failOnWrite: true, failAtWrite: 1}
	b, _ := New(1, 1, func(int) (io.WriteCloser, error) { return fwc, nil })
	rgba := make([]byte, 4)
	err := b.PresentFrame(rgba, 1, 1)
	if !errors.Is(err, ErrPPMWriteFailed) {
		t.Fatalf("err = %v want ErrPPMWriteFailed", err)
	}
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v should wrap injected error", err)
	}
	if b.FramesWritten() != 0 {
		t.Fatalf("FramesWritten = %d want 0", b.FramesWritten())
	}
}

func TestPresentFrame_BodyWriteError(t *testing.T) {
	fwc := &failingWC{failOnWrite: true, failAtWrite: 2} // header passes, body fails
	b, _ := New(1, 1, func(int) (io.WriteCloser, error) { return fwc, nil })
	rgba := make([]byte, 4)
	err := b.PresentFrame(rgba, 1, 1)
	if !errors.Is(err, ErrPPMWriteFailed) {
		t.Fatalf("err = %v want ErrPPMWriteFailed", err)
	}
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v should wrap injected error", err)
	}
}

func TestPresentFrame_CloseError(t *testing.T) {
	fwc := &failingWC{failOnClose: true}
	b, _ := New(1, 1, func(int) (io.WriteCloser, error) { return fwc, nil })
	rgba := make([]byte, 4)
	err := b.PresentFrame(rgba, 1, 1)
	if !errors.Is(err, ErrPPMWriteFailed) {
		t.Fatalf("err = %v want ErrPPMWriteFailed", err)
	}
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v should wrap injected error", err)
	}
	if b.FramesWritten() != 0 {
		t.Fatalf("FramesWritten = %d want 0 (close failed)", b.FramesWritten())
	}
}

func TestSize(t *testing.T) {
	b, _ := New(640, 480, (&nopFactory{}).factory)
	w, h := b.Size()
	if w != 640 || h != 480 {
		t.Fatalf("Size = %dx%d want 640x480", w, h)
	}
}

func TestQueueAudio_AppendsAndIsolates(t *testing.T) {
	b, _ := New(1, 1, (&nopFactory{}).factory)
	samples := []sound.StereoSample{{L: 10, R: 20}, {L: 30, R: 40}}
	if err := b.QueueAudio(samples); err != nil {
		t.Fatalf("QueueAudio: %v", err)
	}
	if len(b.Audio) != 1 || len(b.Audio[0]) != 2 {
		t.Fatalf("Audio shape = %v", b.Audio)
	}
	// Mutate caller's slice; backend copy unaffected.
	samples[0].L = 9999
	if b.Audio[0][0].L != 10 {
		t.Fatalf("QueueAudio didn't copy data")
	}
	// Second call appends.
	if err := b.QueueAudio([]sound.StereoSample{{L: 1, R: 2}}); err != nil {
		t.Fatalf("QueueAudio 2nd: %v", err)
	}
	if len(b.Audio) != 2 {
		t.Fatalf("Audio outer len = %d want 2", len(b.Audio))
	}
}

func TestSampleRate(t *testing.T) {
	b, _ := New(1, 1, (&nopFactory{}).factory)
	if b.SampleRate() != 22050 {
		t.Fatalf("SampleRate = %d want 22050", b.SampleRate())
	}
}

func TestPollInput_Empty(t *testing.T) {
	b, _ := New(1, 1, (&nopFactory{}).factory)
	snap, err := b.PollInput()
	if err != nil {
		t.Fatalf("PollInput: %v", err)
	}
	want := backend.InputSnapshot{}
	if snap.MouseDX != want.MouseDX || snap.MouseDY != want.MouseDY ||
		snap.QuitRequested != want.QuitRequested ||
		len(snap.KeysDown) != 0 || len(snap.KeysUp) != 0 {
		t.Fatalf("PollInput = %+v want empty", snap)
	}
}

func TestNow_Increments(t *testing.T) {
	b, _ := New(1, 1, (&nopFactory{}).factory)
	if n := b.Now(); n != 0 {
		t.Fatalf("first Now = %v want 0", n)
	}
	if n := b.Now(); n != TickIncrement {
		t.Fatalf("second Now = %v want %v", n, TickIncrement)
	}
	if n := b.Now(); n != 2*TickIncrement {
		t.Fatalf("third Now = %v want %v", n, 2*TickIncrement)
	}
}

func TestResetClock(t *testing.T) {
	b, _ := New(1, 1, (&nopFactory{}).factory)
	_ = b.Now()
	_ = b.Now()
	_ = b.Now()
	b.ResetClock()
	if n := b.Now(); n != 0 {
		t.Fatalf("Now after ResetClock = %v want 0", n)
	}
}

func TestFramesWritten_StartsZero(t *testing.T) {
	b, _ := New(1, 1, (&nopFactory{}).factory)
	if b.FramesWritten() != 0 {
		t.Fatalf("FramesWritten init = %d want 0", b.FramesWritten())
	}
}

func TestNumberedFileFactory(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "frame_")
	factory := NumberedFileFactory(prefix, "ppm", 4)

	// Write a couple of frames and verify the file paths + contents.
	for i := 0; i < 2; i++ {
		wc, err := factory(i)
		if err != nil {
			t.Fatalf("factory[%d]: %v", i, err)
		}
		payload := []byte(fmt.Sprintf("payload-%d", i))
		if _, err := wc.Write(payload); err != nil {
			t.Fatalf("write[%d]: %v", i, err)
		}
		if err := wc.Close(); err != nil {
			t.Fatalf("close[%d]: %v", i, err)
		}
	}

	for i := 0; i < 2; i++ {
		want := filepath.Join(dir, fmt.Sprintf("frame_%04d.ppm", i))
		got, err := os.ReadFile(want)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", want, err)
		}
		if string(got) != fmt.Sprintf("payload-%d", i) {
			t.Fatalf("file %s = %q want %q", want, got, fmt.Sprintf("payload-%d", i))
		}
	}
}

func TestNumberedFileFactory_BadPath(t *testing.T) {
	// Pointing into a nonexistent parent dir causes os.Create to fail.
	factory := NumberedFileFactory("/this/path/definitely/does/not/exist/frame_", "ppm", 4)
	wc, err := factory(0)
	if err == nil {
		_ = wc.Close()
		t.Fatalf("expected error for bad path")
	}
}

// End-to-end smoke: New + PresentFrame via NumberedFileFactory.
// Guards against subtle integration mistakes (e.g. factory invoked
// with wrong index, header/body order swapped).
func TestPresentFrame_EndToEnd_NumberedFile(t *testing.T) {
	dir := t.TempDir()
	factory := NumberedFileFactory(filepath.Join(dir, "shot_"), "ppm", 3)
	b, err := New(2, 1, factory)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 2x1 RGBA: red, green
	rgba := []byte{
		0xFF, 0x00, 0x00, 0xFF,
		0x00, 0xFF, 0x00, 0xFF,
	}
	if err := b.PresentFrame(rgba, 2, 1); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	path := filepath.Join(dir, "shot_000.ppm")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := append([]byte("P6\n2 1\n255\n"), 0xFF, 0x00, 0x00, 0x00, 0xFF, 0x00)
	if !bytes.Equal(got, want) {
		t.Fatalf("file bytes = %v want %v", got, want)
	}
}
