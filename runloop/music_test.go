// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package runloop

import (
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/music"
	"github.com/go-quake1/engine/sound"
)

// fakeMusicDecoder feeds a pre-canned float32 stream so tickMusic
// can be exercised end-to-end without an OGG fixture.
type fakeMusicDecoder struct {
	data     []float32
	pos      int
	rate     int
	channels int
}

func (f *fakeMusicDecoder) Read(p []float32) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	if f.pos >= len(f.data) {
		return n, io.EOF
	}
	return n, nil
}
func (f *fakeMusicDecoder) SampleRate() int { return f.rate }
func (f *fakeMusicDecoder) Channels() int   { return f.channels }

// --- saturatedAdd16 ---------------------------------------------------

func TestSaturatedAdd16_NoSaturation(t *testing.T) {
	if got := saturatedAdd16(100, 200); got != 300 {
		t.Errorf("100+200: got %d want 300", got)
	}
	if got := saturatedAdd16(-50, -50); got != -100 {
		t.Errorf("-50+-50: got %d want -100", got)
	}
	if got := saturatedAdd16(0, 0); got != 0 {
		t.Errorf("0+0: got %d want 0", got)
	}
}

func TestSaturatedAdd16_PositiveClamp(t *testing.T) {
	if got := saturatedAdd16(30000, 30000); got != 32767 {
		t.Errorf("30000+30000: got %d want 32767", got)
	}
}

func TestSaturatedAdd16_NegativeClamp(t *testing.T) {
	if got := saturatedAdd16(-30000, -30000); got != -32768 {
		t.Errorf("-30000+-30000: got %d want -32768", got)
	}
}

// --- logMusicMissingOnce ---------------------------------------------

func TestLogMusicMissingOnce_NilSinkNoOp(t *testing.T) {
	r := &Runner{} // no MusicMissingLog
	r.logMusicMissingOnce(2, 2)
	// Just ensure no panic; nothing observable.
}

func TestLogMusicMissingOnce_FiresOncePerPair(t *testing.T) {
	calls := 0
	r := &Runner{
		MusicMissingLog: func(track int) { calls++ },
	}
	r.logMusicMissingOnce(2, 2)
	r.logMusicMissingOnce(2, 2) // duplicate, deduped
	r.logMusicMissingOnce(2, 2)
	if calls != 1 {
		t.Errorf("calls: got %d want 1 (same pair deduped)", calls)
	}
	r.logMusicMissingOnce(3, 3) // distinct pair, fires
	r.logMusicMissingOnce(3, 5) // distinct LoopTrack, fires
	if calls != 3 {
		t.Errorf("after distinct pairs: got %d want 3", calls)
	}
}

// --- tickMusic --------------------------------------------------------

func TestTickMusic_NilClientNoOp(t *testing.T) {
	r := &Runner{}
	r.tickMusic(make([]sound.StereoSample, 4))
	// No crash.
}

func TestTickMusic_NilOpenNoOp(t *testing.T) {
	r := &Runner{
		Client:       client.NewState(),
		MusicDecoder: music.NewVorbisDecoder,
	}
	r.tickMusic(make([]sound.StereoSample, 4))
}

func TestTickMusic_NilDecoderNoOp(t *testing.T) {
	r := &Runner{
		Client:    client.NewState(),
		MusicOpen: func(int) ([]byte, bool) { return []byte{1}, true },
	}
	r.tickMusic(make([]sound.StereoSample, 4))
}

func TestTickMusic_EpochUnchangedNoOp(t *testing.T) {
	// Both MusicEpoch and musicEpochSeen are zero; track is also
	// zero -> driver opens nothing AND musicEpochSeen stays at zero
	// (since the comparison passes only when they differ).
	calls := 0
	r := &Runner{
		Client:       client.NewState(),
		MusicOpen:    func(int) ([]byte, bool) { calls++; return []byte{1}, true },
		MusicDecoder: func([]byte) (music.Decoder, error) { return nil, errors.New("nope") },
	}
	r.tickMusic(make([]sound.StereoSample, 4))
	if calls != 0 {
		t.Errorf("OpenFunc calls: got %d want 0 (no epoch advance)", calls)
	}
}

func TestTickMusic_EpochAdvanceWithZeroTrackJustTearsDown(t *testing.T) {
	r := &Runner{
		Client:       client.NewState(),
		MusicOpen:    func(int) ([]byte, bool) { return []byte{1}, true },
		MusicDecoder: func([]byte) (music.Decoder, error) { return &fakeMusicDecoder{}, nil },
	}
	r.Client.MusicEpoch = 1
	r.Client.MusicTrack = 0
	r.tickMusic(make([]sound.StereoSample, 4))
	if r.musicEpochSeen != 1 {
		t.Errorf("musicEpochSeen: got %d want 1", r.musicEpochSeen)
	}
	if r.musicStreamer != nil {
		t.Errorf("musicStreamer: got non-nil want nil for track==0")
	}
}

func TestTickMusic_OpenAndMix(t *testing.T) {
	// Decoder produces stereo full-scale; mix must add into out.
	data := []float32{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5} // 4 stereo frames
	r := &Runner{
		Client:    client.NewState(),
		MusicOpen: func(track int) ([]byte, bool) { return []byte{1}, true },
		MusicDecoder: func([]byte) (music.Decoder, error) {
			return &fakeMusicDecoder{data: data, rate: 22050, channels: 2}, nil
		},
		MusicVolume: 1.0,
	}
	r.Client.MusicEpoch = 1
	r.Client.MusicTrack = 5
	r.Client.MusicLoopTrack = 0

	out := make([]sound.StereoSample, 4)
	// Seed out with some SFX-side values to prove tickMusic ADDs.
	for i := range out {
		out[i].L = 100
		out[i].R = -200
	}
	r.tickMusic(out)
	// 0.5 * 32767 = 16383; plus 100 -> 16483 on L, plus -200 -> 16183 on R.
	if out[0].L < 16000 || out[0].L > 17000 {
		t.Errorf("out[0].L: got %d want ~16483", out[0].L)
	}
	if out[0].R < 15000 || out[0].R > 17000 {
		t.Errorf("out[0].R: got %d want ~16183", out[0].R)
	}
	if r.musicStreamer == nil {
		t.Errorf("musicStreamer: got nil want non-nil after first epoch advance")
	}
}

func TestTickMusic_TrackMissingLogsAndStaysSilent(t *testing.T) {
	logged := 0
	r := &Runner{
		Client:          client.NewState(),
		MusicOpen:       func(track int) ([]byte, bool) { return nil, false },
		MusicDecoder:    func([]byte) (music.Decoder, error) { return &fakeMusicDecoder{}, nil },
		MusicMissingLog: func(track int) { logged++ },
	}
	r.Client.MusicEpoch = 1
	r.Client.MusicTrack = 2
	r.Client.MusicLoopTrack = 2

	out := make([]sound.StereoSample, 4)
	r.tickMusic(out)
	if logged != 1 {
		t.Errorf("logged: got %d want 1", logged)
	}
	if r.musicStreamer != nil {
		t.Errorf("musicStreamer: got non-nil want nil after track missing")
	}
	// A SECOND epoch with the same (track, loopTrack) does not re-log.
	r.Client.MusicEpoch = 2
	r.tickMusic(out)
	if logged != 1 {
		t.Errorf("logged after repeat: got %d want 1 (dedup)", logged)
	}
}

func TestTickMusic_DecoderFactoryErrorAlsoLogsAndStaysSilent(t *testing.T) {
	logged := 0
	r := &Runner{
		Client:          client.NewState(),
		MusicOpen:       func(track int) ([]byte, bool) { return []byte{1}, true },
		MusicDecoder:    func([]byte) (music.Decoder, error) { return nil, errors.New("bogus") },
		MusicMissingLog: func(track int) { logged++ },
	}
	r.Client.MusicEpoch = 1
	r.Client.MusicTrack = 3
	out := make([]sound.StereoSample, 4)
	r.tickMusic(out)
	if logged != 1 {
		t.Errorf("logged: got %d want 1", logged)
	}
	if r.musicStreamer != nil {
		t.Errorf("musicStreamer: got non-nil want nil after decoder error")
	}
}

func TestTickMusic_StoppedStreamerSkippedAfterEOF(t *testing.T) {
	// Decoder yields a few samples, then EOF; with loopTrack==0 the
	// streamer stops. A second tickMusic (same epoch) is a no-op
	// (the dispatcher only re-opens on epoch advance).
	r := &Runner{
		Client:    client.NewState(),
		MusicOpen: func(track int) ([]byte, bool) { return []byte{1}, true },
		MusicDecoder: func([]byte) (music.Decoder, error) {
			return &fakeMusicDecoder{data: []float32{0.5, 0.5}, rate: 8000, channels: 1}, nil
		},
		MusicVolume: 1.0,
	}
	r.Client.MusicEpoch = 1
	r.Client.MusicTrack = 1

	out := make([]sound.StereoSample, 4)
	r.tickMusic(out) // consume the 2 samples then EOF
	if r.musicStreamer == nil || !r.musicStreamer.Stopped() {
		t.Fatalf("expected streamer stopped after EOF; streamer=%v stopped=%v",
			r.musicStreamer != nil, r.musicStreamer != nil && r.musicStreamer.Stopped())
	}
	// Second call -- same epoch, stopped streamer -> early exit.
	for i := range out {
		out[i] = sound.StereoSample{L: 1, R: 1}
	}
	r.tickMusic(out)
	for i := range out {
		if out[i].L != 1 || out[i].R != 1 {
			t.Errorf("out[%d]: got (%d, %d) want (1, 1) (no mix expected)",
				i, out[i].L, out[i].R)
		}
	}
}
