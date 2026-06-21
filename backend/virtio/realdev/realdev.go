// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package realdev

import (
	"encoding/binary"
	"errors"

	"github.com/go-quake1/engine/backend/virtio"
	"github.com/go-quake1/engine/sound"
	"github.com/go-virtio/gpu"
	gvinput "github.com/go-virtio/input"
	gvsound "github.com/go-virtio/sound"
)

// ErrSoundStreamNotReady is the placeholder error WritePCM returns when
// the underlying virtio-sound stream has not yet been negotiated
// (PCMSetParams → PCMPrepare → PCMStart). The wrapper itself does NOT
// drive that handshake — the caller is responsible — and the sentinel
// exists so the engine's QueueAudio can errors.Is-check it and stay
// silent when audio output is optional. Today the wrapper forwards
// every WritePCM straight to the device, which returns
// gvsound.ErrDeviceStatus if the stream is not RUNNING; this sentinel
// is reserved for future use by callers that want to enforce stream
// state guest-side instead of round-tripping through the device.
var ErrSoundStreamNotReady = errors.New("realdev: sound stream has not been negotiated")

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
type audioAdapter struct {
	write      writePCMFn
	streamID   uint32
	rateLookup sampleRateFn
}

// WrapAudio adapts *sound.VirtioSound to virtio.AudioDevice.
//
// WritePCM serialises each StereoSample as 2 int16 little-endian
// (L then R) and forwards the byte buffer to the device's tx queue
// via snd.Write(streamID, …).
//
// IMPORTANT: this assumes a stream has ALREADY been negotiated —
// the caller must have driven PCMSetParams → PCMPrepare → PCMStart for
// streamID before the first WritePCM. The wrapper performs no control-
// queue work; if the stream is not RUNNING the device will reject the
// transfer with gvsound.ErrDeviceStatus.
//
// SampleRate is best-effort. virtio-sound has no "current rate"
// accessor; the wrapper queries StreamParams(streamID), which is
// populated by a successful PCMSetParams call. If the caller has not
// negotiated the stream yet (StreamParams returns (_, false)) the
// fallback is 0, matching virtio.Backend's "I don't know" handling.
//
// `streamID` is the playback stream the caller pre-allocated via the
// device's control queue.
func WrapAudio(snd *gvsound.VirtioSound, streamID uint32) virtio.AudioDevice {
	return &audioAdapter{
		write:      snd.Write,
		streamID:   streamID,
		rateLookup: snd.StreamParams,
	}
}

// WritePCM forwards `samples` to the underlying device. Returns nil on
// a zero-length input (nothing to push) without touching the device.
func (a *audioAdapter) WritePCM(samples []sound.StereoSample) error {
	if len(samples) == 0 {
		return nil
	}
	buf := serializeStereo(samples)
	_, err := a.write(a.streamID, buf)
	return err
}

// SampleRate returns the negotiated stream sample rate in Hz. Falls
// back to 0 when no PCMSetParams has been issued for the wrapper's
// streamID (virtio.Backend's caller logic then substitutes its 22050 Hz
// default via the nil-AudioDevice path — but a non-nil wrapper that
// can't yet report a rate is a configuration error the caller should
// surface separately).
func (a *audioAdapter) SampleRate() int {
	params, ok := a.rateLookup(a.streamID)
	if !ok {
		return 0
	}
	return pcmRateHz(params.Rate)
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
