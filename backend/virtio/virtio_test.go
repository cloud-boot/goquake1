// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package virtio

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/sound"
)

// ----- mocks -------------------------------------------------------

// fakeFB is an in-memory Framebuffer that records Flush calls + can
// be told to fail Flush on demand.
type fakeFB struct {
	w, h       uint32
	buf        []byte
	flushCalls int
	flushErr   error
}

func newFakeFB(w, h uint32) *fakeFB {
	return &fakeFB{w: w, h: h, buf: make([]byte, int(w)*int(h)*4)}
}

func (f *fakeFB) Buffer() []byte { return f.buf }
func (f *fakeFB) Width() uint32  { return f.w }
func (f *fakeFB) Height() uint32 { return f.h }
func (f *fakeFB) Flush() error {
	f.flushCalls++
	return f.flushErr
}

// fakeInput hands out a canned event list (then drains on subsequent
// calls) and can be told to return an error instead.
type fakeInput struct {
	events []InputEvent
	err    error
	calls  int
}

func (f *fakeInput) PollEvents() ([]InputEvent, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := f.events
	f.events = nil
	return out, nil
}

// fakeAudio captures WritePCM calls + can be told to return an error.
type fakeAudio struct {
	rate    int
	lastIn  []sound.StereoSample
	writes  int
	wantErr error
}

func (f *fakeAudio) WritePCM(s []sound.StereoSample) error {
	f.writes++
	f.lastIn = s
	return f.wantErr
}
func (f *fakeAudio) SampleRate() int { return f.rate }

// ----- New ---------------------------------------------------------

func TestNew_Happy(t *testing.T) {
	fb := newFakeFB(4, 2)
	in := &fakeInput{}
	au := &fakeAudio{rate: 44100}
	clk := func() float64 { return 5 }
	b, err := New(fb, in, au, clk)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.FB != fb || b.Input != in || b.Audio != au {
		t.Fatalf("New: dependencies not wired")
	}
	if b.Clock() != 5 {
		t.Fatalf("clock not wired")
	}
}

func TestNew_NilFB(t *testing.T) {
	_, err := New(nil, nil, nil, nil)
	if !errors.Is(err, ErrVirtioNilFB) {
		t.Fatalf("New(nil FB) err = %v want ErrVirtioNilFB", err)
	}
}

func TestNew_NilOptionalsFallback(t *testing.T) {
	fb := newFakeFB(1, 1)
	b, err := New(fb, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Input != nil || b.Audio != nil || b.Clock != nil {
		t.Fatalf("nil optionals should stay nil; got input=%v audio=%v clock=%v",
			b.Input, b.Audio, b.Clock)
	}
	// PollInput with nil Input → empty snapshot.
	snap, err := b.PollInput()
	if err != nil {
		t.Fatalf("PollInput nil-input: %v", err)
	}
	if len(snap.KeysDown) != 0 || len(snap.KeysUp) != 0 || snap.MouseDX != 0 || snap.MouseDY != 0 || snap.QuitRequested {
		t.Fatalf("nil Input returned non-empty snapshot: %+v", snap)
	}
	// QueueAudio with nil Audio → no error, no panic.
	if err := b.QueueAudio([]sound.StereoSample{{L: 1, R: 1}}); err != nil {
		t.Fatalf("QueueAudio nil-audio: %v", err)
	}
	// SampleRate falls back to 22050.
	if r := b.SampleRate(); r != 22050 {
		t.Fatalf("nil Audio SampleRate = %d want 22050", r)
	}
	// Now with nil Clock advances monotonic 60 Hz tick.
	if got := b.Now(); got != 0 {
		t.Fatalf("first Now() = %v want 0", got)
	}
	if got := b.Now(); got != defaultClockStep {
		t.Fatalf("second Now() = %v want %v", got, defaultClockStep)
	}
}

// ----- PresentFrame ------------------------------------------------

func TestPresentFrame_Happy_BGRASwap(t *testing.T) {
	fb := newFakeFB(2, 1)
	b, _ := New(fb, nil, nil, nil)
	// two RGBA pixels: (0x10, 0x20, 0x30, 0xFF), (0x40, 0x50, 0x60, 0x80).
	src := []byte{
		0x10, 0x20, 0x30, 0xFF,
		0x40, 0x50, 0x60, 0x80,
	}
	if err := b.PresentFrame(src, 2, 1); err != nil {
		t.Fatalf("PresentFrame: %v", err)
	}
	want := []byte{
		0x30, 0x20, 0x10, 0xFF, // B, G, R, A
		0x60, 0x50, 0x40, 0x80,
	}
	for i, w := range want {
		if fb.buf[i] != w {
			t.Fatalf("byte %d = 0x%02x want 0x%02x (full buf = %x)", i, fb.buf[i], w, fb.buf)
		}
	}
	if fb.flushCalls != 1 {
		t.Fatalf("flushCalls = %d want 1", fb.flushCalls)
	}
}

func TestPresentFrame_RGBASizeMismatch(t *testing.T) {
	fb := newFakeFB(2, 2)
	b, _ := New(fb, nil, nil, nil)
	// too short for 2x2x4 = 16 bytes.
	if err := b.PresentFrame([]byte{0, 0, 0, 0}, 2, 2); !errors.Is(err, ErrVirtioRGBASize) {
		t.Fatalf("short RGBA err = %v want ErrVirtioRGBASize", err)
	}
}

func TestPresentFrame_FBBufferMismatch(t *testing.T) {
	// FB whose internal buffer doesn't match width*height*4.
	fb := &fakeFB{w: 2, h: 1, buf: make([]byte, 4)} // half-sized buf
	b, _ := New(fb, nil, nil, nil)
	src := make([]byte, 2*1*4)
	if err := b.PresentFrame(src, 2, 1); !errors.Is(err, ErrVirtioRGBASize) {
		t.Fatalf("FB buf-size mismatch err = %v want ErrVirtioRGBASize", err)
	}
}

func TestPresentFrame_PropagatesFlushErr(t *testing.T) {
	fb := newFakeFB(1, 1)
	fb.flushErr = errors.New("boom")
	b, _ := New(fb, nil, nil, nil)
	src := []byte{1, 2, 3, 4}
	if err := b.PresentFrame(src, 1, 1); err == nil || err.Error() != "boom" {
		t.Fatalf("Flush err = %v want boom", err)
	}
}

// ----- Size --------------------------------------------------------

func TestSize(t *testing.T) {
	fb := newFakeFB(320, 200)
	b, _ := New(fb, nil, nil, nil)
	w, h := b.Size()
	if w != 320 || h != 200 {
		t.Fatalf("Size = %dx%d want 320x200", w, h)
	}
}

// ----- QueueAudio --------------------------------------------------

func TestQueueAudio_Happy(t *testing.T) {
	fb := newFakeFB(1, 1)
	au := &fakeAudio{rate: 44100}
	b, _ := New(fb, nil, au, nil)
	samples := []sound.StereoSample{{L: 100, R: -100}}
	if err := b.QueueAudio(samples); err != nil {
		t.Fatalf("QueueAudio: %v", err)
	}
	if au.writes != 1 || len(au.lastIn) != 1 {
		t.Fatalf("audio writes=%d last=%v", au.writes, au.lastIn)
	}
}

func TestQueueAudio_SwallowsErrUnsupported(t *testing.T) {
	fb := newFakeFB(1, 1)
	au := &fakeAudio{wantErr: backend.ErrUnsupported}
	b, _ := New(fb, nil, au, nil)
	if err := b.QueueAudio(nil); err != nil {
		t.Fatalf("ErrUnsupported should be swallowed, got %v", err)
	}
	// A wrapped ErrUnsupported should also be swallowed.
	au.wantErr = fmt.Errorf("wrap: %w", backend.ErrUnsupported)
	if err := b.QueueAudio(nil); err != nil {
		t.Fatalf("wrapped ErrUnsupported should be swallowed, got %v", err)
	}
}

func TestQueueAudio_PropagatesOtherErrs(t *testing.T) {
	fb := newFakeFB(1, 1)
	au := &fakeAudio{wantErr: errors.New("hw fault")}
	b, _ := New(fb, nil, au, nil)
	if err := b.QueueAudio(nil); err == nil || err.Error() != "hw fault" {
		t.Fatalf("other err = %v want hw fault", err)
	}
}

// ----- SampleRate --------------------------------------------------

func TestSampleRate_FromAudio(t *testing.T) {
	fb := newFakeFB(1, 1)
	au := &fakeAudio{rate: 48000}
	b, _ := New(fb, nil, au, nil)
	if r := b.SampleRate(); r != 48000 {
		t.Fatalf("SampleRate = %d want 48000", r)
	}
}

// ----- PollInput ---------------------------------------------------

func TestPollInput_KeyDownUpMouseQuit(t *testing.T) {
	fb := newFakeFB(1, 1)
	in := &fakeInput{events: []InputEvent{
		{Kind: EventKey, Code: 17, Value: 1}, // KEY_W down
		{Kind: EventKey, Code: 30, Value: 0}, // KEY_A up
		{Kind: EventRelX, Value: 4},
		{Kind: EventRelY, Value: -2},
		{Kind: EventQuit},
	}}
	b, _ := New(fb, in, nil, nil)
	snap, err := b.PollInput()
	if err != nil {
		t.Fatalf("PollInput: %v", err)
	}
	if len(snap.KeysDown) != 1 || snap.KeysDown[0] != backend.KeyW {
		t.Fatalf("KeysDown = %v want [KeyW]", snap.KeysDown)
	}
	if len(snap.KeysUp) != 1 || snap.KeysUp[0] != backend.KeyA {
		t.Fatalf("KeysUp = %v want [KeyA]", snap.KeysUp)
	}
	if snap.MouseDX != 4 || snap.MouseDY != -2 {
		t.Fatalf("mouse delta = (%v, %v) want (4, -2)", snap.MouseDX, snap.MouseDY)
	}
	if !snap.QuitRequested {
		t.Fatalf("QuitRequested = false want true")
	}
	// Second poll: state cleared.
	snap2, _ := b.PollInput()
	if len(snap2.KeysDown) != 0 || len(snap2.KeysUp) != 0 || snap2.MouseDX != 0 || snap2.MouseDY != 0 || snap2.QuitRequested {
		t.Fatalf("state not cleared between polls: %+v", snap2)
	}
}

// TestPollInput_RepeatDropped pins the autorepeat-vs-release distinction.
// Linux EV_KEY value=2 (KeyValueRepeat) is emitted by the virtio-input
// device on every key auto-repeat tick (~33 ms). Treating it as a
// release (the old code's `else` arm) clears the runloop's held bit +
// stamps an up-edge every repeat -- KeyState() then collapses to 0.25
// (both edges, !down) and the player's move axes drop to ~25% of the
// configured cl_*speed (the symptom: cmd.fwd=50 with cl_forwardspeed=
// 200). The driver MUST drop repeats; the original press event already
// latched the held bit and the runloop carries it across frames.
func TestPollInput_RepeatDropped(t *testing.T) {
	fb := newFakeFB(1, 1)
	in := &fakeInput{events: []InputEvent{
		{Kind: EventKey, Code: 17, Value: 1}, // KEY_W down
		{Kind: EventKey, Code: 17, Value: 2}, // KEY_W auto-repeat -- MUST be dropped
		{Kind: EventKey, Code: 17, Value: 2}, // another repeat -- MUST be dropped
	}}
	b, _ := New(fb, in, nil, nil)
	snap, err := b.PollInput()
	if err != nil {
		t.Fatalf("PollInput: %v", err)
	}
	if len(snap.KeysDown) != 1 || snap.KeysDown[0] != backend.KeyW {
		t.Fatalf("KeysDown = %v want exactly [KeyW] (no repeats)", snap.KeysDown)
	}
	if len(snap.KeysUp) != 0 {
		t.Fatalf("KeysUp = %v want [] (repeats must NOT be treated as releases)", snap.KeysUp)
	}
}

func TestPollInput_UnmappedKeyDropped(t *testing.T) {
	fb := newFakeFB(1, 1)
	in := &fakeInput{events: []InputEvent{
		{Kind: EventKey, Code: 0xFFFF, Value: 1}, // not in mapKey table
	}}
	b, _ := New(fb, in, nil, nil)
	snap, err := b.PollInput()
	if err != nil {
		t.Fatalf("PollInput: %v", err)
	}
	if len(snap.KeysDown) != 0 || len(snap.KeysUp) != 0 {
		t.Fatalf("unmapped code should be dropped, got %+v", snap)
	}
}

func TestPollInput_PropagatesPollEventsErr(t *testing.T) {
	fb := newFakeFB(1, 1)
	in := &fakeInput{err: errors.New("device gone")}
	b, _ := New(fb, in, nil, nil)
	if _, err := b.PollInput(); err == nil || err.Error() != "device gone" {
		t.Fatalf("err = %v want device gone", err)
	}
}

// TestPollInput_AllKeyCodes exercises every entry in mapKey + the
// default branch so the code-mapping table has 100% statement coverage.
func TestPollInput_AllKeyCodes(t *testing.T) {
	cases := []struct {
		code uint16
		want backend.KeyCode
		ok   bool
	}{
		{1, backend.KeyEscape, true},
		{17, backend.KeyW, true},
		{28, backend.KeyEnter, true},
		{29, backend.KeyCtrl, true},
		{30, backend.KeyA, true},
		{31, backend.KeyS, true},
		{32, backend.KeyD, true},
		{42, backend.KeyShift, true},
		{57, backend.KeySpace, true},
		{103, backend.KeyUp, true},
		{105, backend.KeyLeft, true},
		{106, backend.KeyRight, true},
		{108, backend.KeyDown, true},
		{272, backend.KeyMouse1, true},
		{273, backend.KeyMouse2, true},
		{999, 0, false},
	}
	for _, tc := range cases {
		got, ok := mapKey(tc.code)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("mapKey(%d) = (%v, %v) want (%v, %v)", tc.code, got, ok, tc.want, tc.ok)
		}
	}
}

// ----- Now ---------------------------------------------------------

func TestNow_WithClock(t *testing.T) {
	fb := newFakeFB(1, 1)
	calls := 0
	b, _ := New(fb, nil, nil, func() float64 {
		calls++
		return 12.5
	})
	if got := b.Now(); got != 12.5 {
		t.Fatalf("Now = %v want 12.5", got)
	}
	if calls != 1 {
		t.Fatalf("clock calls = %d want 1", calls)
	}
}

// ----- Backend-interface satisfaction ------------------------------

func TestBackendImplementsBackend(t *testing.T) {
	fb := newFakeFB(1, 1)
	b, _ := New(fb, nil, nil, nil)
	var _ backend.Backend = b
}
