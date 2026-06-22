// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package realdev

import (
	"errors"
	"fmt"

	gvsound "github.com/go-virtio/sound"
)

// AudioStreamConfig holds the engine's desired PCM negotiation. The
// Quake mixer paints stereo int16 frames at a single fixed rate (the
// engine's MaxMixOutputFrames is 512 stereo frames -> 2048 bytes per
// tick), so we only need to express the rate / channels / format the
// caller wants -- the buffer + period sizes are derived from the tick
// budget by the caller and forwarded verbatim.
//
// Defaults match the engine's upstream Quake values:
//
//   - PreferredRates = [11025, 22050, 44100, 48000] (engine first, then
//     common host rates the QEMU virtio-snd-pci device commonly accepts)
//   - Channels = 2 stereo (the mixer's StereoSample shape)
//   - Format = S16 LE     (the wire-format the WrapAudio adapter emits)
//   - Period = 2048 bytes (one full MaxMixOutputFrames batch)
//   - Buffer = 8192 bytes (4 periods = ~46 ms latency at 44.1 kHz)
//
// The chosen rate is whichever PreferredRates entry the device's chosen
// output stream advertises FIRST in the bitmap walk. SetupAudio
// returns the accepted Hz in AudioSetupResult.Rate so the caller can
// drive an upsampler when the mixer rate (11025 Hz) does not match
// what the device negotiated.
type AudioStreamConfig struct {
	// PreferredRates is the engine's rate-fallback chain (Hz). The
	// helper walks the list in order and picks the first entry whose
	// matching PCMRate* bit is set in the chosen output stream's Rates
	// bitmap. Empty list -> ErrAudioRateUnsupported.
	PreferredRates []uint32

	// Channels is the desired channel count (1 = mono, 2 = stereo). The
	// helper verifies this is inside the stream's ChannelsMin /
	// ChannelsMax range advertised by PCMInfo.
	Channels uint8

	// Format is one of gvsound.PCMFmt* constants (PCMFmtS16,
	// PCMFmtU8, ...). The helper verifies the stream's Formats bitmap
	// has the corresponding bit set.
	Format uint8

	// PeriodBytes is the period size virtio-snd will fire a period-
	// elapsed event after. Must divide BufferBytes evenly.
	PeriodBytes uint32

	// BufferBytes is the full ring-buffer size virtio-snd will reserve
	// for the stream. Drives latency: larger = more underrun resilience
	// but more delay.
	BufferBytes uint32
}

// DefaultAudioStreamConfig is the AudioStreamConfig the engine boots
// with on quake-tamago. Exported so out-of-tree callers can match the
// stock parameters.
var DefaultAudioStreamConfig = AudioStreamConfig{
	PreferredRates: []uint32{11025, 22050, 44100, 48000},
	Channels:       2,
	Format:         gvsound.PCMFmtS16,
	PeriodBytes:    2048, // 512 stereo S16 frames -- one MaxMixOutputFrames batch
	BufferBytes:    8192, // 4 periods
}

// AudioSetupResult is the per-stream metadata returned by SetupAudio:
// the chosen stream's id (the index into the device-advertised stream
// table) plus the rate / format / channels the device accepted. The
// caller passes StreamID into WrapAudio and uses the (Rate, Channels)
// pair to assert the engine's mixer rate matches what the device is
// consuming.
type AudioSetupResult struct {
	StreamID uint32
	Rate     uint32 // Hz (e.g. 11025)
	Channels uint8
	Format   uint8 // gvsound.PCMFmt*
}

// Sentinel errors for the audio setup path. Exported so callers can
// branch + format them.
var (
	// ErrAudioNoStreams is returned when PCMInfo reports zero streams
	// (a device with virtio-sound advertised but no PCM streams
	// configured is a host-side misconfiguration).
	ErrAudioNoStreams = errors.New("realdev: virtio-snd device advertises no PCM streams")
	// ErrAudioNoOutputStream is returned when none of the advertised
	// streams have direction == PCMDirOutput.
	ErrAudioNoOutputStream = errors.New("realdev: no virtio-snd stream advertises Direction=Output")
	// ErrAudioRateUnsupported is returned when the requested rate has
	// no corresponding bit in the chosen output stream's Rates bitmap.
	ErrAudioRateUnsupported = errors.New("realdev: chosen output stream does not advertise the requested rate")
	// ErrAudioFormatUnsupported is returned when the chosen output
	// stream's Formats bitmap lacks the requested PCMFmt* bit.
	ErrAudioFormatUnsupported = errors.New("realdev: chosen output stream does not advertise the requested PCM format")
	// ErrAudioChannelsUnsupported is returned when the chosen output
	// stream's ChannelsMin..ChannelsMax range excludes the requested
	// count.
	ErrAudioChannelsUnsupported = errors.New("realdev: chosen output stream does not accept the requested channel count")
)

// audioCtl is the seam-friendly subset of *gvsound.VirtioSound that
// SetupAudio drives. Production code passes the real driver verbatim;
// tests substitute a fake recording every call.
type audioCtl interface {
	PCMInfo() ([]gvsound.PCMInfoEntry, error)
	PCMSetParams(streamID uint32, p gvsound.PCMParams) error
	PCMPrepare(streamID uint32) error
	PCMStart(streamID uint32) error
}

// SetupAudio drives the virtio-snd PCM handshake end-to-end:
//
//  1. PCMInfo()                       — enumerate advertised streams.
//  2. Pick the first stream with Direction == PCMDirOutput.
//  3. Validate the requested (rate, format, channels) tuple against
//     the chosen stream's bitmaps.
//  4. PCMSetParams(stream, params)    — negotiate the wire format.
//  5. PCMPrepare(stream)              — move to PREPARED.
//  6. PCMStart(stream)                — move to RUNNING. After this
//     call the device DMA-consumes from the tx virtqueue and emits
//     PCM to the host audio backend.
//
// On success the caller can pass the returned AudioSetupResult.StreamID
// into WrapAudio (which then satisfies virtio.AudioDevice). On any
// error the partially-configured stream is left in whatever state the
// failing step landed it -- the caller MAY issue PCMRelease() to reset
// it before retrying.
//
// The caller's cfg.PreferredRates list is walked in order and the
// first entry that maps to a Hz value present in the chosen output
// stream's Rates bitmap is negotiated; the accepted Hz is reported
// back via AudioSetupResult.Rate so the caller can drive a per-tic
// upsampler when the mixer rate (11025 Hz) does not match it.
func SetupAudio(snd *gvsound.VirtioSound, cfg AudioStreamConfig) (AudioSetupResult, error) {
	return setupAudio(snd, cfg)
}

// setupAudio is the interface-typed worker SetupAudio + tests share.
// Split out so tests can pass a fake audioCtl recording every step
// without standing up a real *gvsound.VirtioSound.
func setupAudio(snd audioCtl, cfg AudioStreamConfig) (AudioSetupResult, error) {
	entries, err := snd.PCMInfo()
	if err != nil {
		return AudioSetupResult{}, fmt.Errorf("realdev: PCMInfo: %w", err)
	}
	if len(entries) == 0 {
		return AudioSetupResult{}, ErrAudioNoStreams
	}

	streamID, info, ok := pickOutputStream(entries)
	if !ok {
		return AudioSetupResult{}, ErrAudioNoOutputStream
	}

	rateHz, rateID, ok := pickRate(info.Rates, cfg.PreferredRates)
	if !ok {
		return AudioSetupResult{}, ErrAudioRateUnsupported
	}
	if info.Formats&(uint64(1)<<cfg.Format) == 0 {
		return AudioSetupResult{}, ErrAudioFormatUnsupported
	}
	if cfg.Channels < info.ChannelsMin || cfg.Channels > info.ChannelsMax {
		return AudioSetupResult{}, ErrAudioChannelsUnsupported
	}

	params := gvsound.PCMParams{
		BufferBytes: cfg.BufferBytes,
		PeriodBytes: cfg.PeriodBytes,
		Features:    0,
		Channels:    cfg.Channels,
		Format:      cfg.Format,
		Rate:        rateID,
	}
	if err := snd.PCMSetParams(streamID, params); err != nil {
		return AudioSetupResult{}, fmt.Errorf("realdev: PCMSetParams stream=%d: %w", streamID, err)
	}
	if err := snd.PCMPrepare(streamID); err != nil {
		return AudioSetupResult{}, fmt.Errorf("realdev: PCMPrepare stream=%d: %w", streamID, err)
	}
	if err := snd.PCMStart(streamID); err != nil {
		return AudioSetupResult{}, fmt.Errorf("realdev: PCMStart stream=%d: %w", streamID, err)
	}
	return AudioSetupResult{
		StreamID: streamID,
		Rate:     rateHz,
		Channels: cfg.Channels,
		Format:   cfg.Format,
	}, nil
}

// pickRate walks `preferred` in order and returns the first (Hz, byte-
// id) pair whose corresponding bit is set in the stream's Rates
// bitmap. Returns (0, 0, false) if none of the preferred entries are
// supported (the caller then surfaces ErrAudioRateUnsupported).
//
// Unknown Hz values in `preferred` (e.g. 12345) are silently skipped
// rather than rejecting the whole list -- this keeps the engine's
// fallback chain extensible without per-entry pre-validation.
func pickRate(streamRates uint64, preferred []uint32) (uint32, uint8, bool) {
	for _, hz := range preferred {
		id, ok := rateByteIDFromHz(hz)
		if !ok {
			continue
		}
		if streamRates&(uint64(1)<<id) == 0 {
			continue
		}
		return hz, id, true
	}
	return 0, 0, false
}

// pickOutputStream returns the first entry whose Direction matches
// PCMDirOutput (0). The boolean is false when no entry qualifies. The
// returned index is the stream id PCMSetParams / PCMPrepare /
// PCMStart expect (index into the device-advertised stream table).
func pickOutputStream(entries []gvsound.PCMInfoEntry) (uint32, gvsound.PCMInfoEntry, bool) {
	for i, e := range entries {
		if e.Direction == gvsound.PCMDirOutput {
			return uint32(i), e, true
		}
	}
	return 0, gvsound.PCMInfoEntry{}, false
}

// rateByteIDFromHz maps a Hz value to the corresponding PCMRate* byte
// ID. Returns (0, false) for any Hz value outside the spec-defined
// table -- callers surface ErrAudioRateUnsupported in that case rather
// than handing a garbage rate byte to PCMSetParams.
func rateByteIDFromHz(hz uint32) (uint8, bool) {
	switch hz {
	case 5512:
		return gvsound.PCMRate5512, true
	case 8000:
		return gvsound.PCMRate8000, true
	case 11025:
		return gvsound.PCMRate11025, true
	case 16000:
		return gvsound.PCMRate16000, true
	case 22050:
		return gvsound.PCMRate22050, true
	case 32000:
		return gvsound.PCMRate32000, true
	case 44100:
		return gvsound.PCMRate44100, true
	case 48000:
		return gvsound.PCMRate48000, true
	case 64000:
		return gvsound.PCMRate64000, true
	case 88200:
		return gvsound.PCMRate88200, true
	case 96000:
		return gvsound.PCMRate96000, true
	case 176400:
		return gvsound.PCMRate176400, true
	case 192000:
		return gvsound.PCMRate192000, true
	case 384000:
		return gvsound.PCMRate384000, true
	}
	return 0, false
}
