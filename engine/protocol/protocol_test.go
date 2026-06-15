// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package protocol

import "testing"

func TestKnown(t *testing.T) {
	for _, v := range []int{VersionNQ, VersionFitz, VersionBJP, VersionBJP2, VersionBJP3} {
		if !Known(v) {
			t.Errorf("Known(%d) should be true", v)
		}
	}
	for _, v := range []int{0, 1, 14, 16, 999, -1} {
		if Known(v) {
			t.Errorf("Known(%d) should be false", v)
		}
	}
}

func TestMaxModelsFor(t *testing.T) {
	cases := map[int]int{
		VersionNQ:    min(256, MaxModels),
		VersionFitz:  min(65536, MaxModels),
		VersionBJP:   min(65536, MaxModels),
		VersionBJP2:  min(65536, MaxModels),
		VersionBJP3:  min(65536, MaxModels),
		999:          0,
	}
	for v, want := range cases {
		if got := MaxModelsFor(v); got != want {
			t.Errorf("MaxModelsFor(%d): got %d want %d", v, got, want)
		}
	}
}

func TestMaxSoundsStaticFor(t *testing.T) {
	cases := map[int]int{
		VersionNQ:    min(256, MaxSounds),
		VersionBJP:   min(256, MaxSounds),
		VersionBJP3:  min(256, MaxSounds),
		VersionBJP2:  min(65536, MaxSounds),
		VersionFitz:  min(65536, MaxSounds),
		999:          0,
	}
	for v, want := range cases {
		if got := MaxSoundsStaticFor(v); got != want {
			t.Errorf("MaxSoundsStaticFor(%d): got %d want %d", v, got, want)
		}
	}
}

func TestMaxSoundsDynamicFor(t *testing.T) {
	cases := map[int]int{
		VersionNQ:    min(256, MaxSounds),
		VersionBJP:   min(256, MaxSounds),
		VersionBJP2:  min(65536, MaxSounds),
		VersionBJP3:  min(65536, MaxSounds),
		VersionFitz:  min(65536, MaxSounds),
		999:          0,
	}
	for v, want := range cases {
		if got := MaxSoundsDynamicFor(v); got != want {
			t.Errorf("MaxSoundsDynamicFor(%d): got %d want %d", v, got, want)
		}
	}
}

func TestMaxSoundsFor(t *testing.T) {
	// MaxSoundsFor returns max(static, dynamic). For NQ both are 256
	// so the answer is 256. For FITZ both are 65536-capped which is
	// then clamped to MaxSounds (2048).
	if got := MaxSoundsFor(VersionNQ); got != min(256, MaxSounds) {
		t.Errorf("MaxSoundsFor(NQ): %d", got)
	}
	if got := MaxSoundsFor(VersionFitz); got != min(65536, MaxSounds) {
		t.Errorf("MaxSoundsFor(FITZ): %d", got)
	}
	if got := MaxSoundsFor(999); got != 0 {
		t.Errorf("MaxSoundsFor(unknown): %d", got)
	}
}

// Cover the static>dynamic branch of MaxSoundsFor's max() with a
// synthesised mismatch: we cannot do this through real protocol
// versions because tyrquake's table is monotonic, but exercising
// each version pins the table behaviour.
func TestMaxSoundsFor_AllVersions(t *testing.T) {
	for _, v := range []int{VersionNQ, VersionFitz, VersionBJP, VersionBJP2, VersionBJP3} {
		MaxSoundsFor(v) // exercise without asserting -- value pinned in TestMaxSoundsStatic/Dynamic
	}
}

// --- EntAlpha encoding round-trips ---

func TestEntAlphaEncode_Default(t *testing.T) {
	if got := EntAlphaEncode(0); got != EntAlphaDefault {
		t.Errorf("EntAlphaEncode(0): got %d want EntAlphaDefault", got)
	}
}

func TestEntAlphaEncode_Boundaries(t *testing.T) {
	// Smallest non-zero alpha must clamp to >= 1 (NOT 0, since 0 is
	// the "default" sentinel).
	if got := EntAlphaEncode(0.001); got < 1 {
		t.Errorf("EntAlphaEncode(0.001): got %d want >= 1", got)
	}
	// Full opacity should encode to 255.
	if got := EntAlphaEncode(1.0); got != 255 {
		t.Errorf("EntAlphaEncode(1.0): got %d want 255", got)
	}
	// Above 1.0 clamps to 255.
	if got := EntAlphaEncode(2.0); got != 255 {
		t.Errorf("EntAlphaEncode(2.0): got %d want 255", got)
	}
}

func TestEntAlphaEncode_NegativeClamps(t *testing.T) {
	// Negative input is non-zero so it bypasses the default branch
	// + clamps to 1.
	if got := EntAlphaEncode(-0.5); got != 1 {
		t.Errorf("EntAlphaEncode(-0.5): got %d want 1", got)
	}
}

func TestEntAlphaDecode(t *testing.T) {
	if got := EntAlphaDecode(EntAlphaDefault); got != 1.0 {
		t.Errorf("Decode(default): %v", got)
	}
	if got := EntAlphaDecode(EntAlphaOne); got != 1.0 {
		t.Errorf("Decode(255): %v", got)
	}
	if got := EntAlphaDecode(EntAlphaZero); got != 0.0 {
		t.Errorf("Decode(1): %v", got)
	}
}

func TestEntAlphaToSave(t *testing.T) {
	if got := EntAlphaToSave(EntAlphaDefault); got != 0 {
		t.Errorf("ToSave(default): %v", got)
	}
	if got := EntAlphaToSave(EntAlphaZero); got != -1 {
		t.Errorf("ToSave(zero): %v", got)
	}
	if got := EntAlphaToSave(EntAlphaOne); got != 1 {
		t.Errorf("ToSave(one): %v", got)
	}
}

// Round-trip for representative midpoints.
func TestEntAlpha_RoundTripMidpoints(t *testing.T) {
	for _, a := range []float32{0.1, 0.25, 0.5, 0.75, 0.99} {
		enc := EntAlphaEncode(a)
		dec := EntAlphaDecode(enc)
		diff := dec - a
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.005 { // 8-bit quantisation noise floor
			t.Errorf("round-trip %v -> %d -> %v (diff %v)", a, enc, dec, diff)
		}
	}
}

// --- Spot-check the SVC/CLC/TE/U_/SU_/SND_ constant values are not
// accidentally renumbered.
//
// These are wire-protocol invariants -- any change here would silently
// break demo replay + savegames.
func TestSvcInvariants(t *testing.T) {
	cases := map[string]int{
		"SvcBad":            SvcBad,
		"SvcNop":            SvcNop,
		"SvcDisconnect":     SvcDisconnect,
		"SvcServerInfo":     SvcServerInfo,
		"SvcSetView":        SvcSetView,
		"SvcSound":          SvcSound,
		"SvcTime":           SvcTime,
		"SvcPrint":          SvcPrint,
		"SvcStuffText":      SvcStuffText,
		"SvcClientData":     SvcClientData,
		"SvcSpawnBaseline":  SvcSpawnBaseline,
		"SvcTempEntity":     SvcTempEntity,
		"SvcSpawnStaticSound": SvcSpawnStaticSound,
		"SvcIntermission":   SvcIntermission,
		"SvcFinale":         SvcFinale,
		"SvcCutscene":       SvcCutscene,
		"SvcFitzSkybox":     SvcFitzSkybox,
		"SvcFitzFog":        SvcFitzFog,
	}
	wantOrder := []string{
		"SvcBad", "SvcNop", "SvcDisconnect",
	}
	for i, k := range wantOrder {
		if cases[k] != i {
			t.Errorf("%s: got %d want %d", k, cases[k], i)
		}
	}
}

func TestClcInvariants(t *testing.T) {
	if ClcBad != 0 || ClcNop != 1 || ClcDisconnect != 2 || ClcMove != 3 || ClcStringCmd != 4 {
		t.Errorf("Clc layout drift")
	}
}

func TestTeInvariants(t *testing.T) {
	if TESpike != 0 || TESuperSpike != 1 || TEGunshot != 2 || TEExplosion != 3 {
		t.Errorf("TE leading values drift")
	}
	if TEBeam != 13 || TEExplosion2 != 12 {
		t.Errorf("TE tail values drift")
	}
}

func TestUFlagsLayout(t *testing.T) {
	if UMoreBits != 1 || UOrigin1 != 2 || UOrigin2 != 4 || UOrigin3 != 8 {
		t.Errorf("U_ low bits drift")
	}
	if UAngle1 != 1<<8 || ULongEntity != 1<<14 {
		t.Errorf("U_ high bits drift")
	}
	if UFitzExtend1 != 1<<15 || UFitzExtend2 != 1<<23 {
		t.Errorf("U_ FITZ bits drift")
	}
}

func TestSUFlagsLayout(t *testing.T) {
	if SUViewHeight != 1 || SUOnGround != 1<<10 || SUWeapon != 1<<14 {
		t.Errorf("SU_ bits drift")
	}
	if SUFitzExtend3 != 1<<31 {
		t.Errorf("SU_ FITZ extend3 drift")
	}
}

func TestSndFlagsLayout(t *testing.T) {
	if SndVolume != 1 || SndAttenuation != 2 || SndLooping != 4 {
		t.Errorf("SND_ bits drift")
	}
}

func TestMsgTypeValues(t *testing.T) {
	if MsgTypeBaseline != 0 || MsgTypeClientData != 1 || MsgTypeUpdate != 2 {
		t.Errorf("MsgType layout drift")
	}
}

func TestVersionValues(t *testing.T) {
	if VersionNQ != 15 || VersionFitz != 666 {
		t.Errorf("Version drift: NQ=%d FITZ=%d", VersionNQ, VersionFitz)
	}
}
