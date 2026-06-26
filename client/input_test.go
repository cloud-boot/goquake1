// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"math"
	"testing"

	"github.com/go-quake1/engine/mathlib"
)

// --- KeyState ---------------------------------------------------------------

func TestKeyState_HeldFullFrame(t *testing.T) {
	b := ButtonState{Pressed: 1} // bit 0 only (held, no edges)
	if got := KeyState(&b); got != 1.0 {
		t.Errorf("held full frame: got %v want 1.0", got)
	}
	if b.Pressed != 1 {
		t.Errorf("held bit must survive: got %#x want 0x1", b.Pressed)
	}
}

func TestKeyState_NeverDown(t *testing.T) {
	b := ButtonState{Pressed: 0}
	if got := KeyState(&b); got != 0 {
		t.Errorf("never down: got %v want 0", got)
	}
}

func TestKeyState_PressedAndHeld(t *testing.T) {
	// down-edge + held this frame: 0.5
	b := ButtonState{Pressed: 1 | 2}
	got := KeyState(&b)
	if got != 0.5 {
		t.Errorf("pressed+held: got %v want 0.5", got)
	}
	// impulse bits cleared, held bit kept.
	if b.Pressed != 1 {
		t.Errorf("post-sample bits: got %#x want 0x1", b.Pressed)
	}
}

func TestKeyState_DownEdgeWithoutHeld(t *testing.T) {
	// down-edge fired but key reports not held -- C path returns 0.
	b := ButtonState{Pressed: 2}
	if got := KeyState(&b); got != 0 {
		t.Errorf("down-edge no-held: got %v want 0", got)
	}
	if b.Pressed != 0 {
		t.Errorf("impulse must clear: got %#x want 0", b.Pressed)
	}
}

func TestKeyState_ReleasedThisFrame(t *testing.T) {
	// up-edge only, key reports not held -- C returns 0 ("released this frame").
	b := ButtonState{Pressed: 4}
	if got := KeyState(&b); got != 0 {
		t.Errorf("released this frame: got %v want 0", got)
	}
	if b.Pressed != 0 {
		t.Errorf("impulse must clear: got %#x want 0", b.Pressed)
	}
}

func TestKeyState_ReleasedThisFrameSpuriousDown(t *testing.T) {
	// up-edge only with bit 0 still set -- C also returns 0 (FIXME branch).
	b := ButtonState{Pressed: 4 | 1}
	if got := KeyState(&b); got != 0 {
		t.Errorf("up-edge w/ stuck held: got %v want 0", got)
	}
	if b.Pressed != 1 {
		t.Errorf("only impulses clear: got %#x want 0x1", b.Pressed)
	}
}

func TestKeyState_PressedAndReleased(t *testing.T) {
	// both edges, bit 0 cleared -- "pressed and released this frame" = 0.25
	b := ButtonState{Pressed: 2 | 4}
	if got := KeyState(&b); got != 0.25 {
		t.Errorf("pressed+released: got %v want 0.25", got)
	}
}

func TestKeyState_ReleasedAndRepressed(t *testing.T) {
	// both edges, bit 0 set -- "released and re-pressed this frame" = 0.75
	b := ButtonState{Pressed: 1 | 2 | 4}
	if got := KeyState(&b); got != 0.75 {
		t.Errorf("released+repressed: got %v want 0.75", got)
	}
	if b.Pressed != 1 {
		t.Errorf("post-sample bits: got %#x want 0x1", b.Pressed)
	}
}

// --- DefaultInputSpeeds drift detector --------------------------------------

func TestDefaultInputSpeeds(t *testing.T) {
	s := DefaultInputSpeeds()
	want := InputSpeeds{
		Forward: 200, Back: 200, Side: 350, Up: 200,
		Yaw: 140, Pitch: 150,
		AngleSpeedKey: 1.5, MoveSpeedKey: 2.0,
		MouseYaw: 0.022, MousePitch: 0.022,
	}
	if s != want {
		t.Errorf("cvar default drift:\n got  %+v\n want %+v", s, want)
	}
}

// TestDefaultMouseYawPitchConstants pins the upstream m_yaw / m_pitch
// cvar defaults (NQ/cl_main.c) so a future stylistic refactor of the
// exported constants can't silently drift the on-screen sensitivity.
func TestDefaultMouseYawPitchConstants(t *testing.T) {
	if DefaultMouseYaw != 0.022 {
		t.Errorf("DefaultMouseYaw drift: got %v want 0.022", DefaultMouseYaw)
	}
	if DefaultMousePitch != 0.022 {
		t.Errorf("DefaultMousePitch drift: got %v want 0.022", DefaultMousePitch)
	}
}

// --- BaseMove ---------------------------------------------------------------

// held returns a ButtonState that KeyState will report as 1.0.
func held() ButtonState { return ButtonState{Pressed: 1} }

func TestBaseMove_ForwardHeld(t *testing.T) {
	b := MovementButtons{Forward: held()}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.ForwardMove != 200 {
		t.Errorf("forward-only: ForwardMove=%v want 200", cmd.ForwardMove)
	}
	if cmd.SideMove != 0 || cmd.UpMove != 0 {
		t.Errorf("axis cross-talk: %+v", cmd)
	}
}

func TestBaseMove_ForwardAndBackCancel(t *testing.T) {
	b := MovementButtons{Forward: held(), Back: held()}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	// 200 (forward) - 200 (back) = 0.
	if cmd.ForwardMove != 0 {
		t.Errorf("opposed forward/back: got %v want 0", cmd.ForwardMove)
	}
}

func TestBaseMove_StrafeLeft(t *testing.T) {
	b := MovementButtons{MoveLeft: held()}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.SideMove != -350 {
		t.Errorf("strafe-left: SideMove=%v want -350", cmd.SideMove)
	}
}

func TestBaseMove_StrafeRight(t *testing.T) {
	b := MovementButtons{MoveRight: held()}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.SideMove != 350 {
		t.Errorf("strafe-right: SideMove=%v want 350", cmd.SideMove)
	}
}

func TestBaseMove_UpDown(t *testing.T) {
	b := MovementButtons{Up: held()}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.UpMove != 200 {
		t.Errorf("up: UpMove=%v want 200", cmd.UpMove)
	}
	b = MovementButtons{Down: held()}
	cmd = BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.UpMove != -200 {
		t.Errorf("down: UpMove=%v want -200", cmd.UpMove)
	}
}

func TestBaseMove_SpeedKeyScales(t *testing.T) {
	// Forward + strafe + up held with +speed -> all three scale by 2.0.
	b := MovementButtons{
		Forward:   held(),
		MoveRight: held(),
		Up:        held(),
		SpeedHeld: true,
	}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.ForwardMove != 400 {
		t.Errorf("speed-key forward: %v want 400", cmd.ForwardMove)
	}
	if cmd.SideMove != 700 {
		t.Errorf("speed-key side: %v want 700", cmd.SideMove)
	}
	if cmd.UpMove != 400 {
		t.Errorf("speed-key up: %v want 400", cmd.UpMove)
	}
}

// TestBaseMove_ImpulseDrainsAcrossFrames is the regression for the
// "player walks at 25-50% of cl_*speed" bug: KeyState clears the
// per-frame impulse bits as it samples them (tyrquake CL_KeyState
// does the same on the static kbutton_t globals). If BaseMove took
// MovementButtons by value, the drain would land on a stack copy and
// the caller's persistent state would keep the down-edge bit forever
// -- KeyState would then report 0.5 on every subsequent frame the key
// stayed held (down-edge still set + down=1 -> 0.5), collapsing
// ForwardMove to cl_forwardspeed/2 (=100 with the vanilla 200 default,
// the symptom on bare-metal QEMU). The pointer signature wires the
// drain back to the caller's struct: first sample sees impulse-down +
// held (0.5), the SECOND sample on the same struct sees only held
// (1.0). Pin both samples here so any future refactor that drops the
// pointer (or introduces a stray copy) trips the test.
func TestBaseMove_ImpulseDrainsAcrossFrames(t *testing.T) {
	b := MovementButtons{
		Forward: ButtonState{Pressed: 0b011}, // held + down-edge ("first frame pressed")
	}
	s := DefaultInputSpeeds()

	// Frame 1: down-edge still set -> KeyState = 0.5 -> ForwardMove = 100.
	cmd := BaseMove(&b, s, 0.05)
	if cmd.ForwardMove != 100 {
		t.Fatalf("frame 1 ForwardMove = %v want 100 (0.5 * 200)", cmd.ForwardMove)
	}
	if b.Forward.Pressed != 0b001 {
		t.Fatalf("frame 1: impulse bits not drained from caller state; Pressed=%b want 001", b.Forward.Pressed)
	}

	// Frame 2: no edges, just held -> KeyState = 1.0 -> ForwardMove = 200.
	cmd = BaseMove(&b, s, 0.05)
	if cmd.ForwardMove != 200 {
		t.Fatalf("frame 2 ForwardMove = %v want 200 (1.0 * 200) -- impulses must drain from caller state, not a copy", cmd.ForwardMove)
	}
}

func TestBaseMove_ViewAnglesUntouched(t *testing.T) {
	// BaseMove must not touch viewangles -- that's mouse + AdjustAngles' job.
	b := MovementButtons{Forward: held(), MoveLeft: held()}
	cmd := BaseMove(&b, DefaultInputSpeeds(), 0.05)
	if cmd.ViewAngles != ([3]float32{}) {
		t.Errorf("ViewAngles must be zero: %v", cmd.ViewAngles)
	}
}

// --- ApplyMouseMove ---------------------------------------------------------

func TestApplyMouseMove_YawNegativeOnRight(t *testing.T) {
	// dx=100, sens=3, m_yaw=0.022 -> yaw -= 0.022 * 3 * 100 = -6.6
	// Tolerance covers AngleMod's 360/65536 (~0.0055 deg) fixed-point
	// step on top of float32 round-off.
	angles := [3]float32{0, 50, 0}
	out := ApplyMouseMove(angles, 100, 0, 3, DefaultMouseYaw, DefaultMousePitch)
	want := float32(50 - 0.022*3*100)
	if !approxEq(out[mathlib.Yaw], want, 0.01) {
		t.Errorf("yaw on dx=100: got %v want %v", out[mathlib.Yaw], want)
	}
	if out[mathlib.Pitch] != 0 || out[mathlib.Roll] != 0 {
		t.Errorf("pitch/roll cross-talk: %v", out)
	}
}

func TestApplyMouseMove_PitchIncreasesOnDyPositive(t *testing.T) {
	// Default m_pitch=+0.022, so dy>0 (mouse moved DOWN) -> pitch INCREASES
	// (i.e. view tilts DOWN -- "non-inverted" look). Spec said "decreases";
	// matches the C source instead. dy=10, sens=3 -> +0.022*3*10 = +0.66
	angles := [3]float32{0, 0, 0}
	out := ApplyMouseMove(angles, 0, 10, 3, DefaultMouseYaw, DefaultMousePitch)
	want := float32(0.022 * 3 * 10)
	if !approxEq(out[mathlib.Pitch], want, 1e-4) {
		t.Errorf("pitch on dy=10: got %v want %v", out[mathlib.Pitch], want)
	}
}

func TestApplyMouseMove_PitchClampHigh(t *testing.T) {
	// dy huge positive: pitch must clamp at +89 (one degree inside
	// the upstream cl_maxpitch=+90 hard limit so the view never hits
	// the AngleVectors gimbal-lock singularity).
	out := ApplyMouseMove([3]float32{}, 0, 1e6, 3, DefaultMouseYaw, DefaultMousePitch)
	if out[mathlib.Pitch] != 89 {
		t.Errorf("pitch upper clamp: got %v want 89", out[mathlib.Pitch])
	}
}

func TestApplyMouseMove_PitchClampLow(t *testing.T) {
	out := ApplyMouseMove([3]float32{}, 0, -1e6, 3, DefaultMouseYaw, DefaultMousePitch)
	if out[mathlib.Pitch] != -89 {
		t.Errorf("pitch lower clamp: got %v want -89", out[mathlib.Pitch])
	}
}

func TestApplyMouseMove_RollUntouched(t *testing.T) {
	in := [3]float32{0, 0, 17}
	out := ApplyMouseMove(in, 100, 100, 3, DefaultMouseYaw, DefaultMousePitch)
	if out[mathlib.Roll] != 17 {
		t.Errorf("roll mutated: got %v want 17", out[mathlib.Roll])
	}
}

// TestApplyMouseMove_YawDefaultSensitivity asserts the per-pixel
// yaw delta at the runloop's default Sensitivity=1: dx=100 must
// rotate yaw by exactly m_yaw*100 = 2.2 deg in the "yaw decreases"
// direction (Q1 sign convention; mouse-right turns the view right
// because yaw grows CCW). Anchors the canonical wiring contract
// between [backend.InputSnapshot.MouseDX] and the on-screen view.
func TestApplyMouseMove_YawDefaultSensitivity(t *testing.T) {
	const start float32 = 50
	out := ApplyMouseMove([3]float32{0, start, 0}, 100, 0, 1, DefaultMouseYaw, DefaultMousePitch)
	// 50 - 0.022*100 = 47.8; AngleMod's 360/65536 (~0.0055 deg)
	// fixed-point quantisation lands the result at 47.79602. The
	// tolerance covers that step plus float32 round-off.
	const want float32 = 47.8
	if !approxEq(out[mathlib.Yaw], want, 0.01) {
		t.Errorf("yaw on dx=100 sens=1: got %v want %v (start=%v, m_yaw=0.022)",
			out[mathlib.Yaw], want, start)
	}
}

// TestApplyMouseMove_MYawConfigurable proves the m_yaw multiplier
// is honoured: doubling it doubles the yaw delta for the same dx.
func TestApplyMouseMove_MYawConfigurable(t *testing.T) {
	const start float32 = 50
	out := ApplyMouseMove([3]float32{0, start, 0}, 100, 0, 1, 0.044, DefaultMousePitch)
	// dx=100, sens=1, mYaw=0.044 -> yaw -= 4.4 -> 45.6
	const want float32 = 45.6
	if !approxEq(out[mathlib.Yaw], want, 0.01) {
		t.Errorf("yaw with mYaw=0.044: got %v want %v", out[mathlib.Yaw], want)
	}
}

// TestApplyMouseMove_MPitchConfigurable proves the m_pitch
// multiplier is honoured: setting it negative produces "inverted
// look" (mouse-down looks UP), the upstream toggle.
func TestApplyMouseMove_MPitchConfigurable(t *testing.T) {
	out := ApplyMouseMove([3]float32{0, 0, 0}, 0, 10, 3, DefaultMouseYaw, -0.022)
	// dy=+10, sens=3, mPitch=-0.022 -> pitch += -0.66 -> -0.66
	const want float32 = -0.66
	if !approxEq(out[mathlib.Pitch], want, 1e-4) {
		t.Errorf("pitch with mPitch=-0.022 (inverted look): got %v want %v",
			out[mathlib.Pitch], want)
	}
}

// TestApplyMouseMove_MYawZeroDisablesYaw proves the upstream
// "cvar zeroed -> axis disabled" behaviour: mYaw=0 must leave yaw
// untouched regardless of dx.
func TestApplyMouseMove_MYawZeroDisablesYaw(t *testing.T) {
	const start float32 = 42
	out := ApplyMouseMove([3]float32{0, start, 0}, 9999, 0, 5, 0, DefaultMousePitch)
	// AngleMod wraps the unchanged value; 42 stays 42 (within
	// AngleMod's ~0.0055-deg quantisation step).
	if !approxEq(out[mathlib.Yaw], start, 0.01) {
		t.Errorf("yaw moved with mYaw=0: got %v want ~%v", out[mathlib.Yaw], start)
	}
}

// TestApplyMouseMove_YawWraps proves the [mathlib.AngleMod] wrap
// fires on prolonged mouse-left drift: starting at yaw=1, a large
// negative-effective dx (mouse-left, yaw INCREASES) crosses 360
// and lands back near 0 instead of growing unboundedly.
func TestApplyMouseMove_YawWraps(t *testing.T) {
	// dx=-20000, sens=1: yaw += 0.022*20000 = +440 -> 441 (start=1).
	// AngleMod(441) = 81 (440 - 360).
	out := ApplyMouseMove([3]float32{0, 1, 0}, -20000, 0, 1, DefaultMouseYaw, DefaultMousePitch)
	if out[mathlib.Yaw] < 0 || out[mathlib.Yaw] >= 360 {
		t.Errorf("yaw not wrapped into [0,360): got %v", out[mathlib.Yaw])
	}
	// AngleMod uses the tyrquake fixed-point trick; allow a 1-step
	// (360/65536 ~= 0.0055 deg) jitter against the naive want=81.
	if !approxEq(out[mathlib.Yaw], 81, 0.05) {
		t.Errorf("yaw wrap landing: got %v want ~81", out[mathlib.Yaw])
	}
}

// --- AdjustAngles -----------------------------------------------------------

func TestAdjustAngles_LookupDecreasesPitch(t *testing.T) {
	b := MovementButtons{Lookup: held()}
	in := [3]float32{0, 0, 0}
	out := AdjustAngles(in, &b, DefaultInputSpeeds(), 0.1)
	// pitch -= 0.1 * 150 * 1.0 = -15
	if !approxEq(out[mathlib.Pitch], -15, 1e-4) {
		t.Errorf("lookup pitch: got %v want -15", out[mathlib.Pitch])
	}
}

func TestAdjustAngles_LookdownIncreasesPitch(t *testing.T) {
	b := MovementButtons{Lookdown: held()}
	out := AdjustAngles([3]float32{}, &b, DefaultInputSpeeds(), 0.1)
	if !approxEq(out[mathlib.Pitch], 15, 1e-4) {
		t.Errorf("lookdown pitch: got %v want 15", out[mathlib.Pitch])
	}
}

func TestAdjustAngles_RightDecreasesYaw(t *testing.T) {
	// Q1 yaw grows CCW: right turn => yaw decreases.
	b := MovementButtons{Right: held()}
	in := [3]float32{0, 100, 0}
	out := AdjustAngles(in, &b, DefaultInputSpeeds(), 0.1)
	// yaw -= 0.1 * 140 = 14 -> 86, then AngleMod wraps to [0, 360).
	want := mathlib.AngleMod(86)
	if !approxEq(out[mathlib.Yaw], want, 1e-3) {
		t.Errorf("right yaw: got %v want %v", out[mathlib.Yaw], want)
	}
}

func TestAdjustAngles_LeftIncreasesYaw(t *testing.T) {
	b := MovementButtons{Left: held()}
	in := [3]float32{0, 100, 0}
	out := AdjustAngles(in, &b, DefaultInputSpeeds(), 0.1)
	want := mathlib.AngleMod(114)
	if !approxEq(out[mathlib.Yaw], want, 1e-3) {
		t.Errorf("left yaw: got %v want %v", out[mathlib.Yaw], want)
	}
}

func TestAdjustAngles_SpeedKeyScalesAngleRate(t *testing.T) {
	b := MovementButtons{Lookdown: held(), SpeedHeld: true}
	out := AdjustAngles([3]float32{}, &b, DefaultInputSpeeds(), 0.1)
	// speed = 0.1 * 1.5 = 0.15; pitch += 0.15 * 150 = 22.5
	if !approxEq(out[mathlib.Pitch], 22.5, 1e-4) {
		t.Errorf("speed-key pitch: got %v want 22.5", out[mathlib.Pitch])
	}
}

func TestAdjustAngles_PitchClampHigh(t *testing.T) {
	b := MovementButtons{Lookdown: held()}
	// Start at +89, big dt -> would overshoot 90, must clamp.
	out := AdjustAngles([3]float32{89, 0, 0}, &b, DefaultInputSpeeds(), 10)
	if out[mathlib.Pitch] != 90 {
		t.Errorf("pitch upper clamp: got %v want 90", out[mathlib.Pitch])
	}
}

func TestAdjustAngles_PitchClampLow(t *testing.T) {
	b := MovementButtons{Lookup: held()}
	out := AdjustAngles([3]float32{-89, 0, 0}, &b, DefaultInputSpeeds(), 10)
	if out[mathlib.Pitch] != -90 {
		t.Errorf("pitch lower clamp: got %v want -90", out[mathlib.Pitch])
	}
}

func TestAdjustAngles_RollClampHigh(t *testing.T) {
	// Roll input not driven by buttons -- the clamp triggers on
	// whatever the caller passes in.
	out := AdjustAngles([3]float32{0, 0, 75}, &MovementButtons{}, DefaultInputSpeeds(), 0.1)
	if out[mathlib.Roll] != 50 {
		t.Errorf("roll upper clamp: got %v want 50", out[mathlib.Roll])
	}
}

func TestAdjustAngles_RollClampLow(t *testing.T) {
	out := AdjustAngles([3]float32{0, 0, -75}, &MovementButtons{}, DefaultInputSpeeds(), 0.1)
	if out[mathlib.Roll] != -50 {
		t.Errorf("roll lower clamp: got %v want -50", out[mathlib.Roll])
	}
}

func TestAdjustAngles_NoSpeedKey(t *testing.T) {
	// SpeedHeld=false branch (the other branch is exercised above).
	b := MovementButtons{Lookdown: held(), SpeedHeld: false}
	out := AdjustAngles([3]float32{}, &b, DefaultInputSpeeds(), 0.1)
	if !approxEq(out[mathlib.Pitch], 15, 1e-4) {
		t.Errorf("no-speed-key pitch: got %v want 15", out[mathlib.Pitch])
	}
}

// --- helpers ----------------------------------------------------------------

func approxEq(a, b, eps float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return float64(d) < float64(eps) || math.IsNaN(float64(a)) == math.IsNaN(float64(b)) && a == b
}
