// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package runloop

import (
	"errors"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sound"
)

// HostFramer is the minimal contract [Runner] needs from the host
// package: advance the server-side simulation by one tic.
//
// tyrquake: the SV_Frame call inside Host_Frame.
//
// Defined here as a one-method interface (not the full *host.Host
// struct) so tests can stub the per-tic without spinning up a VM /
// World / Progs / Cache stack. The production *host.Host has a
// matching Frame(dt float32) error method and satisfies this
// interface without any wrapper.
type HostFramer interface {
	Frame(dt float32) error
}

// Runner owns one game session's per-frame orchestration. Created once
// at startup with all the long-lived pieces; [Runner.RunFrame] is
// called each tick by the platform's main loop.
//
// tyrquake: the role of Host_Frame in host.c -- collects input,
// advances server + client state, renders the frame, mixes audio,
// hands the output to the backend.
//
// All fields are caller-owned. Runner does not allocate any of them
// (the working buffers RGBA + MixBuffer are pre-sized at startup so
// per-frame allocations stay at zero -- this matches the project's
// bare-metal / TamaGo / GC-pause-free constraint).
type Runner struct {
	Host    HostFramer
	Client  *client.State
	Conn    server.NetConn
	Backend backend.Backend

	// Renderer state (long-lived; reused each frame).
	FrameBuffer *render.FrameBuffer
	Console     *render.Console
	Screen      *render.Screen
	Chars       *render.Pic
	Palette     *render.Palette

	// Audio state.
	SoundPool *sound.Pool

	// Per-frame input bundles (advanced by the input event handler).
	Buttons    client.MovementButtons
	Speeds     client.InputSpeeds
	ViewAngles [3]float32

	// Triggers tracks the held state of the on-wire trigger keys
	// (mouse-fire = +attack, Enter = +jump). Translated to the
	// [server.UserCmd.Buttons] bitmask in RunFrame and handed to
	// [client.Tick] as TickInput.ActionButtons. Movement keys
	// (W/A/S/D + arrows + shift) live in Buttons above and do NOT
	// feed this field -- the on-wire `buttons` byte only carries the
	// trigger bits the QC progs read via self.button*.
	Triggers TriggerButtons

	// ViewOrigin is a legacy caller-owned anchor retained for
	// backwards compatibility. RunFrame no longer sources the
	// per-tic camera position from this field -- the viewOrigin
	// handed to [Runner.Pre2DDraw] now comes from the wire-mirrored
	// client.State.Entities[Client.PlayerNum].Origin (the proper
	// client/server split: the renderer reads what the server told
	// the client, not the server edicts directly). When the player
	// entity has not yet been received the fallback is the zero
	// vector. Callers may still set ViewOrigin for diagnostics /
	// out-of-band camera overrides, but the runloop ignores it.
	ViewOrigin [3]float32

	// Working buffers (long-lived; reused per frame).
	RGBA      []byte               // size = fb.Width * fb.Height * 4
	MixBuffer []sound.StereoSample // size = sound.MixBufferStereoFrames

	// Compose configuration.
	BackgroundIdx  byte    // palette fill for Compose2D
	NotifyLifetime float32 // seconds a notify row stays visible
	MaxNotifyRows  int     // upper bound on the notify overlay row count

	// Pre2DDraw is an optional hook the runner invokes between the
	// client tick and the 2D Compose. The closure owns the 3D
	// rasterization (BSP walk, surface emission, FillTexturedPolygon
	// per face); on return fb holds the rendered scene, which
	// Compose2D then overlays its 2D layers on top of.
	//
	// Signature: (fb, viewOrigin, viewAngles) -> error. viewOrigin
	// is the (x, y, z) world-space camera position; viewAngles is
	// (pitch, yaw, roll) in DEGREES, matching render.RefDef.
	//
	// When non-nil the runner also asks Compose2D to skip its
	// background clear (FrameContext.SkipBackgroundFill = true) so
	// the pre-drawn scene isn't overwritten. When nil the previous
	// 2D-only behaviour is preserved verbatim.
	//
	// Errors propagate from RunFrame verbatim (the present /
	// audio steps are skipped for that tic).
	Pre2DDraw func(fb *render.FrameBuffer, viewOrigin [3]float32, viewAngles [3]float32) error
}

// Sentinel errors returned by [Runner.RunFrame] before any work runs.
var (
	ErrRunnerNilHost    = errors.New("runloop: nil Host")
	ErrRunnerNilClient  = errors.New("runloop: nil Client State")
	ErrRunnerNilConn    = errors.New("runloop: nil NetConn")
	ErrRunnerNilBackend = errors.New("runloop: nil Backend")
	ErrRunnerNilFB      = errors.New("runloop: nil FrameBuffer")
	ErrRunnerRGBASize   = errors.New("runloop: RGBA buffer too small for framebuffer")
)

// RunFrame runs one full game tic. Sequence:
//
//  1. snap := Backend.PollInput()       (collect events)
//  2. apply snap.KeysDown / KeysUp to r.Buttons via
//     [UpdateButtonsFromSnapshot] (the snapshot's mouse deltas are
//     consumed by client.Tick via the TickInput bundle below)
//  3. host.Frame(dt)                    (server-side tick)
//  4. client.Tick(...)                  (client-side: drain inbound,
//     send clc_move; updates r.ViewAngles)
//  5. r.Pre2DDraw(fb, viewOrigin,       (optional 3D pass; skipped
//     viewAngles)                        when nil)
//  6. render.Compose2D(fb, ...)         (2D frame -- console + notify;
//     SkipBackgroundFill when Pre2DDraw is set so the 3D pixels survive)
//  7. fb.Expand(r.RGBA, palette)        (palette -> RGBA)
//  8. Backend.PresentFrame(r.RGBA, ...) (display)
//  9. sound.Paint(pool, r.MixBuffer, n) (mix audio)
//  10. Backend.QueueAudio(r.MixBuffer[:n])
//
// dt is the frame delta in seconds (from Backend.Now() differences;
// caller passes the result). nowSec is the wall-clock-like time the
// notify overlay + client.Tick stamp messages with.
//
// SHORT-CIRCUITS:
//   - client.Tick ALWAYS runs (no Connection-based skip): the wire-
//     driven signon handshake (server.SendSignonHandshake -> client's
//     applySignonNum stage 1) needs the inbound drain to fire even
//     when state.Connection == StateDisconnected; without it the
//     stage-1 byte that transitions the client into StateConnecting
//     would never be read. Tick's OWN guard short-circuits the
//     OUTBOUND clc_move build pre-StateConnected, so a pre-signon
//     Tick is a pure inbound-drain (no spurious clc_move).
//   - if r.SoundPool == nil or len(r.MixBuffer) == 0, the audio steps
//     are SKIPPED (a video-only backend works fine without audio)
//   - if Backend's QueueAudio returns [backend.ErrUnsupported], that
//     specific error is silently ignored (the engine doesn't need to
//     know the backend lacks audio)
//
// All other backend errors propagate verbatim. On error the remaining
// steps are skipped (so a backend PresentFrame failure doesn't
// prevent the host from advancing the server simulation next tick).
func (r *Runner) RunFrame(dt float32, nowSec float32) error {
	if r.Host == nil {
		return ErrRunnerNilHost
	}
	if r.Client == nil {
		return ErrRunnerNilClient
	}
	if r.Conn == nil {
		return ErrRunnerNilConn
	}
	if r.Backend == nil {
		return ErrRunnerNilBackend
	}
	if r.FrameBuffer == nil {
		return ErrRunnerNilFB
	}
	if len(r.RGBA) < r.FrameBuffer.Width*r.FrameBuffer.Height*4 {
		return ErrRunnerRGBASize
	}

	// 1) Collect input.
	snap, err := r.Backend.PollInput()
	if err != nil {
		return err
	}

	// 2) Translate the raw key events into the persistent button state.
	UpdateButtonsFromSnapshot(&r.Buttons, snap)
	UpdateTriggersFromSnapshot(&r.Triggers, snap)

	// 3) Advance server simulation.
	if err := r.Host.Frame(dt); err != nil {
		return err
	}

	// 4) Client tick: drain inbound, send clc_move (post-signon only).
	//    ALWAYS runs: the wire-driven signon handshake needs the
	//    inbound drain to fire even when state.Connection ==
	//    StateDisconnected -- otherwise the server's stage-1 signon
	//    byte (which transitions the client to StateConnecting) is
	//    never read, and the lifecycle deadlocks. The OUTBOUND
	//    clc_move build inside Tick is itself gated on StateConnected
	//    so a pre-signon Tick is a pure inbound-drain (no spurious
	//    clc_move on the wire before the handshake completes).
	in := client.TickInput{
		// Pointer (not a copy) so the per-frame impulse drain inside
		// KeyState lands on the runloop's persistent r.Buttons state.
		// See client.TickInput.Buttons + client.BaseMove docs for the
		// "0.5 forever" bug a stack copy would re-introduce.
		Buttons:       &r.Buttons,
		MouseDX:       snap.MouseDX,
		MouseDY:       snap.MouseDY,
		Sensitivity:   1,
		Speeds:        r.Speeds,
		Dt:            dt,
		NowSec:        nowSec,
		ActionButtons: r.Triggers.ActionButtons(),
	}
	out, err := client.Tick(r.Client, r.Conn, in, r.ViewAngles)
	if err != nil {
		return err
	}
	r.ViewAngles = out.ViewAngles

	// 5) Optional 3D pass. The closure owns the BSP walk +
	//    rasterization; on return r.FrameBuffer holds the rendered
	//    scene that Compose2D overlays its 2D layers on top of.
	//    When nil the previous 2D-only behaviour is preserved.
	//
	//    viewOrigin is sourced from the wire-mirrored client state:
	//    r.Client.Entities[r.Client.PlayerNum].Origin -- the entity
	//    snapshot the server broadcast via svc_update + the client
	//    cached into State.Entities (proper client/server split, the
	//    renderer reads what the server told the client rather than
	//    reaching into the server edict pool directly). A missing
	//    entry (player entity not yet received this signon) falls
	//    back to the zero vector; the renderer's PointInLeaf guard
	//    skips the BSP walk for out-of-map origins.
	//
	//    ViewAngles is the (pitch, yaw, roll) the client tick has
	//    just refreshed from mouse + arrow-key input.
	if r.Pre2DDraw != nil {
		viewOrigin := viewOriginFromState(r.Client)
		if err := r.Pre2DDraw(r.FrameBuffer, viewOrigin, r.ViewAngles); err != nil {
			return err
		}
	}

	// 6+7) Render the 2D frame + expand to RGBA in one call. When
	//      Pre2DDraw is set we skip Compose2D's background clear so
	//      the 3D pixels under the console/notify overlay survive.
	ctx := render.FrameContext{
		Screen:             r.Screen,
		Console:            r.Console,
		Chars:              r.Chars,
		Palette:            r.Palette,
		Now:                nowSec,
		NotifyLifetime:     r.NotifyLifetime,
		MaxNotifyRows:      r.MaxNotifyRows,
		BackgroundIdx:      r.BackgroundIdx,
		SkipBackgroundFill: r.Pre2DDraw != nil,
	}
	if err := render.ExpandFrame(r.FrameBuffer, r.RGBA, ctx); err != nil {
		return err
	}

	// 7) Present.
	if err := r.Backend.PresentFrame(r.RGBA, r.FrameBuffer.Width, r.FrameBuffer.Height); err != nil {
		return err
	}

	// 8+9) Audio (optional).
	if r.SoundPool != nil && len(r.MixBuffer) > 0 {
		// Zero the accumulator each tic; sound.Paint accumulates.
		for i := range r.MixBuffer {
			r.MixBuffer[i] = sound.StereoSample{}
		}
		n := len(r.MixBuffer)
		if n > sound.MaxMixOutputFrames {
			n = sound.MaxMixOutputFrames
		}
		if err := sound.Paint(r.SoundPool, r.MixBuffer, n); err != nil {
			return err
		}
		if err := r.Backend.QueueAudio(r.MixBuffer[:n]); err != nil {
			if !errors.Is(err, backend.ErrUnsupported) {
				return err
			}
		}
	}

	return nil
}

// viewOriginFromState returns the camera position the per-tic
// Pre2DDraw hook should rasterize against, sourced from the wire-
// mirrored client state at [client.State.Entities][PlayerNum].Origin.
//
// This is the proper client/server split: the renderer reads the
// entity snapshot the server broadcast via svc_update + the client
// cached into State.Entities, NOT the server edict pool directly.
// On the single-process loopback path the two values are identical
// per-tic (svc_update writes the edict origin onto the wire and
// applyUpdate writes it back into State.Entities), but the indirection
// keeps the data-flow honest for the eventual remote-server path.
//
// Fallback: if cs is nil OR State.Entities[PlayerNum] is absent (the
// player entity has not been received yet -- pre-signon, or the wire
// drain has not yet delivered the first svc_update for this slot), the
// returned origin is the zero vector. The Pre2DDraw closure's
// PointInLeaf guard will then skip the BSP walk for that tic, which
// is the same behaviour as the legacy out-of-map anchor.
func viewOriginFromState(cs *client.State) [3]float32 {
	if cs == nil || cs.Entities == nil {
		return [3]float32{}
	}
	es, ok := cs.Entities[cs.PlayerNum]
	if !ok {
		return [3]float32{}
	}
	return es.Origin
}

// UpdateButtonsFromSnapshot translates the per-frame raw key events in
// snap into edge transitions on the persistent [client.MovementButtons]
// state. Maps:
//
//	KeyW     -> Buttons.Forward
//	KeyS     -> Buttons.Back
//	KeyA     -> Buttons.MoveLeft   (strafe; +moveleft)
//	KeyD     -> Buttons.MoveRight  (strafe; +moveright)
//	KeyLeft  -> Buttons.Left       (turn arrow; +left)
//	KeyRight -> Buttons.Right      (turn arrow; +right)
//	KeyUp    -> Buttons.Lookup
//	KeyDown  -> Buttons.Lookdown
//	KeySpace -> Buttons.Up         (jump)
//	KeyCtrl  -> Buttons.Down       (crouch)
//	KeyShift -> Buttons.SpeedHeld  (+speed modifier)
//
// Each down event sets the held bit (bit 0) and stamps the down-edge
// bit (bit 1) so [client.KeyState] reports the partial-frame value
// the first tic the key is pressed. Each up event clears the held bit
// and stamps the up-edge bit (bit 2).
//
// The mouse-button slot ([backend.KeyMouse1] / [backend.KeyMouse2])
// and the trigger keys (Enter/Tab/Escape) are NOT mapped here: those
// drive the per-frame ActionButtons / Impulse bits the caller OR-s
// onto the [client.TickInput] (separate from the movement buttons).
func UpdateButtonsFromSnapshot(buttons *client.MovementButtons, snap backend.InputSnapshot) {
	for _, k := range snap.KeysDown {
		if slot := buttonSlot(buttons, k); slot != nil {
			pressButton(slot)
			continue
		}
		if k == backend.KeyShift {
			buttons.SpeedHeld = true
		}
	}
	for _, k := range snap.KeysUp {
		if slot := buttonSlot(buttons, k); slot != nil {
			releaseButton(slot)
			continue
		}
		if k == backend.KeyShift {
			buttons.SpeedHeld = false
		}
	}
}

// buttonSlot resolves k to the matching field of buttons. Returns nil
// for the keys handled out-of-band (Shift -> SpeedHeld bool, and
// every key not in the movement set).
func buttonSlot(buttons *client.MovementButtons, k backend.KeyCode) *client.ButtonState {
	switch k {
	case backend.KeyW:
		return &buttons.Forward
	case backend.KeyS:
		return &buttons.Back
	case backend.KeyA:
		return &buttons.MoveLeft
	case backend.KeyD:
		return &buttons.MoveRight
	case backend.KeyLeft:
		return &buttons.Left
	case backend.KeyRight:
		return &buttons.Right
	case backend.KeyUp:
		return &buttons.Lookup
	case backend.KeyDown:
		return &buttons.Lookdown
	case backend.KeySpace:
		return &buttons.Up
	case backend.KeyCtrl:
		return &buttons.Down
	}
	return nil
}

// pressButton stamps the held bit (bit 0) and the down-edge bit (bit
// 1) onto b. The down-edge bit fires once -- [client.KeyState] clears
// it the next time it samples the button.
func pressButton(b *client.ButtonState) {
	b.Pressed |= 1 | 2
}

// releaseButton clears the held bit (bit 0) and stamps the up-edge
// bit (bit 2). [client.KeyState] clears the up-edge bit on its next
// sample.
func releaseButton(b *client.ButtonState) {
	b.Pressed &^= 1
	b.Pressed |= 4
}

// TriggerButtons tracks the persistent held state of the on-wire
// trigger keys -- the bits the QC progs read via self.button*. The
// movement keys (W/A/S/D, arrows, shift) live in the separate
// [client.MovementButtons] structure and feed [client.BaseMove] /
// [client.AdjustAngles]; they do NOT show up in the on-wire `buttons`
// byte.
//
// Mappings (driven by [UpdateTriggersFromSnapshot]):
//
//	KeyMouse1 -> Attack ([client.ButtonAttack] = 1)
//	KeyEnter  -> Jump   ([client.ButtonJump]   = 2)
//
// The mouse-2 / Escape / Tab keys are intentionally NOT mapped: Q1's
// per-tic clc_move only carries +attack and +jump in vanilla NQ; the
// QC progs do not read additional bits. Engines that need a "use"
// trigger (BUTTON_USE = 4) layer it on via an impulse byte instead.
type TriggerButtons struct {
	Attack bool // KeyMouse1 currently held
	Jump   bool // KeyEnter currently held
}

// ActionButtons returns the [server.UserCmd.Buttons] bitmask the
// runloop hands to [client.Tick] each tic. The caller OR-s this
// straight onto the per-tic [client.TickInput.ActionButtons] field,
// which [client.Tick] then writes onto the outbound clc_move
// payload's `buttons` byte. Mapping mirrors tyrquake's
// CL_BaseButtons:
//
//	Attack -> [client.ButtonAttack] (1)
//	Jump   -> [client.ButtonJump]   (2)
func (t TriggerButtons) ActionButtons() uint8 {
	var b uint8
	if t.Attack {
		b |= client.ButtonAttack
	}
	if t.Jump {
		b |= client.ButtonJump
	}
	return b
}

// UpdateTriggersFromSnapshot edge-applies snap.KeysDown / snap.KeysUp
// onto triggers. Down events set the held flag; up events clear it.
// Keys not in the trigger set (everything but [backend.KeyMouse1] and
// [backend.KeyEnter]) are ignored here -- the movement set is owned
// by [UpdateButtonsFromSnapshot].
//
// IDEMPOTENCE: applying the same KeysDown sequence twice leaves the
// triggers at the same true value; ditto KeysUp. The held bits are
// stateful (NOT auto-cleared each tic) so a held mouse-fire keeps
// firing every clc_move until the up event arrives, matching how
// upstream's +attack works.
func UpdateTriggersFromSnapshot(triggers *TriggerButtons, snap backend.InputSnapshot) {
	for _, k := range snap.KeysDown {
		switch k {
		case backend.KeyMouse1:
			triggers.Attack = true
		case backend.KeyEnter:
			triggers.Jump = true
		}
	}
	for _, k := range snap.KeysUp {
		switch k {
		case backend.KeyMouse1:
			triggers.Attack = false
		case backend.KeyEnter:
			triggers.Jump = false
		}
	}
}
