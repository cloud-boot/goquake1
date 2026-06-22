// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package realdev

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/backend/virtio"
	"github.com/go-quake1/engine/sound"
	"github.com/go-virtio/gpu"
	gvinput "github.com/go-virtio/input"
	gvsound "github.com/go-virtio/sound"
)

// ErrSoundStreamNotReady is returned by WritePCM when the underlying
// virtio-sound stream has not yet been negotiated (PCMSetParams →
// PCMPrepare → PCMStart). The wrapper itself does NOT drive that
// handshake — the caller is responsible — and this sentinel exists so
// the engine's [backend.Backend.QueueAudio] can errors.Is-check it and
// stay silent when audio output is optional.
//
// The sentinel wraps [backend.ErrUnsupported] so existing QueueAudio
// implementations (which already swallow ErrUnsupported on the
// "backend doesn't support audio yet" path) also swallow this guest-
// side guard without further changes.
//
// Detection: SampleRate() returns 0 until the first successful
// PCMSetParams populates the device-side StreamParams cache, so the
// wrapper short-circuits any WritePCM call made before negotiation
// rather than round-tripping a payload the device will reject (or
// worse, time out polling for).
var ErrSoundStreamNotReady = fmt.Errorf("realdev: sound stream has not been negotiated: %w", backend.ErrUnsupported)

// ----- framebuffer ----------------------------------------------------

// framebufferAdapter satisfies virtio.Framebuffer by reading the
// exported fields of *gpu.Framebuffer for the three accessors and
// forwarding Flush through an injected function variable. The function
// variable is the testability seam: production code wires it to
// fb.Flush (which requires a live virtio-gpu transport); tests wire it
// to a no-op or to an error-returning stub.
type framebufferAdapter struct {
	buf    []byte
	width  uint32
	height uint32
	flush  func() error
}

// WrapFramebuffer adapts *gpu.Framebuffer to virtio.Framebuffer.
//
//   - Buffer() returns fb.Pix
//   - Width()  returns fb.Width
//   - Height() returns fb.Height
//   - Flush()  forwards to fb.Flush()
func WrapFramebuffer(fb *gpu.Framebuffer) virtio.Framebuffer {
	return &framebufferAdapter{
		buf:    fb.Pix,
		width:  fb.Width,
		height: fb.Height,
		flush:  fb.Flush,
	}
}

// Buffer returns the BGRA backing slice the engine writes pixels into.
func (a *framebufferAdapter) Buffer() []byte { return a.buf }

// Width returns the framebuffer width in pixels.
func (a *framebufferAdapter) Width() uint32 { return a.width }

// Height returns the framebuffer height in pixels.
func (a *framebufferAdapter) Height() uint32 { return a.height }

// Flush pushes the drawn pixels to the host scanout via the underlying
// gpu.Framebuffer.
func (a *framebufferAdapter) Flush() error { return a.flush() }

// ----- input ----------------------------------------------------------

// readEventFn is the seam-friendly signature of (*input.VirtioInput).ReadEvent.
// Tests substitute a closure that returns a canned sequence of events.
type readEventFn func(blocking bool) (*gvinput.Event, error)

// inputAdapter satisfies virtio.InputDevice by repeatedly calling
// ReadEvent(false) until the device returns ErrEventNotReady (or any
// other error), translating each raw input.Event into the abstract
// virtio.InputEvent shape via translateEvent.
type inputAdapter struct {
	read readEventFn
}

// WrapInput adapts *input.VirtioInput to virtio.InputDevice.
// PollEvents drains the device via ReadEvent(false) in a loop and
// translates each raw input.Event:
//
//   - EvKey                 → virtio.EventKey  (Code, Value)
//   - EvRel + RelX          → virtio.EventRelX (Value)
//   - EvRel + RelY          → virtio.EventRelY (Value)
//   - EvSyn / everything else → silently dropped
//
// Quit detection is NOT performed here (virtio-input has no
// window-close event); the caller must drive virtio.EventQuit through a
// separate channel if needed.
func WrapInput(in *gvinput.VirtioInput) virtio.InputDevice {
	return &inputAdapter{read: in.ReadEvent}
}

// PollEvents drains the underlying device non-blockingly.
func (a *inputAdapter) PollEvents() ([]virtio.InputEvent, error) {
	var out []virtio.InputEvent
	for {
		ev, err := a.read(false)
		if err != nil {
			// "No event available right now" is the loop terminator,
			// not a failure.
			if errors.Is(err, gvinput.ErrEventNotReady) {
				return out, nil
			}
			return out, err
		}
		if ev == nil {
			// Defensive: a (nil, nil) return would otherwise spin.
			// The driver does not produce this shape today, but the
			// guard keeps the loop bounded if it ever does.
			return out, nil
		}
		if mapped, ok := translateEvent(*ev); ok {
			out = append(out, mapped)
		}
	}
}

// translateEvent maps one raw virtio_input_event to the abstract
// virtio.InputEvent shape. Returns (zero, false) for events the
// backend does not care about (EvSyn, EvAbs, EvLED, ..., and EvRel on
// axes other than X/Y like the mouse wheel).
//
// Extracted as a free function so unit tests can cover every branch
// without standing up a real virtio-input transport.
func translateEvent(ev gvinput.Event) (virtio.InputEvent, bool) {
	switch ev.Type {
	case gvinput.EvKey:
		return virtio.InputEvent{
			Kind:  virtio.EventKey,
			Code:  ev.Code,
			Value: int32(ev.Value),
		}, true
	case gvinput.EvRel:
		switch ev.Code {
		case gvinput.RelX:
			return virtio.InputEvent{
				Kind:  virtio.EventRelX,
				Value: int32(ev.Value),
			}, true
		case gvinput.RelY:
			return virtio.InputEvent{
				Kind:  virtio.EventRelY,
				Value: int32(ev.Value),
			}, true
		}
	}
	return virtio.InputEvent{}, false
}

// ----- audio ----------------------------------------------------------

// writePCMFn is the seam-friendly signature of (*sound.VirtioSound).Write.
// Tests substitute a closure that captures the byte payload for assertion.
type writePCMFn func(streamID uint32, frames []byte) (int, error)

// sampleRateFn is the seam-friendly signature of
// (*sound.VirtioSound).StreamParams. Tests substitute a closure that
// returns a canned PCMParams.
type sampleRateFn func(streamID uint32) (gvsound.PCMParams, bool)

// audioAdapter satisfies virtio.AudioDevice by serialising each
// StereoSample as 2 int16 little-endian (L then R) and forwarding the
// byte buffer to the virtio-sound tx queue.
//
// When sourceRate (the engine mixer's sample rate) differs from the
// device's negotiated rate, every WritePCM batch is upsampled via
// nearest-neighbor (sample-and-hold) so the device receives the correct
// number of frames per second. The upsampler runs on the StereoSample
// slice BEFORE serialisation so the byte path stays bit-exact for the
// no-upsample case (sourceRate == deviceRate or sourceRate == 0).
//
// droppedFrames counts ErrXferTimeout occurrences -- the device's data
// queue back-pressure (one xfer buffer per Write, busy-poll bounded).
// The runloop calls WritePCM per-tic; if the device is still consuming
// the previous batch we drop this tic's payload silently rather than
// stalling the loop. The counter is exposed via DroppedFrames() so
// out-of-band telemetry can surface it.
type audioAdapter struct {
	write         writePCMFn
	streamID      uint32
	rateLookup    sampleRateFn
	sourceRate    int // engine mixer Hz; 0 -> upsampler disabled
	droppedFrames uint64
}

// WrapAudio adapts *sound.VirtioSound to virtio.AudioDevice.
//
// WritePCM serialises each StereoSample as 2 int16 little-endian
// (L then R) and forwards the byte buffer to the device's tx queue
// via snd.Write(streamID, …). When the engine mixer rate differs from
// the device-negotiated rate, the wrapper upsamples (nearest-neighbor
// sample-and-hold) before serialisation so the device receives the
// correct frame count per wall-clock second.
//
// IMPORTANT: this assumes a stream has ALREADY been negotiated —
// the caller must have driven PCMSetParams → PCMPrepare → PCMStart for
// streamID before the first WritePCM (see [SetupAudio]). The wrapper
// performs no control-queue work; if the stream is not RUNNING the
// device will reject the transfer with gvsound.ErrDeviceStatus.
//
// SampleRate is best-effort. virtio-sound has no "current rate"
// accessor; the wrapper queries StreamParams(streamID), which is
// populated by a successful PCMSetParams call. If the caller has not
// negotiated the stream yet (StreamParams returns (_, false)) the
// fallback is 0, matching virtio.Backend's "I don't know" handling.
//
// `streamID` is the playback stream the caller pre-allocated via the
// device's control queue.
//
// `sourceRate` is the engine mixer's Hz; pass 0 to disable upsampling
// (then the wrapper passes the StereoSample slice through verbatim).
func WrapAudio(snd *gvsound.VirtioSound, streamID uint32, sourceRate int) virtio.AudioDevice {
	return &audioAdapter{
		write:      snd.Write,
		streamID:   streamID,
		rateLookup: snd.StreamParams,
		sourceRate: sourceRate,
	}
}

// WritePCM forwards `samples` to the underlying device. Returns nil on
// a zero-length input (nothing to push) without touching the device.
// Returns [ErrSoundStreamNotReady] (which wraps [backend.ErrUnsupported])
// when the device-negotiated rate is 0, the sentinel value virtio-sound
// uses when no PCMSetParams has been issued for the wrapper's streamID:
// pushing a payload at that state would either time out at the device
// (no queue descriptor produced) or be rejected with ErrDeviceStatus.
// The guard keeps the engine's audio path optional without requiring
// callers to drive the full handshake just to stay quiet.
//
// When the engine's sourceRate differs from the device-negotiated rate,
// `samples` is upsampled via nearest-neighbor (sample-and-hold) before
// serialisation so the device receives the correct frames-per-second
// count. The upsampler is a no-op when sourceRate == 0 (caller didn't
// supply one) or sourceRate == deviceRate (1:1 match -- the byte path
// stays bit-exact for the no-upsample case).
func (a *audioAdapter) WritePCM(samples []sound.StereoSample) error {
	if len(samples) == 0 {
		return nil
	}
	devRate := a.deviceSampleRate()
	if devRate == 0 {
		return ErrSoundStreamNotReady
	}
	upsampled := resampleNearest(samples, a.sourceRate, devRate)
	buf := serializeStereo(upsampled)
	_, err := a.write(a.streamID, buf)
	if err != nil && errors.Is(err, gvsound.ErrXferTimeout) {
		// Back-pressure: the device's tx queue is still consuming
		// the previous batch (one-xfer-at-a-time + busy-poll).
		// Drop this tic's frames rather than stalling the runloop;
		// the counter is exposed via DroppedFrames so the caller
		// can surface the loss without interleaving log lines per
		// drop. A proper fix needs queue-depth + async submission
		// in go-virtio/sound (out of scope for the engine wrapper).
		a.droppedFrames++
		return nil
	}
	return err
}

// DroppedFrames returns the cumulative count of WritePCM batches the
// wrapper had to drop because the underlying virtio-snd tx queue was
// still busy with a prior batch (ErrXferTimeout). Useful for periodic
// runloop logging without spamming the serial console per drop.
func (a *audioAdapter) DroppedFrames() uint64 { return a.droppedFrames }

// SampleRate returns the engine-facing sample rate. The engine's
// runloop calls Backend.SampleRate() to know what rate to mix at; we
// hand back the engine mixer's sourceRate (the rate the engine
// PRODUCES, not the rate the device CONSUMES), since the wrapper
// upsamples internally. Falls back to the device-negotiated rate when
// no sourceRate was supplied, and to 0 when the stream has not been
// negotiated yet.
func (a *audioAdapter) SampleRate() int {
	if a.sourceRate > 0 {
		return a.sourceRate
	}
	return a.deviceSampleRate()
}

// deviceSampleRate returns the device-negotiated PCM rate in Hz, or 0
// when PCMSetParams has not been issued for the wrapper's streamID.
// Internal accessor used by the upsampler's "do I need to resample?"
// branch and the WritePCM "stream not ready" guard.
func (a *audioAdapter) deviceSampleRate() int {
	params, ok := a.rateLookup(a.streamID)
	if !ok {
		return 0
	}
	return pcmRateHz(params.Rate)
}

// resampleNearest expands `in` from sourceRate to deviceRate by
// nearest-neighbor (sample-and-hold). When sourceRate == 0 (upsampler
// disabled) or sourceRate == deviceRate (1:1 match) the input is
// returned verbatim, no allocation. For upsampling (deviceRate >
// sourceRate) every input frame is replicated `deviceRate/sourceRate`
// times -- audio quality is intentionally minimal; the goal is
// proof-of-life at the wire layer, not faithful reconstruction. A
// proper polyphase resampler is a follow-up.
//
// Downsampling (deviceRate < sourceRate) decimates by the same ratio
// (every Nth frame) -- not used by the Quake mixer (11025 -> 44100 is
// always up) but kept symmetric for completeness.
func resampleNearest(in []sound.StereoSample, sourceRate, deviceRate int) []sound.StereoSample {
	if sourceRate <= 0 || sourceRate == deviceRate || deviceRate <= 0 {
		return in
	}
	outLen := len(in) * deviceRate / sourceRate
	if outLen == 0 {
		return in[:0]
	}
	out := make([]sound.StereoSample, outLen)
	for i := 0; i < outLen; i++ {
		// srcIdx is provably in range: i ranges over [0, outLen) where
		// outLen = len(in)*deviceRate/sourceRate, so the maximum
		// srcIdx = (outLen-1)*sourceRate/deviceRate < len(in) by
		// integer-division monotonicity. No clamp needed.
		srcIdx := i * sourceRate / deviceRate
		out[i] = in[srcIdx]
	}
	return out
}

// serializeStereo packs `samples` into a single byte slice as
// 2 * int16 little-endian per frame (L then R). The output length is
// exactly 4 * len(samples).
func serializeStereo(samples []sound.StereoSample) []byte {
	out := make([]byte, 4*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*4+0:i*4+2], uint16(s.L))
		binary.LittleEndian.PutUint16(out[i*4+2:i*4+4], uint16(s.R))
	}
	return out
}

// pcmRateHz maps a virtio-sound PCMRate* byte-id to its frequency in
// Hertz. Returns 0 for any code outside the spec-defined table.
//
// Source: Virtio 1.2 §5.14.6.6.1 (virtio_snd_pcm_rate enum), mirrored
// by github.com/go-virtio/sound's PCMRate* constants.
func pcmRateHz(rate uint8) int {
	switch rate {
	case gvsound.PCMRate5512:
		return 5512
	case gvsound.PCMRate8000:
		return 8000
	case gvsound.PCMRate11025:
		return 11025
	case gvsound.PCMRate16000:
		return 16000
	case gvsound.PCMRate22050:
		return 22050
	case gvsound.PCMRate32000:
		return 32000
	case gvsound.PCMRate44100:
		return 44100
	case gvsound.PCMRate48000:
		return 48000
	case gvsound.PCMRate64000:
		return 64000
	case gvsound.PCMRate88200:
		return 88200
	case gvsound.PCMRate96000:
		return 96000
	case gvsound.PCMRate176400:
		return 176400
	case gvsound.PCMRate192000:
		return 192000
	case gvsound.PCMRate384000:
		return 384000
	}
	return 0
}
