// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ascii

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/go-quake1/engine/sound"
)

// ----- New ---------------------------------------------------------

func TestNew_Happy(t *testing.T) {
	var buf bytes.Buffer
	b, err := New(320, 200, 80, 25, &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Width != 320 || b.Height != 200 || b.Cols != 80 || b.Rows != 25 {
		t.Fatalf("dims: %+v", b)
	}
}

func TestNew_NilWriter(t *testing.T) {
	_, err := New(320, 200, 80, 25, nil)
	if !errors.Is(err, ErrASCIINilWriter) {
		t.Fatalf("err = %v want ErrASCIINilWriter", err)
	}
}

func TestNew_BadDim(t *testing.T) {
	var buf bytes.Buffer
	cases := []struct{ w, h, c, r int }{
		{0, 200, 80, 25},
		{320, 0, 80, 25},
		{320, 200, 0, 25},
		{320, 200, 80, 0},
		{-1, 200, 80, 25},
	}
	for _, c := range cases {
		if _, err := New(c.w, c.h, c.c, c.r, &buf); !errors.Is(err, ErrASCIIBadDim) {
			t.Fatalf("New(%+v) err = %v want ErrASCIIBadDim", c, err)
		}
	}
}

// ----- PresentFrame -----------------------------------------------

func TestPresentFrame_AllBlack(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(8, 4, 4, 2, &buf)
	rgba := make([]byte, 8*4*4) // all zero -> luminance 0
	if err := b.PresentFrame(rgba, 8, 4); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	// 2 rows of 4 chars each + frame separator
	out := buf.String()
	if !strings.Contains(out, "    \n") {
		t.Fatalf("expected darkest row of 4 spaces; got: %q", out)
	}
}

func TestPresentFrame_AllWhite(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(8, 4, 4, 2, &buf)
	rgba := make([]byte, 8*4*4)
	for i := range rgba {
		rgba[i] = 0xFF
	}
	if err := b.PresentFrame(rgba, 8, 4); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	// Brightest ramp char (last in Ramp) is '@'.
	if !strings.Contains(buf.String(), "@@@@\n") {
		t.Fatalf("expected brightest row of 4 '@'; got: %q", buf.String())
	}
}

func TestPresentFrame_BadRGBASize(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(8, 4, 4, 2, &buf)
	err := b.PresentFrame(make([]byte, 100), 8, 4)
	if !errors.Is(err, ErrASCIIRGBASize) {
		t.Fatalf("err = %v want ErrASCIIRGBASize", err)
	}
}

func TestPresentFrame_FramesWritten(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(4, 4, 2, 2, &buf)
	rgba := make([]byte, 4*4*4)
	for i := 0; i < 3; i++ {
		if err := b.PresentFrame(rgba, 4, 4); err != nil {
			t.Fatalf("PresentFrame %d: %v", i, err)
		}
	}
	if b.FramesWritten() != 3 {
		t.Fatalf("FramesWritten = %d want 3", b.FramesWritten())
	}
}

// errWriter fails on every Write call.
type errWriter struct{ err error }

func (e *errWriter) Write(_ []byte) (int, error) { return 0, e.err }

func TestPresentFrame_WriterError(t *testing.T) {
	myErr := errors.New("disk full")
	b, _ := New(4, 4, 2, 2, &errWriter{err: myErr})
	err := b.PresentFrame(make([]byte, 64), 4, 4)
	if !errors.Is(err, ErrASCIIWriteFailed) {
		t.Fatalf("err = %v want ErrASCIIWriteFailed", err)
	}
	if !errors.Is(err, myErr) {
		t.Fatalf("err = %v should also wrap underlying", err)
	}
}

// flakyWriter errors after the Nth Write call (used to test the
// frame-separator error path, which happens AFTER the row writes).
type flakyWriter struct {
	failAfter int
	count     int
	buf       bytes.Buffer
}

func (f *flakyWriter) Write(p []byte) (int, error) {
	f.count++
	if f.count > f.failAfter {
		return 0, io.ErrShortWrite
	}
	return f.buf.Write(p)
}

func TestPresentFrame_SeparatorWriteError(t *testing.T) {
	// failAfter = Rows (=2): first 2 row writes succeed, the
	// frame-separator write fails.
	w := &flakyWriter{failAfter: 2}
	b, _ := New(4, 4, 2, 2, w)
	err := b.PresentFrame(make([]byte, 64), 4, 4)
	if !errors.Is(err, ErrASCIIWriteFailed) {
		t.Fatalf("separator-err = %v want ErrASCIIWriteFailed", err)
	}
}

func TestPresentFrame_CellClamping(t *testing.T) {
	// Cols > Width -> cellW = 0 -> clamps to 1.
	var buf bytes.Buffer
	b, _ := New(4, 4, 8, 8, &buf)
	rgba := make([]byte, 4*4*4)
	if err := b.PresentFrame(rgba, 4, 4); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
}

func TestPresentFrame_ExactLuminanceCells(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(4, 2, 2, 1, &buf)
	// Build a 4x2 RGBA frame: left half all 0x80, right half all 0xC0.
	rgba := make([]byte, 4*2*4)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			off := (y*4 + x) * 4
			rgba[off] = 0x80
			rgba[off+1] = 0x80
			rgba[off+2] = 0x80
			rgba[off+3] = 0xFF
		}
		for x := 2; x < 4; x++ {
			off := (y*4 + x) * 4
			rgba[off] = 0xC0
			rgba[off+1] = 0xC0
			rgba[off+2] = 0xC0
			rgba[off+3] = 0xFF
		}
	}
	if err := b.PresentFrame(rgba, 4, 2); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	out := buf.String()
	// The output should be 3 bytes (2 ascii chars + newline) + separator.
	// First char (left) should be darker than second char (right).
	if len(out) < 3 {
		t.Fatalf("too-short output: %q", out)
	}
	// Compare ramp indices (Ramp isn't sorted by ASCII byte value).
	idx0 := bytes.IndexByte(Ramp, out[0])
	idx1 := bytes.IndexByte(Ramp, out[1])
	if idx0 >= idx1 {
		t.Fatalf("expected gradient by ramp idx: %q (left='%c'@%d, right='%c'@%d)",
			out, out[0], idx0, out[1], idx1)
	}
}

// ----- Backend interface methods ---------------------------------

func TestSize(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(320, 200, 80, 25, &buf)
	w, h := b.Size()
	if w != 320 || h != 200 {
		t.Fatalf("Size = %dx%d want 320x200", w, h)
	}
}

func TestQueueAudio_DropsSilently(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(4, 4, 2, 2, &buf)
	if err := b.QueueAudio([]sound.StereoSample{{L: 1, R: 1}}); err != nil {
		t.Fatalf("QueueAudio: %v", err)
	}
}

func TestSampleRate(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(4, 4, 2, 2, &buf)
	if r := b.SampleRate(); r != 22050 {
		t.Fatalf("SampleRate = %d want 22050", r)
	}
}

func TestPollInput_Empty(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(4, 4, 2, 2, &buf)
	snap, err := b.PollInput()
	if err != nil {
		t.Fatalf("PollInput: %v", err)
	}
	if len(snap.KeysDown) != 0 || len(snap.KeysUp) != 0 {
		t.Fatalf("PollInput returned non-empty")
	}
}

func TestNow_Monotonic(t *testing.T) {
	var buf bytes.Buffer
	b, _ := New(4, 4, 2, 2, &buf)
	t0 := b.Now()
	t1 := b.Now()
	t2 := b.Now()
	if t0 != 0 {
		t.Fatalf("Now()[0] = %v want 0", t0)
	}
	if t1 <= t0 || t2 <= t1 {
		t.Fatalf("Now() not monotonic: %v %v %v", t0, t1, t2)
	}
}

func TestRampPresent(t *testing.T) {
	// Drift detector for the canonical ramp.
	if len(Ramp) != 10 {
		t.Fatalf("Ramp length = %d want 10", len(Ramp))
	}
	if Ramp[0] != ' ' || Ramp[len(Ramp)-1] != '@' {
		t.Fatalf("Ramp endpoints: %q .. %q", Ramp[0], Ramp[len(Ramp)-1])
	}
}
