// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sound

import "testing"

func TestSpatialize_ZeroDistanceCenter(t *testing.T) {
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{100, 100, 100},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{100, 100, 100}, // same point
		MasterVolume:   200,
		Attenuation:    AttenuationNormal,
	})
	// At distance 0 + AttenuationNormal: master = 1, balance = 0,
	// so each ear gets MasterVolume * 1 * 0.5 = 100.
	if out.LeftVol != 100 || out.RightVol != 100 {
		t.Fatalf("zero-dist L/R = %d/%d want 100/100", out.LeftVol, out.RightVol)
	}
}

func TestSpatialize_RightOfListener(t *testing.T) {
	// Sound is +Y of listener; ListenerRight = +Y.
	// dx=0, dy=10, dz=0; dist=10
	// balance = (0*0 + 10*1 + 0*0) / 10 = 1 (fully right ear)
	// master = 1 - 10*0.001*1 = 0.99
	// scaledBase = 200 * 0.99 = 198
	// leftScale = 0, rightScale = 1
	// L = 0, R = 198
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{0, 10, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNormal,
	})
	if out.LeftVol != 0 {
		t.Fatalf("LeftVol = %d want 0 (sound is fully right)", out.LeftVol)
	}
	if out.RightVol != 198 {
		t.Fatalf("RightVol = %d want 198", out.RightVol)
	}
}

func TestSpatialize_LeftOfListener(t *testing.T) {
	// Sound at -Y; balance -1; left ear only.
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{0, -10, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNormal,
	})
	if out.RightVol != 0 {
		t.Fatalf("RightVol = %d want 0 (sound is fully left)", out.RightVol)
	}
	if out.LeftVol != 198 {
		t.Fatalf("LeftVol = %d want 198", out.LeftVol)
	}
}

func TestSpatialize_BeyondFalloff(t *testing.T) {
	// At 1000 units with AttenuationNormal -> master = 0
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{0, 2000, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNormal,
	})
	if out.LeftVol != 0 || out.RightVol != 0 {
		t.Fatalf("beyond-falloff L/R = %d/%d want 0/0", out.LeftVol, out.RightVol)
	}
}

func TestSpatialize_NoAttenuation(t *testing.T) {
	// AttenuationNone -> master stays 1 regardless of distance.
	// At dist 10000, balance still computed from direction.
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{0, 10000, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNone,
	})
	// fully right: L=0, R=200
	if out.LeftVol != 0 || out.RightVol != 200 {
		t.Fatalf("NoAttenuation L/R = %d/%d want 0/200", out.LeftVol, out.RightVol)
	}
}

func TestSpatialize_NoAttenuationCenter(t *testing.T) {
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{0, 0, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNone,
	})
	// dist 0 -> balance 0 -> L=R=100
	if out.LeftVol != 100 || out.RightVol != 100 {
		t.Fatalf("center L/R = %d/%d want 100/100", out.LeftVol, out.RightVol)
	}
}

func TestSpatialize_MasterClampHigh(t *testing.T) {
	// Negative distance can't happen physically but a fabricated
	// scenario where the master scale would exceed 1 needs the
	// clamp arm to be hit. We construct one via a degenerate
	// attenuation value (-1) so 1 - d*0.001*(-1) = 1 + d*0.001 > 1.
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 1, 0},
		SoundOrigin:    [3]float32{0, 1000, 0},
		MasterVolume:   200,
		Attenuation:    SoundAttenuation(-1),
	})
	// At dist 1000 + atten -1: master = 1 - 1000*0.001*(-1) = 2 -> clamp to 1
	// fully right ear: L=0, R=200
	if out.RightVol != 200 {
		t.Fatalf("master>1 clamp: RightVol = %d want 200", out.RightVol)
	}
}

func TestSpatialize_NegativeClampLeft(t *testing.T) {
	// Non-unit ListenerRight + sound aligned with it -> balance > 1
	// -> leftScale = (1 - balance) * 0.5 < 0 -> left volume clamps to 0.
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 2, 0}, // length 2
		SoundOrigin:    [3]float32{0, 10, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNone,
	})
	// balance = (0 + 10*2 + 0) / 10 = 2 (> 1)
	// leftScale = (1-2)/2 = -0.5 -> left = -100 -> clamp 0
	if out.LeftVol != 0 {
		t.Fatalf("balance>1 LeftVol = %d want 0 (clamp)", out.LeftVol)
	}
}

func TestSpatialize_NegativeClampRight(t *testing.T) {
	// Symmetric: balance < -1 -> rightScale < 0 -> right clamps to 0.
	out := Spatialize(SpatializeIn{
		ListenerOrigin: [3]float32{0, 0, 0},
		ListenerRight:  [3]float32{0, 2, 0},
		SoundOrigin:    [3]float32{0, -10, 0},
		MasterVolume:   200,
		Attenuation:    AttenuationNone,
	})
	if out.RightVol != 0 {
		t.Fatalf("balance<-1 RightVol = %d want 0 (clamp)", out.RightVol)
	}
}

func TestSpatialize_AttenuationConstants(t *testing.T) {
	// Drift detector.
	if AttenuationNone != 0 || AttenuationNormal != 1 || AttenuationIdle != 2 || AttenuationStatic != 3 {
		t.Fatalf("attenuation enum drift")
	}
	if SoundFalloffDist != 1000 || MaxVolume != 255 {
		t.Fatalf("sound constants drift")
	}
}
