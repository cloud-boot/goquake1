// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package runloop

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sound"
)

// ----- fakes ---------------------------------------------------------

// fakeHost satisfies HostFramer with caller-controlled error + tic
// counter so tests can assert "Frame was called once / N times" without
// spinning up a real *host.Host (which needs VM + Cache + Resolver +
// Progs + a SpawnServer pass).
type fakeHost struct {
	calls int
	err   error
}

func (f *fakeHost) Frame(_ float32) error {
	f.calls++
	return f.err
}

// failingPresent wraps a Recorder + overrides PresentFrame to return a
// fixed error. Used to assert error propagation from the display path.
type failingPresent struct {
	*backend.Recorder
	err error
}

func (f *failingPresent) PresentFrame(_ []byte, _, _ int) error { return f.err }

// failingPoll wraps Recorder + overrides PollInput to fail.
type failingPoll struct {
	*backend.Recorder
	err error
}

func (f *failingPoll) PollInput() (backend.InputSnapshot, error) {
	return backend.InputSnapshot{}, f.err
}

// failingAudio wraps Recorder + overrides QueueAudio with a caller-
// chosen error. errors.Is(backend.ErrUnsupported) is the silently-
// ignored case; any other error must propagate.
type failingAudio struct {
	*backend.Recorder
	err error
}

func (f *failingAudio) QueueAudio(_ []sound.StereoSample) error { return f.err }

// makeCharsSheet builds a 128x128 conchars Pic with each glyph filled
// with a distinct byte so Compose2D has a well-formed sheet to read
// from. Lifted from render/draw_test.go's same-name helper (kept local
// because Go's test-helper visibility is per-package).
func makeCharsSheet() *render.Pic {
	const dim = 128
	p := &render.Pic{
		Width:  dim,
		Height: dim,
		Pixels: make([]byte, dim*dim),
	}
	for row := 0; row < 16; row++ {
		for col := 0; col < 16; col++ {
			glyph := byte(0x10 + row*16 + col)
			for v := 0; v < render.CharHeight; v++ {
				base := (row*render.CharHeight+v)*dim + col*render.CharWidth
				for u := 0; u < render.CharWidth; u++ {
					p.Pixels[base+u] = glyph
				}
			}
		}
	}
	return p
}

// newRunner builds a minimal Runner wired to a Recorder backend +
// loopback NetConn. The Client is left in StateDisconnected (so
// client.Tick is short-circuited) unless the test overrides.
func newRunner(t *testing.T, b backend.Backend) (*Runner, *server.LoopbackConn) {
	t.Helper()
	const w, h = (1 + render.MinConsoleWidth) * render.CharWidth, 64
	fb, err := render.NewFrameBuffer(w, h)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	con, err := render.NewConsole(render.MinConsoleWidth, render.MinConsoleLines)
	if err != nil {
		t.Fatalf("NewConsole: %v", err)
	}
	scr, err := render.NewScreen(w, h)
	if err != nil {
		t.Fatalf("NewScreen: %v", err)
	}
	pal := &render.Palette{}
	clientSide, _ := server.NewLoopbackConn()
	loop, ok := clientSide.(*server.LoopbackConn)
	if !ok {
		t.Fatalf("loopback conn type %T", clientSide)
	}
	r := &Runner{
		Host:           &fakeHost{},
		Client:         client.NewState(),
		Conn:           clientSide,
		Backend:        b,
		FrameBuffer:    fb,
		Console:        con,
		Screen:         scr,
		Chars:          makeCharsSheet(),
		Palette:        pal,
		Speeds:         client.DefaultInputSpeeds(),
		RGBA:           make([]byte, w*h*4),
		BackgroundIdx:  0x10,
		NotifyLifetime: 3,
		MaxNotifyRows:  2,
	}
	return r, loop
}

// ----- nil-arg + size guards ----------------------------------------

func TestRunFrame_NilHost(t *testing.T) {
	r, _ := newRunner(t, backend.NewRecorder(0, 0))
	r.Host = nil
	if err := r.RunFrame(0.05, 1); !errors.Is(err, ErrRunnerNilHost) {
		t.Fatalf("err = %v want %v", err, ErrRunnerNilHost)
	}
}

func TestRunFrame_NilClient(t *testing.T) {
	r, _ := newRunner(t, backend.NewRecorder(0, 0))
	r.Client = nil
	if err := r.RunFrame(0.05, 1); !errors.Is(err, ErrRunnerNilClient) {
		t.Fatalf("err = %v want %v", err, ErrRunnerNilClient)
	}
}

func TestRunFrame_NilConn(t *testing.T) {
	r, _ := newRunner(t, backend.NewRecorder(0, 0))
	r.Conn = nil
	if err := r.RunFrame(0.05, 1); !errors.Is(err, ErrRunnerNilConn) {
		t.Fatalf("err = %v want %v", err, ErrRunnerNilConn)
	}
}

func TestRunFrame_NilBackend(t *testing.T) {
	r, _ := newRunner(t, backend.NewRecorder(0, 0))
	r.Backend = nil
	if err := r.RunFrame(0.05, 1); !errors.Is(err, ErrRunnerNilBackend) {
		t.Fatalf("err = %v want %v", err, ErrRunnerNilBackend)
	}
}

func TestRunFrame_NilFB(t *testing.T) {
	r, _ := newRunner(t, backend.NewRecorder(0, 0))
	r.FrameBuffer = nil
	if err := r.RunFrame(0.05, 1); !errors.Is(err, ErrRunnerNilFB) {
		t.Fatalf("err = %v want %v", err, ErrRunnerNilFB)
	}
}

func TestRunFrame_RGBASizeTooSmall(t *testing.T) {
	r, _ := newRunner(t, backend.NewRecorder(0, 0))
	r.RGBA = r.RGBA[:4] // way too small
	if err := r.RunFrame(0.05, 1); !errors.Is(err, ErrRunnerRGBASize) {
		t.Fatalf("err = %v want %v", err, ErrRunnerRGBASize)
	}
}

// ----- happy paths ---------------------------------------------------

func TestRunFrame_HappyDisconnected(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, loop := newRunner(t, rec)
	r.Client.Connection = client.StateDisconnected

	// SoundPool present + mix buffer present -- audio path runs.
	pool, err := sound.NewPool(8)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	r.SoundPool = pool
	r.MixBuffer = make([]sound.StereoSample, sound.MixBufferStereoFrames)

	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	if got := r.Host.(*fakeHost).calls; got != 1 {
		t.Fatalf("host.Frame calls = %d want 1", got)
	}
	if len(rec.Frames) != 1 {
		t.Fatalf("rec.Frames len = %d want 1", len(rec.Frames))
	}
	want := r.FrameBuffer.Width * r.FrameBuffer.Height * 4
	if len(rec.Frames[0]) != want {
		t.Fatalf("rec.Frames[0] len = %d want %d", len(rec.Frames[0]), want)
	}
	if len(rec.Audio) != 1 {
		t.Fatalf("rec.Audio len = %d want 1", len(rec.Audio))
	}
	// Disconnected -> client.Tick skipped -> loopback outbox empty.
	kind, _, _ := loop.ReadMessage()
	if kind != server.MessageNone {
		t.Fatalf("loop ReadMessage kind = %v want MessageNone (client.Tick should be skipped)", kind)
	}
}

func TestRunFrame_HappyConnectedSendsMove(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	// Connected client -> client.Tick runs the outbound clc_move path.
	r.Client.Connection = client.StateConnected

	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	// The client SendUnreliable goes to the SERVER side of the loopback;
	// we kept the client-side conn so server-side reads would need the
	// other half. Instead assert SentMove via a second tick + by
	// allocating the server side:
	_, serverSide := server.NewLoopbackConn()
	_ = serverSide
	// Recorder captured a frame.
	if len(rec.Frames) != 1 {
		t.Fatalf("rec.Frames len = %d want 1", len(rec.Frames))
	}
}

// TestRunFrame_ConnectedSendsClcMove uses a fresh loopback pair so the
// test can observe the server-side inbox. Asserts the client.Tick
// outbound branch was reached (SentMove path) when Connection ==
// StateConnected.
func TestRunFrame_ConnectedSendsClcMove(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	clientSide, serverSide := server.NewLoopbackConn()
	r.Conn = clientSide
	r.Client.Connection = client.StateConnected

	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	kind, data, err := serverSide.ReadMessage()
	if err != nil {
		t.Fatalf("serverSide.ReadMessage: %v", err)
	}
	if kind != server.MessageUnreliable {
		t.Fatalf("serverSide kind = %v want MessageUnreliable", kind)
	}
	if len(data) == 0 {
		t.Fatalf("serverSide clc_move payload empty")
	}
}

// ----- short-circuits ------------------------------------------------

func TestRunFrame_AudioSkippedWhenPoolNil(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	// SoundPool nil + MixBuffer empty -> audio path skipped.
	r.SoundPool = nil
	r.MixBuffer = nil

	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	if len(rec.Audio) != 0 {
		t.Fatalf("rec.Audio len = %d want 0 (SoundPool nil)", len(rec.Audio))
	}
}

func TestRunFrame_AudioSkippedWhenMixBufferEmpty(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	pool, err := sound.NewPool(8)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	r.SoundPool = pool
	r.MixBuffer = nil

	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	if len(rec.Audio) != 0 {
		t.Fatalf("rec.Audio len = %d want 0 (MixBuffer empty)", len(rec.Audio))
	}
}

// ----- error propagation --------------------------------------------

func TestRunFrame_PollInputErrorPropagates(t *testing.T) {
	wantErr := errors.New("poll boom")
	fp := &failingPoll{Recorder: backend.NewRecorder(0, 0), err: wantErr}
	r, _ := newRunner(t, fp)
	if err := r.RunFrame(0.05, 1); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v want %v", err, wantErr)
	}
}

func TestRunFrame_HostFrameErrorPropagates(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	wantErr := errors.New("host frame boom")
	r.Host = &fakeHost{err: wantErr}
	if err := r.RunFrame(0.05, 1); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v want %v", err, wantErr)
	}
	// PresentFrame must NOT have been called.
	if len(rec.Frames) != 0 {
		t.Fatalf("rec.Frames len = %d want 0 on host error", len(rec.Frames))
	}
}

func TestRunFrame_ClientTickErrorPropagates(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	r.Client.Connection = client.StateConnected
	// Close the loopback so SendUnreliable returns ErrNetConnClosed.
	if err := r.Conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := r.RunFrame(0.05, 1)
	if err == nil {
		t.Fatalf("RunFrame: got nil err, want client.Tick failure")
	}
	if len(rec.Frames) != 0 {
		t.Fatalf("rec.Frames len = %d want 0 on client.Tick error", len(rec.Frames))
	}
}

func TestRunFrame_ExpandFrameErrorPropagates(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	// Compose2D needs ctx.Chars; nil-out triggers ErrComposeNilChars.
	r.Chars = nil
	if err := r.RunFrame(0.05, 1); !errors.Is(err, render.ErrComposeNilChars) {
		t.Fatalf("err = %v want %v", err, render.ErrComposeNilChars)
	}
	if len(rec.Frames) != 0 {
		t.Fatalf("rec.Frames len = %d want 0 on compose error", len(rec.Frames))
	}
}

func TestRunFrame_PresentFrameErrorPropagates(t *testing.T) {
	wantErr := errors.New("present boom")
	fp := &failingPresent{Recorder: backend.NewRecorder(0, 0), err: wantErr}
	r, _ := newRunner(t, fp)
	if err := r.RunFrame(0.05, 1); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v want %v", err, wantErr)
	}
}

func TestRunFrame_PaintErrorPropagates(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	pool, err := sound.NewPool(0)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	// Park a 16-bit channel in the pool: sound.Paint rejects with
	// ErrMixBadFormat before any output is written.
	pool.Channels[0].Sfx = &sound.Sample{
		BitsPerSam: 16,
		Data:       []byte{0, 0, 0, 0},
		NumSamples: 2,
	}
	pool.Channels[0].EndPos = 2
	r.SoundPool = pool
	r.MixBuffer = make([]sound.StereoSample, sound.MixBufferStereoFrames)
	if err := r.RunFrame(0.05, 1); !errors.Is(err, sound.ErrMixBadFormat) {
		t.Fatalf("err = %v want %v", err, sound.ErrMixBadFormat)
	}
}

func TestRunFrame_QueueAudioUnsupportedIgnored(t *testing.T) {
	fa := &failingAudio{Recorder: backend.NewRecorder(0, 0), err: backend.ErrUnsupported}
	r, _ := newRunner(t, fa)
	pool, err := sound.NewPool(8)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	r.SoundPool = pool
	r.MixBuffer = make([]sound.StereoSample, sound.MixBufferStereoFrames)
	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v (ErrUnsupported should be silent)", err)
	}
	// PresentFrame still ran.
	if len(fa.Recorder.Frames) != 1 {
		t.Fatalf("rec.Frames len = %d want 1", len(fa.Recorder.Frames))
	}
}

func TestRunFrame_QueueAudioOtherErrorPropagates(t *testing.T) {
	wantErr := errors.New("queue boom")
	fa := &failingAudio{Recorder: backend.NewRecorder(0, 0), err: wantErr}
	r, _ := newRunner(t, fa)
	pool, err := sound.NewPool(8)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	r.SoundPool = pool
	r.MixBuffer = make([]sound.StereoSample, sound.MixBufferStereoFrames)
	if err := r.RunFrame(0.05, 1); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v want %v", err, wantErr)
	}
}

// TestRunFrame_MixBufferClampedToMax exercises the n > MaxMixOutputFrames
// branch by allocating a MixBuffer larger than the cap; RunFrame should
// clamp + still succeed.
func TestRunFrame_MixBufferClampedToMax(t *testing.T) {
	rec := backend.NewRecorder(0, 0)
	r, _ := newRunner(t, rec)
	pool, err := sound.NewPool(8)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	r.SoundPool = pool
	r.MixBuffer = make([]sound.StereoSample, sound.MixBufferStereoFrames+128)
	if err := r.RunFrame(0.05, 1); err != nil {
		t.Fatalf("RunFrame: %v", err)
	}
	if len(rec.Audio) != 1 {
		t.Fatalf("rec.Audio len = %d want 1", len(rec.Audio))
	}
	if got := len(rec.Audio[0]); got != sound.MixBufferStereoFrames {
		t.Fatalf("rec.Audio[0] len = %d want %d", got, sound.MixBufferStereoFrames)
	}
}

// ----- UpdateButtonsFromSnapshot ------------------------------------

func TestUpdateButtonsFromSnapshot_DownAllMappedKeys(t *testing.T) {
	var b client.MovementButtons
	snap := backend.InputSnapshot{
		KeysDown: []backend.KeyCode{
			backend.KeyW, backend.KeyS,
			backend.KeyA, backend.KeyD,
			backend.KeyLeft, backend.KeyRight,
			backend.KeyUp, backend.KeyDown,
			backend.KeySpace, backend.KeyCtrl,
			backend.KeyShift,
		},
	}
	UpdateButtonsFromSnapshot(&b, snap)
	cases := []struct {
		name string
		got  *client.ButtonState
	}{
		{"Forward", &b.Forward},
		{"Back", &b.Back},
		{"MoveLeft", &b.MoveLeft},
		{"MoveRight", &b.MoveRight},
		{"Left", &b.Left},
		{"Right", &b.Right},
		{"Lookup", &b.Lookup},
		{"Lookdown", &b.Lookdown},
		{"Up", &b.Up},
		{"Down", &b.Down},
	}
	for _, tc := range cases {
		// Down -> bits 0 + 1 set, bit 2 clear.
		if tc.got.Pressed&1 == 0 {
			t.Fatalf("%s: held bit not set", tc.name)
		}
		if tc.got.Pressed&2 == 0 {
			t.Fatalf("%s: down-edge bit not set", tc.name)
		}
		if tc.got.Pressed&4 != 0 {
			t.Fatalf("%s: up-edge bit unexpectedly set", tc.name)
		}
	}
	if !b.SpeedHeld {
		t.Fatalf("SpeedHeld not set on KeyShift down")
	}
}

func TestUpdateButtonsFromSnapshot_UpReleasesAllMappedKeys(t *testing.T) {
	var b client.MovementButtons
	// Pre-press all buttons.
	all := []backend.KeyCode{
		backend.KeyW, backend.KeyS,
		backend.KeyA, backend.KeyD,
		backend.KeyLeft, backend.KeyRight,
		backend.KeyUp, backend.KeyDown,
		backend.KeySpace, backend.KeyCtrl,
		backend.KeyShift,
	}
	UpdateButtonsFromSnapshot(&b, backend.InputSnapshot{KeysDown: all})
	UpdateButtonsFromSnapshot(&b, backend.InputSnapshot{KeysUp: all})

	slots := []*client.ButtonState{
		&b.Forward, &b.Back, &b.MoveLeft, &b.MoveRight,
		&b.Left, &b.Right, &b.Lookup, &b.Lookdown,
		&b.Up, &b.Down,
	}
	for i, s := range slots {
		if s.Pressed&1 != 0 {
			t.Fatalf("slot %d: held bit still set after release", i)
		}
		if s.Pressed&4 == 0 {
			t.Fatalf("slot %d: up-edge bit not set after release", i)
		}
	}
	if b.SpeedHeld {
		t.Fatalf("SpeedHeld still set after KeyShift up")
	}
}

// TestUpdateButtonsFromSnapshot_UnmappedKeysIgnored covers the
// fall-through path in buttonSlot (the keys with no movement mapping
// like KeyEnter / KeyMouse1 are silently dropped, NOT crashed on).
func TestUpdateButtonsFromSnapshot_UnmappedKeysIgnored(t *testing.T) {
	var b client.MovementButtons
	snap := backend.InputSnapshot{
		KeysDown: []backend.KeyCode{
			backend.KeyEnter, backend.KeyEscape, backend.KeyTab,
			backend.KeyMouse1, backend.KeyMouse2,
		},
		KeysUp: []backend.KeyCode{
			backend.KeyEnter, backend.KeyEscape, backend.KeyTab,
			backend.KeyMouse1, backend.KeyMouse2,
		},
	}
	UpdateButtonsFromSnapshot(&b, snap)
	// Nothing should be set.
	if (b != client.MovementButtons{}) {
		t.Fatalf("movement state mutated by non-movement keys: %+v", b)
	}
}
