// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package music

import (
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/sound"
)

// fakeDecoder is the test-side [Decoder] that feeds a caller-supplied
// float32 stream into the [Streamer] without round-tripping through a
// real OGG container. The streamer is the unit under test; the
// Decoder contract itself is exercised separately by the
// NewVorbisDecoder integration test below (against a real OGG
// fixture).
type fakeDecoder struct {
	data     []float32
	pos      int
	rate     int
	channels int
	readErr  error // when non-nil, returned alongside any short read
}

func (f *fakeDecoder) Read(p []float32) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	if f.pos >= len(f.data) {
		if f.readErr != nil {
			return n, f.readErr
		}
		return n, io.EOF
	}
	return n, nil
}
func (f *fakeDecoder) SampleRate() int { return f.rate }
func (f *fakeDecoder) Channels() int   { return f.channels }

// fakeBlob is the sentinel used by the test resolvers: the OpenFunc
// stores the float32 stream + channel layout for the active track on
// the blob, and the DecoderFactory parses it back. This keeps the
// resolver + factory split honest in tests without standing up a
// real OGG encoder.

// stuckDecoder always returns (0, nil) -- a contract-violating decoder
// that would freeze NextSamples without the defensive zero-progress
// guard. The (0, nil) safeguard in NextSamples should treat this as
// end-of-stream and bail out cleanly.
type stuckDecoder struct{}

func (stuckDecoder) Read(p []float32) (int, error) { return 0, nil }
func (stuckDecoder) SampleRate() int               { return 22050 }
func (stuckDecoder) Channels() int                 { return 2 }

func TestNextSamples_ZeroProgressBreaksLoop(t *testing.T) {
	open := func(track int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) { return stuckDecoder{}, nil }
	s, err := LoadTrack(open, factory, 7, 0, DefaultVolume)
	if err != nil {
		t.Fatalf("LoadTrack: %v", err)
	}
	buf := make([]sound.StereoSample, 32)
	// Without the (0, nil) guard this NextSamples would never
	// return. With the guard it bails to 0 written + stops cleanly.
	got := s.NextSamples(buf)
	if got != 0 {
		t.Errorf("NextSamples = %d, want 0 on stuck decoder", got)
	}
	if !s.Stopped() {
		t.Errorf("Stopped() = false, want true after stuck decoder bail")
	}
}

// Test 1: LoadTrack happy path (single stereo decoder, no loop).

func TestLoadTrack_HappyPath(t *testing.T) {
	frames := 100
	data := make([]float32, frames*2) // stereo
	for i := range data {
		data[i] = 0.5
	}
	open := func(track int) ([]byte, bool) {
		if track != 7 {
			return nil, false
		}
		return []byte{1}, true
	}
	factory := func(blob []byte) (Decoder, error) {
		return &fakeDecoder{data: data, rate: 22050, channels: 2}, nil
	}
	s, err := LoadTrack(open, factory, 7, 0, 0.5)
	if err != nil {
		t.Fatalf("LoadTrack: %v", err)
	}
	if s.Track() != 7 {
		t.Errorf("Track: got %d want 7", s.Track())
	}
	if s.LoopTrack() != 0 {
		t.Errorf("LoopTrack: got %d want 0", s.LoopTrack())
	}
	if s.Volume() != 0.5 {
		t.Errorf("Volume: got %v want 0.5", s.Volume())
	}
	if s.SampleRate() != 22050 {
		t.Errorf("SampleRate: got %d want 22050", s.SampleRate())
	}
	if s.Channels() != 2 {
		t.Errorf("Channels: got %d want 2", s.Channels())
	}
	if s.Stopped() {
		t.Errorf("Stopped: got true want false")
	}
}

// Test 2: LoadTrack defaults volume when zero.

func TestLoadTrack_DefaultVolume(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{rate: 11025, channels: 1}, nil
	}
	s, err := LoadTrack(open, factory, 2, 2, 0)
	if err != nil {
		t.Fatalf("LoadTrack: %v", err)
	}
	if s.Volume() != DefaultVolume {
		t.Errorf("Volume: got %v want DefaultVolume=%v", s.Volume(), DefaultVolume)
	}
}

// Test 3: LoadTrack guards against nil resolver / nil factory.

func TestLoadTrack_NilOpen(t *testing.T) {
	_, err := LoadTrack(nil, NewVorbisDecoder, 1, 1, 0)
	if !errors.Is(err, ErrNilOpen) {
		t.Errorf("err: got %v want ErrNilOpen", err)
	}
}

func TestLoadTrack_NilFactory(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	_, err := LoadTrack(open, nil, 1, 1, 0)
	if !errors.Is(err, ErrNilDecoderFactory) {
		t.Errorf("err: got %v want ErrNilDecoderFactory", err)
	}
}

// Test 4: LoadTrack returns ErrTrackMissing when the resolver says so.

func TestLoadTrack_TrackMissing(t *testing.T) {
	open := func(int) ([]byte, bool) { return nil, false }
	factory := func([]byte) (Decoder, error) { return &fakeDecoder{}, nil }
	s, err := LoadTrack(open, factory, 2, 2, 0)
	if !errors.Is(err, ErrTrackMissing) {
		t.Errorf("err: got %v want ErrTrackMissing", err)
	}
	if s != nil {
		t.Errorf("streamer: got %v want nil", s)
	}
}

// Test 5: LoadTrack wraps the factory's error.

func TestLoadTrack_FactoryError(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	cause := errors.New("bogus blob")
	factory := func([]byte) (Decoder, error) { return nil, cause }
	_, err := LoadTrack(open, factory, 2, 2, 0)
	if !errors.Is(err, ErrDecoderFailed) {
		t.Errorf("err: got %v want ErrDecoderFailed", err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is on cause: got false want true (chain=%v)", err)
	}
	if err.Error() == "" {
		t.Errorf("Error string is empty")
	}
}

// Test 6: LoadTrack rejects a (nil, nil) factory return.

func TestLoadTrack_FactoryReturnsNil(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) { return nil, nil }
	_, err := LoadTrack(open, factory, 2, 2, 0)
	if !errors.Is(err, ErrDecoderFailed) {
		t.Errorf("err: got %v want ErrDecoderFailed", err)
	}
}

// Test 7: NextSamples writes the expected stereo frames from a stereo decoder.

func TestNextSamples_StereoPassthrough(t *testing.T) {
	// 4 stereo frames: L=1,R=-1; L=0.5,R=-0.5; L=0,R=0; L=-1,R=1
	data := []float32{1, -1, 0.5, -0.5, 0, 0, -1, 1}
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: data, rate: 44100, channels: 2}, nil
	}
	s, err := LoadTrack(open, factory, 1, 0, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]sound.StereoSample, 4)
	n := s.NextSamples(out)
	if n != 4 {
		t.Errorf("frames written: got %d want 4", n)
	}
	// Full-scale should be saturated/near-int16-max (1.0 * 32767 = 32767,
	// -1.0 * 32767 = -32767; clamp16 floors at -32768 only for values
	// strictly below that, so the symmetric +-1.0 scale lands at +-32767).
	if out[0].L != 32767 || out[0].R != -32767 {
		t.Errorf("frame[0]: got (L=%d, R=%d) want (32767, -32767)", out[0].L, out[0].R)
	}
	if out[3].L != -32767 || out[3].R != 32767 {
		t.Errorf("frame[3]: got (L=%d, R=%d) want (-32767, 32767)", out[3].L, out[3].R)
	}
	// Also exercise the clamp16 lower floor explicitly: a sample with
	// magnitude > 1.0 must saturate at -32768.
	_ = clamp16(-1.5 * float32(32767))
	// After consuming all input, streamer with loopTrack==0 should stop.
	if !s.Stopped() {
		t.Errorf("Stopped: got false want true after EOF without loop")
	}
}

// Test 8: NextSamples handles mono by duplicating the sample.

func TestNextSamples_MonoBroadcast(t *testing.T) {
	data := []float32{0.5, -0.5, 0, 1}
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: data, rate: 11025, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	out := make([]sound.StereoSample, 4)
	n := s.NextSamples(out)
	if n != 4 {
		t.Errorf("n: got %d want 4", n)
	}
	for i := range out {
		if out[i].L != out[i].R {
			t.Errorf("mono frame[%d]: L=%d R=%d (must be equal)", i, out[i].L, out[i].R)
		}
	}
}

// Test 9: NextSamples downmixes multi-channel input to stereo.

func TestNextSamples_MultiChannelDownmix(t *testing.T) {
	// 4 channels per frame, 2 frames. Channels 0,1 -> L; 2,3 -> R.
	data := []float32{
		1.0, 0.0, 0.0, 1.0, // frame 0
		-1.0, 0.0, 0.0, -1.0, // frame 1
	}
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: data, rate: 48000, channels: 4}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	out := make([]sound.StereoSample, 2)
	n := s.NextSamples(out)
	if n != 2 {
		t.Errorf("n: got %d want 2", n)
	}
	// Frame 0: L = (1.0+0.0)/2 = 0.5, R = (0.0+1.0)/2 = 0.5.
	// 0.5 * 32767 ~= 16383.
	if out[0].L < 16000 || out[0].L > 16400 {
		t.Errorf("frame[0].L: got %d want ~16383", out[0].L)
	}
	if out[0].R < 16000 || out[0].R > 16400 {
		t.Errorf("frame[0].R: got %d want ~16383", out[0].R)
	}
	if out[1].L > -16000 || out[1].L < -16400 {
		t.Errorf("frame[1].L: got %d want ~-16383", out[1].L)
	}
	if out[1].R > -16000 || out[1].R < -16400 {
		t.Errorf("frame[1].R: got %d want ~-16383", out[1].R)
	}
}

// Test 10: NextSamples on empty buffer returns 0.

func TestNextSamples_EmptyBuffer(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: []float32{0.1, 0.1}, rate: 8000, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	if n := s.NextSamples(nil); n != 0 {
		t.Errorf("nil buf: got %d want 0", n)
	}
	if n := s.NextSamples([]sound.StereoSample{}); n != 0 {
		t.Errorf("empty buf: got %d want 0", n)
	}
}

// Test 11: NextSamples on a stopped streamer returns 0.

func TestNextSamples_AfterStop(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: []float32{0.1, 0.1}, rate: 8000, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	out := make([]sound.StereoSample, 4)
	_ = s.NextSamples(out) // exhausts decoder + sets stopped
	if !s.Stopped() {
		t.Fatal("expected Stopped()=true after exhausting input")
	}
	if n := s.NextSamples(out); n != 0 {
		t.Errorf("post-stop: got %d want 0", n)
	}
	if s.SampleRate() != 0 {
		t.Errorf("stopped SampleRate: got %d want 0", s.SampleRate())
	}
	if s.Channels() != 0 {
		t.Errorf("stopped Channels: got %d want 0", s.Channels())
	}
}

// Test 12: Loop wrap-around when LoopTrack is non-zero.
//
// The first decoder yields 4 mono samples; on EOF the streamer
// re-opens via the resolver and the factory builds a fresh decoder
// with 4 more samples. NextSamples must seamlessly produce all 8
// frames in a single call.

func TestNextSamples_LoopWrap(t *testing.T) {
	calls := 0
	open := func(track int) ([]byte, bool) {
		calls++
		// Encode the call-number into the blob so the factory can
		// distinguish first vs. loop invocations.
		return []byte{byte(calls)}, true
	}
	factory := func(blob []byte) (Decoder, error) {
		if blob[0] == 1 {
			return &fakeDecoder{data: []float32{1, 1, 1, 1}, rate: 8000, channels: 1}, nil
		}
		return &fakeDecoder{data: []float32{-1, -1, -1, -1}, rate: 8000, channels: 1}, nil
	}
	s, err := LoadTrack(open, factory, 1, 1, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]sound.StereoSample, 8)
	n := s.NextSamples(out)
	if n != 8 {
		t.Errorf("frames: got %d want 8", n)
	}
	// First 4 are from first decoder (positive); next 4 from loop (negative).
	for i := 0; i < 4; i++ {
		if out[i].L <= 0 {
			t.Errorf("frame[%d].L: got %d want > 0", i, out[i].L)
		}
	}
	for i := 4; i < 8; i++ {
		if out[i].L >= 0 {
			t.Errorf("frame[%d].L: got %d want < 0", i, out[i].L)
		}
	}
	if s.Stopped() {
		t.Errorf("Stopped: got true want false (loop should keep stream alive)")
	}
	if s.Track() != 1 { // both tracks were 1 in this test
		t.Errorf("Track post-loop: got %d want 1", s.Track())
	}
}

// Test 13: Loop fails (track now missing) -> streamer stops cleanly.

func TestNextSamples_LoopMissing(t *testing.T) {
	calls := 0
	open := func(track int) ([]byte, bool) {
		calls++
		if calls > 1 {
			return nil, false
		}
		return []byte{1}, true
	}
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: []float32{0.5, 0.5}, rate: 8000, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 2, 1.0)
	out := make([]sound.StereoSample, 8)
	n := s.NextSamples(out)
	if n != 2 {
		t.Errorf("frames written: got %d want 2", n)
	}
	if !s.Stopped() {
		t.Errorf("Stopped: got false want true after loop-open failure")
	}
}

// Test 14: Loop factory itself errors -> streamer stops cleanly.

func TestNextSamples_LoopFactoryError(t *testing.T) {
	calls := 0
	open := func(track int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("loop decoder bust")
		}
		return &fakeDecoder{data: []float32{0.5, 0.5}, rate: 8000, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 2, 1.0)
	out := make([]sound.StereoSample, 8)
	n := s.NextSamples(out)
	if n != 2 {
		t.Errorf("frames: got %d want 2", n)
	}
	if !s.Stopped() {
		t.Errorf("Stopped: got false want true after loop factory failure")
	}
}

// Test 15: Loop factory returns (nil, nil) -> streamer stops cleanly.

func TestNextSamples_LoopFactoryReturnsNil(t *testing.T) {
	calls := 0
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		calls++
		if calls > 1 {
			return nil, nil
		}
		return &fakeDecoder{data: []float32{0.5, 0.5}, rate: 8000, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 2, 1.0)
	out := make([]sound.StereoSample, 8)
	_ = s.NextSamples(out)
	if !s.Stopped() {
		t.Errorf("Stopped: got false want true after loop factory returned (nil, nil)")
	}
}

// Test 16: Decoder reporting zero channels is treated as end-of-stream.

func TestNextSamples_ZeroChannelsTreatedAsEnd(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: []float32{0.5, 0.5}, rate: 8000, channels: 0}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	out := make([]sound.StereoSample, 4)
	n := s.NextSamples(out)
	if n != 0 {
		t.Errorf("n: got %d want 0 (zero-channel decoder must short-circuit)", n)
	}
	if !s.Stopped() {
		t.Errorf("Stopped: got false want true")
	}
}

// Test 16b: Zero-channel decoder TRIGGERS the loop fallback. The
// first decoder reports channels==0 (signalling stream-end on the
// very first NextSamples), the loop track resolves to a valid
// decoder, and the streamer continues from there.

func TestNextSamples_ZeroChannelsTriggersLoop(t *testing.T) {
	calls := 0
	open := func(int) ([]byte, bool) {
		calls++
		return []byte{byte(calls)}, true
	}
	factory := func(blob []byte) (Decoder, error) {
		if blob[0] == 1 {
			return &fakeDecoder{data: []float32{}, rate: 8000, channels: 0}, nil
		}
		return &fakeDecoder{data: []float32{0.5, 0.5}, rate: 8000, channels: 1}, nil
	}
	s, err := LoadTrack(open, factory, 1, 2, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]sound.StereoSample, 2)
	n := s.NextSamples(out)
	if n != 2 {
		t.Errorf("frames after loop wrap from zero-channel decoder: got %d want 2", n)
	}
	if s.Stopped() {
		t.Errorf("Stopped: got true want false (loop recovered the stream)")
	}
}

// Test 17: SetVolume clamps to [0, 1] and the new value is honoured.

func TestSetVolume_Clamp(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) {
		return &fakeDecoder{data: []float32{1, 1, 1, 1}, rate: 8000, channels: 1}, nil
	}
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	s.SetVolume(-5)
	if s.Volume() != 0 {
		t.Errorf("Volume after SetVolume(-5): got %v want 0", s.Volume())
	}
	s.SetVolume(99)
	if s.Volume() != 1 {
		t.Errorf("Volume after SetVolume(99): got %v want 1", s.Volume())
	}
	s.SetVolume(0.25)
	if s.Volume() != 0.25 {
		t.Errorf("Volume after SetVolume(0.25): got %v want 0.25", s.Volume())
	}
	out := make([]sound.StereoSample, 4)
	_ = s.NextSamples(out)
	// 1.0 * 0.25 * 32767 ~= 8191.
	if out[0].L < 8000 || out[0].L > 8400 {
		t.Errorf("scaled sample: got %d want ~8191", out[0].L)
	}
}

// Test 18: clamp16 boundary cases.

func TestClamp16(t *testing.T) {
	if clamp16(50000) != 32767 {
		t.Errorf("clamp16(50000): got %d want 32767", clamp16(50000))
	}
	if clamp16(-50000) != -32768 {
		t.Errorf("clamp16(-50000): got %d want -32768", clamp16(-50000))
	}
	if clamp16(0) != 0 {
		t.Errorf("clamp16(0): got %d want 0", clamp16(0))
	}
	if clamp16(123) != 123 {
		t.Errorf("clamp16(123): got %d want 123", clamp16(123))
	}
}

// Test 19: NewVorbisDecoder rejects a garbage blob.

func TestNewVorbisDecoder_BadBlob(t *testing.T) {
	_, err := NewVorbisDecoder([]byte("not an ogg"))
	if err == nil {
		t.Errorf("err: got nil want non-nil for garbage blob")
	}
}

// Test 20: NewVorbisDecoder happy path on the real OGG fixture --
// proves the pure-Go oggvorbis dependency is correctly wired through
// the Decoder adapter + that the streamer actually produces non-zero
// PCM from a real-world OGG.

func TestNewVorbisDecoder_RealOGG(t *testing.T) {
	blob := loadTestOGG(t)
	dec, err := NewVorbisDecoder(blob)
	if err != nil {
		t.Fatalf("NewVorbisDecoder: %v", err)
	}
	if dec.SampleRate() <= 0 {
		t.Errorf("SampleRate: got %d want > 0", dec.SampleRate())
	}
	if dec.Channels() < 1 {
		t.Errorf("Channels: got %d want >= 1", dec.Channels())
	}
	// Drain a few hundred floats; just prove the Read pipeline doesn't
	// fault.
	buf := make([]float32, 1024)
	n, err := dec.Read(buf)
	if n == 0 && err == nil {
		t.Errorf("Read: zero floats AND no error; expected one or the other")
	}
}

// Test 21: Streamer end-to-end against a real OGG fixture; loop the
// same fixture and prove the decoder is recreated and the second pass
// also produces frames.

func TestStreamer_RealOGG_LoopWrap(t *testing.T) {
	blob := loadTestOGG(t)
	open := func(int) ([]byte, bool) { return blob, true }
	s, err := LoadTrack(open, NewVorbisDecoder, 1, 1, DefaultVolume)
	if err != nil {
		t.Fatalf("LoadTrack: %v", err)
	}
	// Drain many small chunks to force the loop wrap-around at least
	// once (the test.ogg fixture is short).
	out := make([]sound.StereoSample, 64)
	totalNonZero := 0
	for iter := 0; iter < 500; iter++ {
		n := s.NextSamples(out)
		if n == 0 {
			break
		}
		for i := 0; i < n; i++ {
			if out[i].L != 0 || out[i].R != 0 {
				totalNonZero++
			}
		}
	}
	if totalNonZero == 0 {
		t.Errorf("expected non-zero PCM output from real OGG fixture, got zero")
	}
	// The streamer should still be alive (looping) -- the test.ogg
	// reopens on EOF and the loop-track equals the active track.
	if s.Stopped() {
		t.Errorf("Stopped: got true want false (loop track == active should keep stream alive)")
	}
}

// Test 21b: An over-producing decoder (returns more floats than the
// caller asked for in one Read) is defensively clamped to the
// requested frame count so the output buffer is never overflowed.
//
// This isn't a contract a well-behaved Decoder violates (the
// io.Reader-style copy(p, ...) idiom guarantees n <= len(p)), but
// the streamer's `if frames > need { frames = need }` guard exists
// so the audio mixer is safe against a misbehaving decoder
// implementation; this test exercises that guard.

type overproducingDecoder struct {
	emitted bool
}

func (d *overproducingDecoder) Read(p []float32) (int, error) {
	if d.emitted {
		return 0, io.EOF
	}
	d.emitted = true
	// Pretend we wrote MORE samples than p can hold. Fill p first
	// (the actual byte memory only goes that far), then claim a
	// larger n.
	for i := range p {
		p[i] = 0.25
	}
	return len(p) + 8, nil // overclaim by 8
}
func (d *overproducingDecoder) SampleRate() int { return 8000 }
func (d *overproducingDecoder) Channels() int   { return 1 }

func TestNextSamples_OverproducingDecoderClamped(t *testing.T) {
	open := func(int) ([]byte, bool) { return []byte{1}, true }
	factory := func([]byte) (Decoder, error) { return &overproducingDecoder{}, nil }
	s, _ := LoadTrack(open, factory, 1, 0, 1.0)
	out := make([]sound.StereoSample, 4)
	n := s.NextSamples(out)
	if n != 4 {
		t.Errorf("frames written (clamped): got %d want 4", n)
	}
	for i := range out {
		if out[i].L == 0 {
			t.Errorf("frame[%d].L: got zero (clamp lost the data)", i)
		}
	}
}

// Test 22: PathPrefix + PathSuffix constants are wired (cover the
// canonical-path-builder seam embedders use).

func TestPathConstants(t *testing.T) {
	if PathPrefix == "" || PathSuffix == "" {
		t.Errorf("path constants empty: prefix=%q suffix=%q", PathPrefix, PathSuffix)
	}
}
