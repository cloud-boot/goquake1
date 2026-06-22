// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"github.com/go-quake1/engine/mathlib"
	"github.com/go-quake1/engine/server"
)

// Quake 1 trigger-button bitmask values that ride on
// [server.UserCmd.Buttons] (the `byte buttons` field of the on-wire
// clc_move payload). The bitmask is what the QC progs read via
// self.button0 / self.button2 (BUTTON_ATTACK = 0x01, BUTTON_JUMP =
// 0x02). tyrquake: BUTTON_ATTACK / BUTTON_JUMP in NQ/quakedef.h.
//
// Movement keys (W/A/S/D, arrows, shift) live in the SEPARATE
// per-axis [MovementButtons] structure and are consumed by [BaseMove]
// + [AdjustAngles] -- they do NOT show up in the on-wire `buttons`
// byte. Only the trigger bits (mouse-fire, jump) do.
const (
	// ButtonAttack is the +attack bit. tyrquake: BUTTON_ATTACK = 1.
	ButtonAttack uint8 = 1
	// ButtonJump is the +jump bit. tyrquake: BUTTON_JUMP = 2.
	ButtonJump uint8 = 2
)

// ButtonState tracks the current state of one gameplay button across
// frames. tyrquake: kbutton_t in NQ/keys.h.
//
// Down holds the keynums of the (up to two) keys currently driving
// this button. A zero entry is unbound. The press/release path in
// the C client decides when to flip Pressed bits based on whether
// EITHER key is held -- two keys can press the same button so the
// button isn't released until BOTH are released.
//
// Pressed is the impulse-state bitfield:
//
//	bit 0: currently held (key down right now)
//	bit 1: edge-triggered up->down THIS frame ("impulsedown")
//	bit 2: edge-triggered down->up THIS frame ("impulseup")
//
// CL_KeyState clears bits 1+2 after sampling so they only fire once.
type ButtonState struct {
	Down    [2]int // keynum array for two keys mapped to this button (0 = unbound)
	Pressed uint8  // bit 0: down; bit 1: down-edge this frame; bit 2: up-edge this frame
}

// KeyState returns the fraction of the frame the button was held.
// Used by [BaseMove]: ForwardMove = forwardspeed * KeyState(forward) -
// backspeed * KeyState(back).
//
// tyrquake: CL_KeyState in NQ/cl_input.c. Returns 0..1.
//
// The 4-way table matches upstream:
//
//	down-edge only,  down=1: 0.5  (pressed + held this frame)
//	up-edge only,    down=0: 0    (released this frame)
//	no edges,        down=1: 1.0  (held the entire frame)
//	no edges,        down=0: 0    (up the entire frame)
//	both edges,      down=1: 0.75 (released and re-pressed)
//	both edges,      down=0: 0.25 (pressed and released)
//
// Side-effect: the impulse bits (1+2) are cleared so the next frame
// only sees fresh transitions. The held bit (0) is preserved.
func KeyState(b *ButtonState) float32 {
	impulsedown := b.Pressed&2 != 0
	impulseup := b.Pressed&4 != 0
	down := b.Pressed&1 != 0
	var val float32

	switch {
	case impulsedown && impulseup:
		if down {
			val = 0.75
		} else {
			val = 0.25
		}
	case impulsedown:
		// up-edge absent. C path: down=>0.5, !down=>0 (per upstream comment "I_Error").
		if down {
			val = 0.5
		}
	case impulseup:
		// down-edge absent. Both branches are zero in C.
		val = 0
	default:
		if down {
			val = 1.0
		}
	}

	b.Pressed &= 1 // clear impulses, keep held bit
	return val
}

// InputSpeeds is the per-axis sensitivity bundle. Each cvar
// (cl_forwardspeed, cl_backspeed, ...) becomes a struct field so
// callers can pass an immutable snapshot instead of reaching into
// the cvar registry per-frame.
type InputSpeeds struct {
	Forward       float32 // cl_forwardspeed
	Back          float32 // cl_backspeed
	Side          float32 // cl_sidespeed
	Up            float32 // cl_upspeed
	Yaw           float32 // cl_yawspeed
	Pitch         float32 // cl_pitchspeed
	AngleSpeedKey float32 // cl_anglespeedkey -- multiplier when "+speed" is held
	MoveSpeedKey  float32 // cl_movespeedkey
}

// DefaultInputSpeeds returns the C upstream cvar defaults.
// tyrquake: cl_*speed cvar registrations in NQ/cl_input.c
// (cl_forwardspeed=200, cl_backspeed=200, cl_sidespeed=350,
// cl_upspeed=200, cl_yawspeed=140, cl_pitchspeed=150,
// cl_anglespeedkey=1.5, cl_movespeedkey=2.0).
func DefaultInputSpeeds() InputSpeeds {
	return InputSpeeds{
		Forward:       200,
		Back:          200,
		Side:          350,
		Up:            200,
		Yaw:           140,
		Pitch:         150,
		AngleSpeedKey: 1.5,
		MoveSpeedKey:  2.0,
	}
}

// MovementButtons bundles the per-axis button states (the up / down /
// left / right / forward / back keys) plus the +speed modifier flag.
//
// Left / Right are the turn-by-arrow keys (yaw rotation via
// [AdjustAngles]); MoveLeft / MoveRight are the strafe keys
// (sidemove via [BaseMove]). Upstream uses different command names
// for the same reason: +left / +right vs +moveleft / +moveright.
type MovementButtons struct {
	Forward, Back       ButtonState
	Left, Right         ButtonState // turn-by-arrow keys (NOT strafe)
	MoveLeft, MoveRight ButtonState // strafe keys
	Up, Down            ButtonState
	Lookup, Lookdown    ButtonState
	SpeedHeld           bool // +speed bound key currently held
}

// BaseMove returns the per-frame UserCmd built from button states +
// speeds + frame delta. tyrquake: CL_BaseMove in NQ/cl_input.c.
//
// Mouse + joystick deltas (CL_MouseMove / CL_JoyMove) are layered
// ON TOP via separate calls -- those add to ViewAngles directly,
// not via the button-axis path.
//
// Parameters:
//
//	buttons  current button states (with this-frame events already merged in)
//	speeds   per-axis sensitivities (typically [DefaultInputSpeeds])
//	dt       frame delta in seconds (unused here -- AdjustAngles owns it;
//	         BaseMove's outputs are velocities, not deltas)
//
// Returns the UserCmd with ForwardMove / SideMove / UpMove filled
// from button axes; ViewAngles is NOT set here (mouse + [AdjustAngles]
// own it). [server.UserCmd.Buttons] + [server.UserCmd.Impulse] are
// also left at zero -- BaseMove is the keyboard-axis path, not the
// trigger path. Callers that want clc_move to carry the +attack /
// +jump bitmask or the in_impulse one-shot byte are responsible for
// OR-ing those onto the returned cmd before handing it to
// [EncodeClcMove].
//
// Upstream's CL_BaseMove also honors the +strafe modifier (which
// re-routes the Left/Right turn keys into strafing) and +klook
// (which re-routes Forward/Back into looking). Those are encoded in
// upstream's static in_strafe / in_klook kbuttons; this Go port
// drops them for now -- the call sites that need them can wrap
// [BaseMove] and re-mux their buttons before passing them in.
func BaseMove(buttons MovementButtons, speeds InputSpeeds, dt float32) server.UserCmd {
	_ = dt // upstream CL_BaseMove also ignores host_frametime here

	var cmd server.UserCmd

	cmd.SideMove += speeds.Side * KeyState(&buttons.MoveRight)
	cmd.SideMove -= speeds.Side * KeyState(&buttons.MoveLeft)

	cmd.UpMove += speeds.Up * KeyState(&buttons.Up)
	cmd.UpMove -= speeds.Up * KeyState(&buttons.Down)

	cmd.ForwardMove += speeds.Forward * KeyState(&buttons.Forward)
	cmd.ForwardMove -= speeds.Back * KeyState(&buttons.Back)

	// +speed modifier (typically shift): scale axes by cl_movespeedkey.
	// Upstream XORs against cl_run; we treat SpeedHeld as the already-
	// XOR'd "is run/walk mode active" flag because the run-vs-walk
	// toggle is a host-side config, not per-frame input.
	if buttons.SpeedHeld {
		cmd.ForwardMove *= speeds.MoveSpeedKey
		cmd.SideMove *= speeds.MoveSpeedKey
		cmd.UpMove *= speeds.MoveSpeedKey
	}

	return cmd
}

// ApplyMouseMove adjusts viewangles by the per-frame mouse delta.
// tyrquake: IN_MouseMove (the angle-adjustment portion) in
// common/in_sdl.c / in_x11.c / in_win.c.
//
// dx, dy are pixel deltas; sensitivity is the cl_sensitivity cvar
// (default 3). The per-axis m_yaw / m_pitch multipliers (default
// 0.022 each, see NQ/cl_main.c) are folded in here.
//
// Sign conventions, matching upstream exactly:
//
//	yaw   -= m_yaw   * sensitivity * dx   (mouse right => yaw decreases;
//	                                       Q1's yaw grows CCW so this
//	                                       turns the view right)
//	pitch += m_pitch * sensitivity * dy   (mouse down  => pitch increases;
//	                                       with default m_pitch=+0.022
//	                                       this is "non-inverted look",
//	                                       i.e. dragging down looks down)
//
// Roll is left alone. Pitch is clamped to [-90, +90] to match the
// cl_minpitch / cl_maxpitch defaults from NQ/cl_main.c. The cvar
// values aren't passed in here because the upstream defaults haven't
// moved in ~25 years; if a future caller needs runtime configurability,
// extend the signature rather than re-reading a global.
func ApplyMouseMove(currentAngles [3]float32, dx, dy float32, sensitivity float32) [3]float32 {
	const mYaw float32 = 0.022   // m_yaw default
	const mPitch float32 = 0.022 // m_pitch default
	const maxPitch float32 = 90
	const minPitch float32 = -90

	mx := dx * sensitivity
	my := dy * sensitivity

	out := currentAngles
	out[mathlib.Yaw] -= mYaw * mx
	out[mathlib.Pitch] += mPitch * my

	if out[mathlib.Pitch] > maxPitch {
		out[mathlib.Pitch] = maxPitch
	}
	if out[mathlib.Pitch] < minPitch {
		out[mathlib.Pitch] = minPitch
	}
	return out
}

// AdjustAngles applies keyboard turn/look (Left / Right / Lookup /
// Lookdown buttons) to viewangles. tyrquake: CL_AdjustAngles in
// NQ/cl_input.c.
//
// The +speed modifier scales the turn rate by cl_anglespeedkey (so
// holding shift slows the keyboard turn, matching upstream's
// "fine-aim" behaviour). dt is host_frametime in seconds.
//
// Sign conventions, matching upstream exactly:
//
//	yaw   -= speed * cl_yawspeed   * KeyState(right)
//	yaw   += speed * cl_yawspeed   * KeyState(left)
//	pitch -= speed * cl_pitchspeed * KeyState(lookup)
//	pitch += speed * cl_pitchspeed * KeyState(lookdown)
//
// I.e. pressing Right DECREASES yaw and pressing Lookup DECREASES
// pitch. Q1's yaw grows CCW (mathematical convention), so "yaw
// decreases when turning right" matches a real-world right turn;
// this is the opposite sign of yaw in many modern FPS engines.
//
// Yaw is wrapped via [mathlib.AngleMod] (matching upstream's
// anglemod()); pitch is clamped to [cl_minpitch, cl_maxpitch] =
// [-90, +90]; roll is clamped to +/-50. The cvar values are hard-
// coded to the upstream defaults for the same reason as in
// [ApplyMouseMove].
func AdjustAngles(currentAngles [3]float32, buttons MovementButtons, speeds InputSpeeds, dt float32) [3]float32 {
	const maxPitch float32 = 90
	const minPitch float32 = -90
	const maxRoll float32 = 50

	var speed float32
	if buttons.SpeedHeld {
		speed = dt * speeds.AngleSpeedKey
	} else {
		speed = dt
	}

	out := currentAngles

	out[mathlib.Yaw] -= speed * speeds.Yaw * KeyState(&buttons.Right)
	out[mathlib.Yaw] += speed * speeds.Yaw * KeyState(&buttons.Left)
	out[mathlib.Yaw] = mathlib.AngleMod(out[mathlib.Yaw])

	out[mathlib.Pitch] -= speed * speeds.Pitch * KeyState(&buttons.Lookup)
	out[mathlib.Pitch] += speed * speeds.Pitch * KeyState(&buttons.Lookdown)

	if out[mathlib.Pitch] > maxPitch {
		out[mathlib.Pitch] = maxPitch
	}
	if out[mathlib.Pitch] < minPitch {
		out[mathlib.Pitch] = minPitch
	}

	if out[mathlib.Roll] > maxRoll {
		out[mathlib.Roll] = maxRoll
	}
	if out[mathlib.Roll] < -maxRoll {
		out[mathlib.Roll] = -maxRoll
	}

	return out
}
