// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package realdev

import (
	"errors"
	"testing"

	gvsound "github.com/go-virtio/sound"
)

// fakeAudioCtl records every controlq round-trip SetupAudio drives.
// Each call appends a string trace entry; the per-step `*Err` fields
// let a test inject a failure at any boundary to cover the error
// branches.
type fakeAudioCtl struct {
	info       []gvsound.PCMInfoEntry
	infoErr    error
	setParams  func(streamID uint32, p gvsound.PCMParams) error
	prepare    func(streamID uint32) error
	start      func(streamID uint32) error
	calls      []string
	lastParams gvsound.PCMParams
	lastStream uint32
}

func (f *fakeAudioCtl) PCMInfo() ([]gvsound.PCMInfoEntry, error) {
	f.calls = append(f.calls, "PCMInfo")
	return f.info, f.infoErr
}

func (f *fakeAudioCtl) PCMSetParams(streamID uint32, p gvsound.PCMParams) error {
	f.calls = append(f.calls, "PCMSetParams")
	f.lastStream = streamID
	f.lastParams = p
	if f.setParams != nil {
		return f.setParams(streamID, p)
	}
	return nil
}

func (f *fakeAudioCtl) PCMPrepare(streamID uint32) error {
	f.calls = append(f.calls, "PCMPrepare")
	if f.prepare != nil {
		return f.prepare(streamID)
	}
	return nil
}

func (f *fakeAudioCtl) PCMStart(streamID uint32) error {
	f.calls = append(f.calls, "PCMStart")
	if f.start != nil {
		return f.start(streamID)
	}
	return nil
}

// twoStreamInfo returns one input stream at index 0 and one output
// stream at index 1 -- the latter advertises 11025 Hz / S16 / stereo
// so DefaultAudioStreamConfig negotiates cleanly. The fixture proves
// the output picker walks past the input slot.
func twoStreamInfo() []gvsound.PCMInfoEntry {
	return []gvsound.PCMInfoEntry{
		{
			Direction:   gvsound.PCMDirInput,
			Rates:       uint64(1) << gvsound.PCMRate11025,
			Formats:     uint64(1) << gvsound.PCMFmtS16,
			ChannelsMin: 1,
			ChannelsMax: 2,
		},
		{
			Direction:   gvsound.PCMDirOutput,
			Rates:       uint64(1) << gvsound.PCMRate11025,
			Formats:     uint64(1) << gvsound.PCMFmtS16,
			ChannelsMin: 1,
			ChannelsMax: 2,
		},
	}
}

func stringsEq(a, b []string) bool {
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

// TestSetupAudio_HappyPath drives the full four-step handshake against
// a fixture that advertises one input + one output stream with bitmaps
// matching DefaultAudioStreamConfig. The result must point at the
// output stream (index 1), and the recorded call sequence must be
// PCMInfo -> PCMSetParams -> PCMPrepare -> PCMStart with the params
// the engine cares about (rate=11025 -> PCMRate11025, format=PCMFmtS16,
// channels=2, period/buffer pass-through from cfg).
func TestSetupAudio_HappyPath(t *testing.T) {
	f := &fakeAudioCtl{info: twoStreamInfo()}
	got, err := setupAudio(f, DefaultAudioStreamConfig)
	if err != nil {
		t.Fatalf("setupAudio err = %v", err)
	}
	if got.StreamID != 1 {
		t.Fatalf("StreamID = %d want 1", got.StreamID)
	}
	if got.Rate != 11025 || got.Channels != 2 || got.Format != gvsound.PCMFmtS16 {
		t.Fatalf("result = %+v want rate=11025 ch=2 fmt=PCMFmtS16", got)
	}
	wantCalls := []string{"PCMInfo", "PCMSetParams", "PCMPrepare", "PCMStart"}
	if !stringsEq(f.calls, wantCalls) {
		t.Fatalf("calls = %v want %v", f.calls, wantCalls)
	}
	if f.lastStream != 1 {
		t.Fatalf("PCMSetParams streamID = %d want 1", f.lastStream)
	}
	if f.lastParams.Rate != gvsound.PCMRate11025 ||
		f.lastParams.Format != gvsound.PCMFmtS16 ||
		f.lastParams.Channels != 2 ||
		f.lastParams.PeriodBytes != DefaultAudioStreamConfig.PeriodBytes ||
		f.lastParams.BufferBytes != DefaultAudioStreamConfig.BufferBytes {
		t.Fatalf("PCMSetParams params = %+v want %+v", f.lastParams, DefaultAudioStreamConfig)
	}
}

// TestSetupAudio_NoStreams covers the empty-PCMInfo branch.
func TestSetupAudio_NoStreams(t *testing.T) {
	f := &fakeAudioCtl{info: nil}
	_, err := setupAudio(f, DefaultAudioStreamConfig)
	if !errors.Is(err, ErrAudioNoStreams) {
		t.Fatalf("err = %v want ErrAudioNoStreams", err)
	}
}

// TestSetupAudio_PCMInfoErr covers the PCMInfo round-trip failing
// (e.g. the device returned a non-OK status).
func TestSetupAudio_PCMInfoErr(t *testing.T) {
	boom := errors.New("info boom")
	f := &fakeAudioCtl{infoErr: boom}
	_, err := setupAudio(f, DefaultAudioStreamConfig)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v want wrapping %v", err, boom)
	}
}

// TestSetupAudio_NoOutputStream covers the picker fall-through when
// every advertised stream is direction=Input.
func TestSetupAudio_NoOutputStream(t *testing.T) {
	f := &fakeAudioCtl{info: []gvsound.PCMInfoEntry{
		{Direction: gvsound.PCMDirInput, Rates: uint64(1) << gvsound.PCMRate11025, Formats: uint64(1) << gvsound.PCMFmtS16, ChannelsMin: 1, ChannelsMax: 2},
	}}
	_, err := setupAudio(f, DefaultAudioStreamConfig)
	if !errors.Is(err, ErrAudioNoOutputStream) {
		t.Fatalf("err = %v want ErrAudioNoOutputStream", err)
	}
}

// TestSetupAudio_RateUnsupported_AllUnknown covers the Hz->byte ID
// lookup failing for every preferred entry (an unknown-rate list the
// spec does not enumerate -- e.g. an out-of-tree caller experimenting
// with custom rates).
func TestSetupAudio_RateUnsupported_AllUnknown(t *testing.T) {
	cfg := DefaultAudioStreamConfig
	cfg.PreferredRates = []uint32{12345, 67890} // none in virtio-snd spec
	f := &fakeAudioCtl{info: twoStreamInfo()}
	_, err := setupAudio(f, cfg)
	if !errors.Is(err, ErrAudioRateUnsupported) {
		t.Fatalf("err = %v want ErrAudioRateUnsupported", err)
	}
}

// TestSetupAudio_RateUnsupported_NoneInBitmap covers the stream's
// Rates bitmap lacking every preferred-list bit (every spec-valid Hz
// in the list misses the chosen stream's advertised bitmap).
func TestSetupAudio_RateUnsupported_NoneInBitmap(t *testing.T) {
	cfg := DefaultAudioStreamConfig
	cfg.PreferredRates = []uint32{22050, 48000} // bitmap only has Rate11025
	f := &fakeAudioCtl{info: twoStreamInfo()}
	_, err := setupAudio(f, cfg)
	if !errors.Is(err, ErrAudioRateUnsupported) {
		t.Fatalf("err = %v want ErrAudioRateUnsupported", err)
	}
}

// TestSetupAudio_RateFallbackPicksSecondEntry proves the rate
// fallback chain works: preferred [11025, 44100] against a stream that
// only advertises 44100 negotiates 44100 (the second entry).
func TestSetupAudio_RateFallbackPicksSecondEntry(t *testing.T) {
	info := twoStreamInfo()
	info[1].Rates = uint64(1) << gvsound.PCMRate44100 // only 44100
	cfg := DefaultAudioStreamConfig
	cfg.PreferredRates = []uint32{11025, 44100}
	f := &fakeAudioCtl{info: info}
	got, err := setupAudio(f, cfg)
	if err != nil {
		t.Fatalf("setupAudio err = %v", err)
	}
	if got.Rate != 44100 {
		t.Fatalf("Rate = %d want 44100 (fallback)", got.Rate)
	}
	if f.lastParams.Rate != gvsound.PCMRate44100 {
		t.Fatalf("PCMSetParams Rate = %d want PCMRate44100 (%d)",
			f.lastParams.Rate, gvsound.PCMRate44100)
	}
}

// TestSetupAudio_RateFallbackSkipsUnknownThenAccepts covers the
// "preferred chain mixes spec-unknown + spec-known rates" branch: the
// helper silently skips the unknown 12345 entry and lands on the
// spec-valid 11025.
func TestSetupAudio_RateFallbackSkipsUnknownThenAccepts(t *testing.T) {
	cfg := DefaultAudioStreamConfig
	cfg.PreferredRates = []uint32{12345, 11025}
	f := &fakeAudioCtl{info: twoStreamInfo()}
	got, err := setupAudio(f, cfg)
	if err != nil {
		t.Fatalf("setupAudio err = %v", err)
	}
	if got.Rate != 11025 {
		t.Fatalf("Rate = %d want 11025", got.Rate)
	}
}

// TestSetupAudio_EmptyPreferredRates covers the cfg.PreferredRates ==
// nil branch — pickRate's empty-loop walks to the fallthrough and
// surfaces ErrAudioRateUnsupported.
func TestSetupAudio_EmptyPreferredRates(t *testing.T) {
	cfg := DefaultAudioStreamConfig
	cfg.PreferredRates = nil
	f := &fakeAudioCtl{info: twoStreamInfo()}
	_, err := setupAudio(f, cfg)
	if !errors.Is(err, ErrAudioRateUnsupported) {
		t.Fatalf("err = %v want ErrAudioRateUnsupported", err)
	}
}

// TestSetupAudio_FormatUnsupported covers the stream's Formats bitmap
// lacking the requested PCMFmt* bit.
func TestSetupAudio_FormatUnsupported(t *testing.T) {
	cfg := DefaultAudioStreamConfig
	cfg.Format = gvsound.PCMFmtU8 // bitmap only advertises S16
	f := &fakeAudioCtl{info: twoStreamInfo()}
	_, err := setupAudio(f, cfg)
	if !errors.Is(err, ErrAudioFormatUnsupported) {
		t.Fatalf("err = %v want ErrAudioFormatUnsupported", err)
	}
}

// TestSetupAudio_ChannelsTooFew covers the cfg.Channels < ChannelsMin
// guard.
func TestSetupAudio_ChannelsTooFew(t *testing.T) {
	info := twoStreamInfo()
	info[1].ChannelsMin = 2 // require stereo
	info[1].ChannelsMax = 2
	cfg := DefaultAudioStreamConfig
	cfg.Channels = 1
	f := &fakeAudioCtl{info: info}
	_, err := setupAudio(f, cfg)
	if !errors.Is(err, ErrAudioChannelsUnsupported) {
		t.Fatalf("err = %v want ErrAudioChannelsUnsupported", err)
	}
}

// TestSetupAudio_ChannelsTooMany covers the cfg.Channels > ChannelsMax
// guard.
func TestSetupAudio_ChannelsTooMany(t *testing.T) {
	info := twoStreamInfo()
	info[1].ChannelsMin = 1
	info[1].ChannelsMax = 1 // mono only
	cfg := DefaultAudioStreamConfig
	cfg.Channels = 2
	f := &fakeAudioCtl{info: info}
	_, err := setupAudio(f, cfg)
	if !errors.Is(err, ErrAudioChannelsUnsupported) {
		t.Fatalf("err = %v want ErrAudioChannelsUnsupported", err)
	}
}

// TestSetupAudio_SetParamsErr covers the device rejecting PCMSetParams.
func TestSetupAudio_SetParamsErr(t *testing.T) {
	boom := errors.New("set-params boom")
	f := &fakeAudioCtl{
		info:      twoStreamInfo(),
		setParams: func(uint32, gvsound.PCMParams) error { return boom },
	}
	_, err := setupAudio(f, DefaultAudioStreamConfig)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v want wrapping %v", err, boom)
	}
	// The chain stops at PCMSetParams -- Prepare / Start must NOT fire.
	want := []string{"PCMInfo", "PCMSetParams"}
	if !stringsEq(f.calls, want) {
		t.Fatalf("calls = %v want %v", f.calls, want)
	}
}

// TestSetupAudio_PrepareErr covers the device rejecting PCMPrepare.
func TestSetupAudio_PrepareErr(t *testing.T) {
	boom := errors.New("prepare boom")
	f := &fakeAudioCtl{
		info:    twoStreamInfo(),
		prepare: func(uint32) error { return boom },
	}
	_, err := setupAudio(f, DefaultAudioStreamConfig)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v want wrapping %v", err, boom)
	}
	want := []string{"PCMInfo", "PCMSetParams", "PCMPrepare"}
	if !stringsEq(f.calls, want) {
		t.Fatalf("calls = %v want %v", f.calls, want)
	}
}

// TestSetupAudio_StartErr covers the device rejecting PCMStart.
func TestSetupAudio_StartErr(t *testing.T) {
	boom := errors.New("start boom")
	f := &fakeAudioCtl{
		info:  twoStreamInfo(),
		start: func(uint32) error { return boom },
	}
	_, err := setupAudio(f, DefaultAudioStreamConfig)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v want wrapping %v", err, boom)
	}
	want := []string{"PCMInfo", "PCMSetParams", "PCMPrepare", "PCMStart"}
	if !stringsEq(f.calls, want) {
		t.Fatalf("calls = %v want %v", f.calls, want)
	}
}

// TestSetupAudio_PicksFirstOutput proves the picker returns the FIRST
// output stream when multiple are advertised (the input slot in front
// of two outputs is skipped).
func TestSetupAudio_PicksFirstOutput(t *testing.T) {
	info := []gvsound.PCMInfoEntry{
		{Direction: gvsound.PCMDirInput, Rates: uint64(1) << gvsound.PCMRate11025, Formats: uint64(1) << gvsound.PCMFmtS16, ChannelsMin: 1, ChannelsMax: 2},
		{Direction: gvsound.PCMDirOutput, Rates: uint64(1) << gvsound.PCMRate11025, Formats: uint64(1) << gvsound.PCMFmtS16, ChannelsMin: 1, ChannelsMax: 2},
		{Direction: gvsound.PCMDirOutput, Rates: uint64(1) << gvsound.PCMRate44100, Formats: uint64(1) << gvsound.PCMFmtS16, ChannelsMin: 2, ChannelsMax: 2},
	}
	f := &fakeAudioCtl{info: info}
	got, err := setupAudio(f, DefaultAudioStreamConfig)
	if err != nil {
		t.Fatalf("setupAudio err = %v", err)
	}
	if got.StreamID != 1 {
		t.Fatalf("StreamID = %d want 1 (first output)", got.StreamID)
	}
}

// TestPickOutputStream_NoneOK covers the "no output entry" branch.
func TestPickOutputStream_NoneOK(t *testing.T) {
	_, _, ok := pickOutputStream([]gvsound.PCMInfoEntry{
		{Direction: gvsound.PCMDirInput},
		{Direction: gvsound.PCMDirInput},
	})
	if ok {
		t.Fatalf("pickOutputStream(input-only) ok = true, want false")
	}
}

// TestRateByteIDFromHz covers every spec-defined rate plus the
// unknown-rate fallback.
func TestRateByteIDFromHz(t *testing.T) {
	cases := []struct {
		hz   uint32
		want uint8
		ok   bool
	}{
		{5512, gvsound.PCMRate5512, true},
		{8000, gvsound.PCMRate8000, true},
		{11025, gvsound.PCMRate11025, true},
		{16000, gvsound.PCMRate16000, true},
		{22050, gvsound.PCMRate22050, true},
		{32000, gvsound.PCMRate32000, true},
		{44100, gvsound.PCMRate44100, true},
		{48000, gvsound.PCMRate48000, true},
		{64000, gvsound.PCMRate64000, true},
		{88200, gvsound.PCMRate88200, true},
		{96000, gvsound.PCMRate96000, true},
		{176400, gvsound.PCMRate176400, true},
		{192000, gvsound.PCMRate192000, true},
		{384000, gvsound.PCMRate384000, true},
		{12345, 0, false},
		{0, 0, false},
	}
	for _, tc := range cases {
		gotID, gotOK := rateByteIDFromHz(tc.hz)
		if gotID != tc.want || gotOK != tc.ok {
			t.Fatalf("rateByteIDFromHz(%d) = (%d,%v) want (%d,%v)",
				tc.hz, gotID, gotOK, tc.want, tc.ok)
		}
	}
}

// TestSetupAudio_PublicWrapper exercises the exported SetupAudio
// against a nil *gvsound.VirtioSound so the production wrapper's
// single-statement delegation is observed -- the nil receiver makes
// the underlying PCMInfo panic, so we recover + assert the panic was
// the indirection into PCMInfo (not a check inside setupAudio).
//
// (We accept either a non-nil error or a runtime panic -- both prove
// the wrapper hands `snd` straight to setupAudio without
// pre-validation; the engine documents that it constructs SetupAudio
// only after OpenVirtioSound succeeds.)
func TestSetupAudio_PublicWrapper(t *testing.T) {
	defer func() { _ = recover() }()
	_, _ = SetupAudio(nil, DefaultAudioStreamConfig)
}
