// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package realdev

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/backend/virtio"
	"github.com/go-quake1/engine/sound"
	"github.com/go-virtio/gpu"
	gvinput "github.com/go-virtio/input"
	gvsound "github.com/go-virtio/sound"
)

// ----- framebuffer ----------------------------------------------------

// TestWrapFramebuffer verifies the constructor wires the three
// trivially-readable fields (Buffer / Width / Height) by inspection on
// a hand-built *gpu.Framebuffer. Flush is covered separately via the
// internal struct + injected function (a real fb.Flush() would dive
// into VirtioGPU.sendCommand and crash on the nil transport).
func TestWrapFramebuffer(t *testing.T) {
	pix := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	fb := &gpu.Framebuffer{Pix: pix, Width: 2, Height: 1}

	got := WrapFramebuffer(fb)
	if !sliceEq(got.Buffer(), pix) {
		t.Fatalf("Buffer mismatch: got %v want %v", got.Buffer(), pix)
	}
	if got.Width() != 2 {
		t.Fatalf("Width = %d want 2", got.Width())
	}
	if got.Height() != 1 {
		t.Fatalf("Height = %d want 1", got.Height())
	}
}

// TestFramebufferAdapter_FlushOK / TestFramebufferAdapter_FlushErr cover
// Flush via the internal adapter with an injected flush func — the
// production constructor wires this to fb.Flush, which we cannot
// invoke without a live virtio-gpu transport.
func TestFramebufferAdapter_FlushOK(t *testing.T) {
	called := 0
	a := &framebufferAdapter{flush: func() error { called++; return nil }}
	if err := a.Flush(); err != nil {
		t.Fatalf("Flush err = %v", err)
	}
	if called != 1 {
		t.Fatalf("flush called %d times, want 1", called)
	}
}

func TestFramebufferAdapter_FlushErr(t *testing.T) {
	want := errors.New("flush-failed")
	a := &framebufferAdapter{flush: func() error { return want }}
	if err := a.Flush(); !errors.Is(err, want) {
		t.Fatalf("Flush err = %v want %v", err, want)
	}
}

// ----- input ----------------------------------------------------------

// TestWrapInput verifies the constructor returns a non-nil adapter
// over the underlying *input.VirtioInput. The PollEvents drain loop is
// covered separately via the internal struct + injected ReadEvent func
// (a real in.ReadEvent would touch the unexported eventq and panic on
// the nil transport).
func TestWrapInput(t *testing.T) {
	got := WrapInput(&gvinput.VirtioInput{})
	if got == nil {
		t.Fatalf("WrapInput returned nil")
	}
}

// TestInputAdapter_PollEvents_DrainsUntilNotReady feeds the loop a key
// down + a REL_X delta + a REL_Y delta, then signals end-of-batch via
// ErrEventNotReady. The translated slice must mirror those three
// events in order.
func TestInputAdapter_PollEvents_DrainsUntilNotReady(t *testing.T) {
	queue := []*gvinput.Event{
		{Type: gvinput.EvKey, Code: gvinput.KeyW, Value: gvinput.KeyValueDown},
		{Type: gvinput.EvRel, Code: gvinput.RelX, Value: u32(-7)},
		{Type: gvinput.EvRel, Code: gvinput.RelY, Value: 5},
	}
	calls := 0
	a := &inputAdapter{read: func(blocking bool) (*gvinput.Event, error) {
		calls++
		if blocking {
			t.Fatalf("PollEvents must call ReadEvent(false), got blocking=true")
		}
		if len(queue) == 0 {
			return nil, gvinput.ErrEventNotReady
		}
		ev := queue[0]
		queue = queue[1:]
		return ev, nil
	}}
	got, err := a.PollEvents()
	if err != nil {
		t.Fatalf("PollEvents err = %v", err)
	}
	want := []virtio.InputEvent{
		{Kind: virtio.EventKey, Code: gvinput.KeyW, Value: 1},
		{Kind: virtio.EventRelX, Value: -7},
		{Kind: virtio.EventRelY, Value: 5},
	}
	if !eventsEq(got, want) {
		t.Fatalf("PollEvents got %#v want %#v", got, want)
	}
	if calls != 4 {
		t.Fatalf("ReadEvent called %d times, want 4 (3 events + 1 not-ready)", calls)
	}
}

// TestInputAdapter_PollEvents_EmptyQueue: an immediate ErrEventNotReady
// returns an empty slice + nil error.
func TestInputAdapter_PollEvents_EmptyQueue(t *testing.T) {
	a := &inputAdapter{read: func(blocking bool) (*gvinput.Event, error) {
		return nil, gvinput.ErrEventNotReady
	}}
	got, err := a.PollEvents()
	if err != nil {
		t.Fatalf("PollEvents err = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("PollEvents got %d events, want 0", len(got))
	}
}

// TestInputAdapter_PollEvents_NonRetryableError: any error other than
// ErrEventNotReady aborts the loop and surfaces.
func TestInputAdapter_PollEvents_NonRetryableError(t *testing.T) {
	boom := errors.New("transport boom")
	a := &inputAdapter{read: func(blocking bool) (*gvinput.Event, error) {
		return nil, boom
	}}
	got, err := a.PollEvents()
	if !errors.Is(err, boom) {
		t.Fatalf("PollEvents err = %v want %v", err, boom)
	}
	if got != nil {
		t.Fatalf("PollEvents events = %v, want nil", got)
	}
}

// TestInputAdapter_PollEvents_PartialBatchThenError: events accumulated
// before the error are returned alongside the error.
func TestInputAdapter_PollEvents_PartialBatchThenError(t *testing.T) {
	boom := errors.New("late boom")
	step := 0
	a := &inputAdapter{read: func(blocking bool) (*gvinput.Event, error) {
		step++
		if step == 1 {
			return &gvinput.Event{Type: gvinput.EvKey, Code: gvinput.KeyA, Value: 1}, nil
		}
		return nil, boom
	}}
	got, err := a.PollEvents()
	if !errors.Is(err, boom) {
		t.Fatalf("PollEvents err = %v want %v", err, boom)
	}
	if len(got) != 1 || got[0].Kind != virtio.EventKey || got[0].Code != gvinput.KeyA {
		t.Fatalf("PollEvents partial batch = %#v", got)
	}
}

// TestInputAdapter_PollEvents_NilNilTerminates: if ReadEvent ever
// returns (nil, nil) the loop must terminate instead of spinning.
func TestInputAdapter_PollEvents_NilNilTerminates(t *testing.T) {
	calls := 0
	a := &inputAdapter{read: func(blocking bool) (*gvinput.Event, error) {
		calls++
		return nil, nil
	}}
	got, err := a.PollEvents()
	if err != nil {
		t.Fatalf("PollEvents err = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("PollEvents got %d events want 0", len(got))
	}
	if calls != 1 {
		t.Fatalf("PollEvents called read %d times, want 1 (must not spin)", calls)
	}
}

// TestInputAdapter_PollEvents_DropsUnmappedEvent: events translateEvent
// drops (EvSyn, EvAbs, REL_WHEEL, ...) must not appear in the output.
func TestInputAdapter_PollEvents_DropsUnmappedEvent(t *testing.T) {
	queue := []*gvinput.Event{
		{Type: gvinput.EvSyn, Code: gvinput.SynReport, Value: 0},
		{Type: gvinput.EvRel, Code: gvinput.RelX, Value: 1},
	}
	a := &inputAdapter{read: func(blocking bool) (*gvinput.Event, error) {
		if len(queue) == 0 {
			return nil, gvinput.ErrEventNotReady
		}
		ev := queue[0]
		queue = queue[1:]
		return ev, nil
	}}
	got, err := a.PollEvents()
	if err != nil {
		t.Fatalf("PollEvents err = %v", err)
	}
	if len(got) != 1 || got[0].Kind != virtio.EventRelX {
		t.Fatalf("PollEvents = %#v want a single EventRelX", got)
	}
}

// ----- translateEvent -------------------------------------------------

// TestTranslateEvent_EvKey covers the key-press / key-release / repeat
// fan-out — the wrapper preserves Code + Value verbatim and tags the
// event as EventKey.
func TestTranslateEvent_EvKey(t *testing.T) {
	cases := []struct {
		name  string
		ev    gvinput.Event
		wantV int32
	}{
		{"down", gvinput.Event{Type: gvinput.EvKey, Code: gvinput.KeyA, Value: gvinput.KeyValueDown}, 1},
		{"up", gvinput.Event{Type: gvinput.EvKey, Code: gvinput.KeyA, Value: gvinput.KeyValueUp}, 0},
		{"repeat", gvinput.Event{Type: gvinput.EvKey, Code: gvinput.KeyA, Value: gvinput.KeyValueRepeat}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := translateEvent(tc.ev)
			if !ok || out.Kind != virtio.EventKey || out.Code != gvinput.KeyA || out.Value != tc.wantV {
				t.Fatalf("translate(%+v) = (%+v,%v)", tc.ev, out, ok)
			}
		})
	}
}

// TestTranslateEvent_EvRelXY covers the two recognised relative axes.
// Negative deltas must round-trip through the uint32→int32 reinterpret.
func TestTranslateEvent_EvRelXY(t *testing.T) {
	cases := []struct {
		name string
		ev   gvinput.Event
		kind virtio.EventKind
		want int32
	}{
		{"relx-positive", gvinput.Event{Type: gvinput.EvRel, Code: gvinput.RelX, Value: 42}, virtio.EventRelX, 42},
		{"relx-negative", gvinput.Event{Type: gvinput.EvRel, Code: gvinput.RelX, Value: u32(-3)}, virtio.EventRelX, -3},
		{"rely-positive", gvinput.Event{Type: gvinput.EvRel, Code: gvinput.RelY, Value: 7}, virtio.EventRelY, 7},
		{"rely-negative", gvinput.Event{Type: gvinput.EvRel, Code: gvinput.RelY, Value: u32(-9)}, virtio.EventRelY, -9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := translateEvent(tc.ev)
			if !ok || out.Kind != tc.kind || out.Value != tc.want {
				t.Fatalf("translate(%+v) = (%+v,%v) want kind=%d val=%d",
					tc.ev, out, ok, tc.kind, tc.want)
			}
		})
	}
}

// TestTranslateEvent_DroppedClasses covers every "silent drop" branch:
//
//   - EvSyn (synchronisation marker)
//   - EvAbs (absolute axis, out of MVP scope)
//   - EvLED (LED state)
//   - EvRel on an axis other than X / Y (the mouse wheel — RelWheel)
//   - EvMsc (miscellaneous)
//   - an event class outside the spec table (defensive default)
func TestTranslateEvent_DroppedClasses(t *testing.T) {
	dropped := []gvinput.Event{
		{Type: gvinput.EvSyn, Code: gvinput.SynReport},
		{Type: gvinput.EvAbs, Code: 0, Value: 100},
		{Type: gvinput.EvLED, Code: 0, Value: 1},
		{Type: gvinput.EvMsc, Code: 4, Value: 0xff},
		{Type: gvinput.EvRel, Code: 8 /* RelWheel */, Value: 1},
		{Type: 0xdead /* unknown */, Code: 0, Value: 0},
	}
	for _, ev := range dropped {
		out, ok := translateEvent(ev)
		if ok {
			t.Fatalf("translate(%+v) should drop, got %+v", ev, out)
		}
		if out != (virtio.InputEvent{}) {
			t.Fatalf("translate(%+v) drop must return zero value, got %+v", ev, out)
		}
	}
}

// ----- audio ----------------------------------------------------------

// TestWrapAudio verifies the constructor returns a non-nil adapter
// over the underlying *sound.VirtioSound. WritePCM + SampleRate are
// covered separately via the internal struct + injected callbacks
// (real snd.Write / snd.StreamParams require the device's unexported
// virtqueue + per-stream registry).
func TestWrapAudio(t *testing.T) {
	got := WrapAudio(&gvsound.VirtioSound{}, 0)
	if got == nil {
		t.Fatalf("WrapAudio returned nil")
	}
}

// TestAudioAdapter_WritePCM_SerializesLEInt16 verifies the byte
// serialisation: two int16 little-endian per frame, L then R, exactly
// 4 bytes per frame.
func TestAudioAdapter_WritePCM_SerializesLEInt16(t *testing.T) {
	var capturedBuf []byte
	var capturedSID uint32 = 0xdeadbeef
	a := &audioAdapter{
		streamID: 7,
		write: func(streamID uint32, frames []byte) (int, error) {
			capturedSID = streamID
			capturedBuf = append(capturedBuf, frames...)
			return len(frames), nil
		},
		rateLookup: stubRateLookup(gvsound.PCMRate44100, true),
	}
	samples := []sound.StereoSample{
		{L: 0x0102, R: 0x0304},
		{L: int16(-1), R: int16(0x7fff)},
	}
	if err := a.WritePCM(samples); err != nil {
		t.Fatalf("WritePCM err = %v", err)
	}
	if capturedSID != 7 {
		t.Fatalf("streamID forwarded as %d want 7", capturedSID)
	}
	want := make([]byte, 8)
	for i, v := range []int16{0x0102, 0x0304, -1, 0x7fff} {
		binary.LittleEndian.PutUint16(want[i*2:i*2+2], uint16(v))
	}
	if !sliceEq(capturedBuf, want) {
		t.Fatalf("serialised bytes = %v want %v", capturedBuf, want)
	}
}

// TestAudioAdapter_WritePCM_EmptyIsNoop: a zero-length input must not
// reach the device — guards against the spec's "Write(nil) is a no-op"
// being unreachable through this wrapper.
func TestAudioAdapter_WritePCM_EmptyIsNoop(t *testing.T) {
	calls := 0
	a := &audioAdapter{write: func(uint32, []byte) (int, error) {
		calls++
		return 0, nil
	}}
	if err := a.WritePCM(nil); err != nil {
		t.Fatalf("WritePCM(nil) err = %v", err)
	}
	if err := a.WritePCM([]sound.StereoSample{}); err != nil {
		t.Fatalf("WritePCM(empty) err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("device called %d times for empty input, want 0", calls)
	}
}

// TestAudioAdapter_WritePCM_ForwardsError: an error from the underlying
// Write must surface to the caller verbatim.
func TestAudioAdapter_WritePCM_ForwardsError(t *testing.T) {
	want := errors.New("xfer boom")
	a := &audioAdapter{
		write:      func(uint32, []byte) (int, error) { return 0, want },
		rateLookup: stubRateLookup(gvsound.PCMRate44100, true),
	}
	err := a.WritePCM([]sound.StereoSample{{L: 1, R: 2}})
	if !errors.Is(err, want) {
		t.Fatalf("WritePCM err = %v want %v", err, want)
	}
}

// TestAudioAdapter_WritePCM_StreamNotReady covers the guard that
// short-circuits the device round-trip when the stream has not been
// negotiated (SampleRate == 0). The wrapper returns
// ErrSoundStreamNotReady without touching the underlying write fn so
// the engine's QueueAudio swallows it via the backend.ErrUnsupported
// branch.
func TestAudioAdapter_WritePCM_StreamNotReady(t *testing.T) {
	calls := 0
	a := &audioAdapter{
		write: func(uint32, []byte) (int, error) {
			calls++
			return 0, nil
		},
		rateLookup: stubRateLookup(0, false),
	}
	err := a.WritePCM([]sound.StereoSample{{L: 1, R: 2}})
	if !errors.Is(err, ErrSoundStreamNotReady) {
		t.Fatalf("WritePCM err = %v want ErrSoundStreamNotReady", err)
	}
	if !errors.Is(err, backend.ErrUnsupported) {
		t.Fatalf("WritePCM err = %v want wrapping backend.ErrUnsupported", err)
	}
	if calls != 0 {
		t.Fatalf("device write called %d times when stream not ready, want 0", calls)
	}
}

// TestAudioAdapter_SampleRate_FromStreamParams covers the happy path:
// StreamParams returns a cached PCMParams; the wrapper decodes Rate
// through pcmRateHz.
func TestAudioAdapter_SampleRate_FromStreamParams(t *testing.T) {
	a := &audioAdapter{
		streamID: 0,
		rateLookup: func(uint32) (gvsound.PCMParams, bool) {
			return gvsound.PCMParams{Rate: gvsound.PCMRate44100}, true
		},
	}
	if got := a.SampleRate(); got != 44100 {
		t.Fatalf("SampleRate = %d want 44100", got)
	}
}

// TestAudioAdapter_SampleRate_NotNegotiated covers the fallback when
// StreamParams reports the stream has not been configured yet.
func TestAudioAdapter_SampleRate_NotNegotiated(t *testing.T) {
	a := &audioAdapter{
		rateLookup: func(uint32) (gvsound.PCMParams, bool) {
			return gvsound.PCMParams{}, false
		},
	}
	if got := a.SampleRate(); got != 0 {
		t.Fatalf("SampleRate not-negotiated = %d want 0", got)
	}
}

// TestPCMRateHz covers every spec-defined byte-id plus the
// default-zero branch for an out-of-range value.
func TestPCMRateHz(t *testing.T) {
	cases := []struct {
		in   uint8
		want int
	}{
		{gvsound.PCMRate5512, 5512},
		{gvsound.PCMRate8000, 8000},
		{gvsound.PCMRate11025, 11025},
		{gvsound.PCMRate16000, 16000},
		{gvsound.PCMRate22050, 22050},
		{gvsound.PCMRate32000, 32000},
		{gvsound.PCMRate44100, 44100},
		{gvsound.PCMRate48000, 48000},
		{gvsound.PCMRate64000, 64000},
		{gvsound.PCMRate88200, 88200},
		{gvsound.PCMRate96000, 96000},
		{gvsound.PCMRate176400, 176400},
		{gvsound.PCMRate192000, 192000},
		{gvsound.PCMRate384000, 384000},
		{0xff /* out of range */, 0},
	}
	for _, tc := range cases {
		if got := pcmRateHz(tc.in); got != tc.want {
			t.Fatalf("pcmRateHz(%d) = %d want %d", tc.in, got, tc.want)
		}
	}
}

// TestSerializeStereo_Empty: the helper must handle a zero-length
// input by returning a zero-length buffer (no panic on out[0*4+0:...]).
func TestSerializeStereo_Empty(t *testing.T) {
	if got := serializeStereo(nil); len(got) != 0 {
		t.Fatalf("serializeStereo(nil) = %v want empty", got)
	}
}

// TestErrSoundStreamNotReady_IsExported guards the sentinel's
// stability — engine code is allowed to errors.Is-check it. The
// sentinel must also wrap backend.ErrUnsupported so the existing
// backend.QueueAudio "swallow when unsupported" branch fires without
// any change to the engine.
func TestErrSoundStreamNotReady_IsExported(t *testing.T) {
	if ErrSoundStreamNotReady == nil {
		t.Fatal("ErrSoundStreamNotReady must be non-nil")
	}
	if ErrSoundStreamNotReady.Error() == "" {
		t.Fatal("ErrSoundStreamNotReady must have a non-empty message")
	}
	if !errors.Is(ErrSoundStreamNotReady, backend.ErrUnsupported) {
		t.Fatalf("ErrSoundStreamNotReady (%v) must wrap backend.ErrUnsupported", ErrSoundStreamNotReady)
	}
}

// stubRateLookup returns a sampleRateFn closure that always reports
// the supplied (PCMRate*, ok) pair regardless of the streamID asked
// about. Test-helper for audioAdapter constructors.
func stubRateLookup(rate uint8, ok bool) sampleRateFn {
	return func(uint32) (gvsound.PCMParams, bool) {
		return gvsound.PCMParams{Rate: rate}, ok
	}
}

// ----- helpers --------------------------------------------------------

// u32 reinterprets a signed delta as the unsigned wire encoding used
// by virtio_input_event.value (Linux evdev stores signed int32 as
// the same bit pattern in a uint32 field).
func u32(v int32) uint32 { return uint32(v) }

func sliceEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eventsEq(a, b []virtio.InputEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
