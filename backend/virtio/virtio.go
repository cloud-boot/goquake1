// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package virtio

import (
	"errors"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/sound"
)

// Framebuffer is the subset of *go-virtio/gpu.Framebuffer this backend
// uses. Kept as an interface so unit tests can plug in an in-memory
// mock without dragging the virtio-gpu driver (and its PCI transport
// dependency) into the test binary.
type Framebuffer interface {
	// Buffer returns the BGRA backing slice the engine writes pixels
	// into. Length must equal Width()*Height()*4.
	Buffer() []byte
	// Width returns the framebuffer width in pixels.
	Width() uint32
	// Height returns the framebuffer height in pixels.
	Height() uint32
	// Flush pushes the drawn pixels to the host scanout.
	Flush() error
}

// InputDevice is the subset of *go-virtio/input.VirtioInput this
// backend uses.
type InputDevice interface {
	// PollEvents returns any input events queued since the last call
	// (key down / up + relative mouse motion + quit). The
	// implementation may return an empty slice + nil error if no
	// events are queued.
	PollEvents() ([]InputEvent, error)
}

// InputEvent is the abstracted event type the backend translates into
// backend.InputSnapshot. Code values follow the Linux input layer
// (KEY_*) — virtio-input uses these verbatim on the wire.
type InputEvent struct {
	Kind  EventKind
	Code  uint16 // Linux KEY_* / BTN_* for EventKey; ignored for REL / QUIT
	Value int32  // EventKey: 1 = down, 0 = up; EventRel*: signed delta
}

// EventKind tags the event type.
type EventKind int

// EventKind values.
const (
	// EventKey is a key press (Value=1) or release (Value=0).
	EventKey EventKind = 1
	// EventRelX is a relative mouse X delta (Value is the signed delta).
	EventRelX EventKind = 2
	// EventRelY is a relative mouse Y delta (Value is the signed delta).
	EventRelY EventKind = 3
	// EventQuit is the synthetic "window close / SIGINT" event.
	EventQuit EventKind = 4
)

// AudioDevice is the subset of *go-virtio/sound.VirtioSound this
// backend uses.
type AudioDevice interface {
	// WritePCM submits one frame's worth of stereo 16-bit PCM samples.
	// Returns backend.ErrUnsupported (or any error wrapping it) when
	// the device has not negotiated a stream yet — the backend's
	// QueueAudio swallows that error so audio output stays optional.
	WritePCM(samples []sound.StereoSample) error
	// SampleRate returns the device's negotiated PCM sample rate in Hz.
	SampleRate() int
}

// ClockFn returns wall-clock seconds. Injected so tests can supply a
// deterministic clock instead of time.Now-derived randomness.
type ClockFn func() float64

// defaultClockStep is the per-Now() tick advance used when no ClockFn
// is injected. 1/60 ≈ 16.7 ms matches a typical 60 Hz display.
const defaultClockStep = 1.0 / 60.0

// Backend implements backend.Backend over the three injected devices.
// Construct via New; the public fields are exposed so callers can
// inspect or swap dependencies after construction (e.g. unit tests).
type Backend struct {
	FB    Framebuffer
	Input InputDevice
	Audio AudioDevice
	Clock ClockFn

	// Per-tic state — accumulated between PollInput calls.
	pendingDown []backend.KeyCode
	pendingUp   []backend.KeyCode
	pendingDX   float32
	pendingDY   float32
	quitFlag    bool

	// monotonic tick counter, advanced once per Now() call when no
	// ClockFn is injected.
	tick uint64
}

// Sentinel errors. Exported so callers can errors.Is-check them.
var (
	ErrVirtioNilFB    = errors.New("virtio: nil Framebuffer in backend")
	ErrVirtioRGBASize = errors.New("virtio: RGBA buffer size doesn't match framebuffer dimensions")
)

// New returns a Backend wrapping the supplied devices. fb is required;
// in / au / clock are optional:
//
//   - nil in    → PollInput returns an empty snapshot.
//   - nil au    → QueueAudio drops every frame, SampleRate returns 22050.
//   - nil clock → Now() advances a synthetic 60 Hz monotonic tick.
func New(fb Framebuffer, in InputDevice, au AudioDevice, clock ClockFn) (*Backend, error) {
	if fb == nil {
		return nil, ErrVirtioNilFB
	}
	return &Backend{
		FB:    fb,
		Input: in,
		Audio: au,
		Clock: clock,
	}, nil
}

// PresentFrame copies the engine's RGBA frame into the framebuffer's
// BGRA backing slice (swapping R↔B per pixel), then calls Flush.
//
// The engine's renderer produces RGBA8 byte order (R,G,B,A); virtio-
// gpu's 2D resource format is VIRTIO_GPU_FORMAT_B8G8R8A8_UNORM. We
// swap on the way through:
//
//	dst[i*4+0] = src[i*4+2]   // B = src.B
//	dst[i*4+1] = src[i*4+1]   // G unchanged
//	dst[i*4+2] = src[i*4+0]   // R = src.R
//	dst[i*4+3] = src[i*4+3]   // A unchanged
func (b *Backend) PresentFrame(rgba []byte, width, height int) error {
	expected := width * height * 4
	dst := b.FB.Buffer()
	if len(rgba) != expected || len(dst) != expected {
		return ErrVirtioRGBASize
	}
	for i := 0; i < expected; i += 4 {
		dst[i+0] = rgba[i+2]
		dst[i+1] = rgba[i+1]
		dst[i+2] = rgba[i+0]
		dst[i+3] = rgba[i+3]
	}
	return b.FB.Flush()
}

// Size returns the framebuffer dimensions.
func (b *Backend) Size() (int, int) {
	return int(b.FB.Width()), int(b.FB.Height())
}

// QueueAudio forwards samples to the audio device. A nil Audio is a
// silent drop. backend.ErrUnsupported (e.g. a stream that hasn't been
// negotiated yet) is swallowed; any other error is returned.
func (b *Backend) QueueAudio(samples []sound.StereoSample) error {
	if b.Audio == nil {
		return nil
	}
	if err := b.Audio.WritePCM(samples); err != nil {
		if errors.Is(err, backend.ErrUnsupported) {
			return nil
		}
		return err
	}
	return nil
}

// SampleRate returns the audio device's preferred rate. Defaults to
// 22050 Hz when no Audio device is wired up (matches NoAudio).
func (b *Backend) SampleRate() int {
	if b.Audio == nil {
		return 22050
	}
	return b.Audio.SampleRate()
}

// PollInput drains the input device, translates virtio-input events
// to backend.KeyCode + accumulated mouse delta, and returns the
// snapshot. Unmapped keycodes are silently dropped. The pending state
// is cleared on every call.
func (b *Backend) PollInput() (backend.InputSnapshot, error) {
	if b.Input == nil {
		return backend.InputSnapshot{}, nil
	}
	events, err := b.Input.PollEvents()
	if err != nil {
		return backend.InputSnapshot{}, err
	}
	for _, ev := range events {
		switch ev.Kind {
		case EventKey:
			kc, ok := mapKey(ev.Code)
			if !ok {
				continue
			}
			// Linux EV_KEY value semantics (mirrored verbatim by
			// virtio-input): 0=release, 1=press, 2=auto-repeat. The
			// runloop's held-bit state in client.MovementButtons is
			// already latched by the original press event, so repeats
			// are redundant -- and crucially MUST NOT be funneled
			// through the release path or the held bit gets cleared
			// + an up-edge gets stamped on every repeat, collapsing
			// KeyState() to 0.25 (both edges, !down) and the player's
			// move axes to ~25% of cl_*speed. Drop repeats; preserve
			// the press/release edges only.
			switch ev.Value {
			case 1:
				b.pendingDown = append(b.pendingDown, kc)
			case 0:
				b.pendingUp = append(b.pendingUp, kc)
			default:
				// auto-repeat (Linux KeyValueRepeat=2) or any future
				// value we don't model -- ignore.
			}
		case EventRelX:
			b.pendingDX += float32(ev.Value)
		case EventRelY:
			b.pendingDY += float32(ev.Value)
		case EventQuit:
			b.quitFlag = true
		}
	}
	snap := backend.InputSnapshot{
		KeysDown:      b.pendingDown,
		KeysUp:        b.pendingUp,
		MouseDX:       b.pendingDX,
		MouseDY:       b.pendingDY,
		QuitRequested: b.quitFlag,
	}
	b.pendingDown = nil
	b.pendingUp = nil
	b.pendingDX = 0
	b.pendingDY = 0
	b.quitFlag = false
	return snap, nil
}

// Now returns wall-clock seconds. With an injected Clock the value is
// whatever the caller's closure returns; without one we hand back a
// monotonic 60 Hz tick (deterministic — handy for headless reproducible
// runs).
func (b *Backend) Now() float64 {
	if b.Clock != nil {
		return b.Clock()
	}
	t := b.tick
	b.tick++
	return float64(t) * defaultClockStep
}

// mapKey translates a Linux KEY_* / BTN_* code to backend.KeyCode.
// Unmapped codes return ok=false and are dropped by PollInput.
//
// Wire-code reference (Linux input-event-codes.h, mirrored by
// go-virtio/input/keys.go):
//
//	KEY_ESC        =   1   → KeyEscape
//	KEY_ENTER      =  28   → KeyEnter
//	KEY_LEFTCTRL   =  29   → KeyCtrl
//	KEY_A          =  30   → KeyA
//	KEY_S          =  31   → KeyS
//	KEY_D          =  32   → KeyD
//	KEY_GRAVE      =  41   → KeyTilde   (US `~` key, scancode 0x29)
//	KEY_LEFTSHIFT  =  42   → KeyShift
//	KEY_SPACE      =  57   → KeySpace
//	KEY_W          =  17   → KeyW
//	KEY_UP         = 103   → KeyUp
//	KEY_LEFT       = 105   → KeyLeft
//	KEY_RIGHT      = 106   → KeyRight
//	KEY_DOWN       = 108   → KeyDown
//	BTN_LEFT       = 272   → KeyMouse1
//	BTN_RIGHT      = 273   → KeyMouse2
func mapKey(code uint16) (backend.KeyCode, bool) {
	switch code {
	case 1:
		return backend.KeyEscape, true
	case 17:
		return backend.KeyW, true
	case 28:
		return backend.KeyEnter, true
	case 29:
		return backend.KeyCtrl, true
	case 30:
		return backend.KeyA, true
	case 31:
		return backend.KeyS, true
	case 32:
		return backend.KeyD, true
	case 41:
		return backend.KeyTilde, true
	case 42:
		return backend.KeyShift, true
	case 57:
		return backend.KeySpace, true
	case 103:
		return backend.KeyUp, true
	case 105:
		return backend.KeyLeft, true
	case 106:
		return backend.KeyRight, true
	case 108:
		return backend.KeyDown, true
	case 272:
		return backend.KeyMouse1, true
	case 273:
		return backend.KeyMouse2, true
	default:
		return 0, false
	}
}
