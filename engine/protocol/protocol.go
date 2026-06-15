// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package protocol

import "math"

// Protocol version magic. Tyrquake supports the original NQ + Fitz +
// three Bengt-Jardrup extensions.
const (
	VersionNQ   = 15
	VersionFitz = 666
	VersionBJP  = 10000
	VersionBJP2 = 10001
	VersionBJP3 = 10002
)

// MaxModels / MaxSounds bounds the upstream uses to size its arrays.
// The header value the QuakeC compiler emits is hard-coded; we
// surface it so callers can reuse it.
const (
	MaxModels = 2048
	MaxSounds = 2048
)

// Known reports whether v is a protocol version this build accepts.
// tyrquake: Protocol_Known.
func Known(v int) bool {
	switch v {
	case VersionNQ, VersionFitz, VersionBJP, VersionBJP2, VersionBJP3:
		return true
	}
	return false
}

// MaxModelsFor returns the on-wire model-index ceiling for protocol
// v. tyrquake: max_models.
func MaxModelsFor(v int) int {
	switch v {
	case VersionNQ:
		return min(256, MaxModels)
	case VersionBJP, VersionBJP2, VersionBJP3, VersionFitz:
		return min(65536, MaxModels)
	}
	return 0
}

// MaxSoundsStaticFor returns the static-sound-index ceiling.
// tyrquake: max_sounds_static.
func MaxSoundsStaticFor(v int) int {
	switch v {
	case VersionNQ, VersionBJP, VersionBJP3:
		return min(256, MaxSounds)
	case VersionBJP2, VersionFitz:
		return min(65536, MaxSounds)
	}
	return 0
}

// MaxSoundsDynamicFor returns the dynamic-sound-index ceiling.
// tyrquake: max_sounds_dynamic.
func MaxSoundsDynamicFor(v int) int {
	switch v {
	case VersionNQ, VersionBJP:
		return min(256, MaxSounds)
	case VersionBJP2, VersionBJP3, VersionFitz:
		return min(65536, MaxSounds)
	}
	return 0
}

// MaxSoundsFor returns the max of the static + dynamic ceilings.
// For every canonical protocol version the dynamic ceiling is >=
// the static one (NQ/BJP: 256==256; BJP3: 256<2048; BJP2/FITZ:
// 2048==2048), so the dynamic value alone is sufficient. Unknown
// versions yield 0 from both. tyrquake: max_sounds (which expressed
// it as a defensive qmax that simplifies away here).
func MaxSoundsFor(v int) int {
	return MaxSoundsDynamicFor(v)
}

// MsgType is the cl_parse-side tag distinguishing baseline /
// clientdata / update reads (each has different flag-bit semantics
// for the model + frame index width).
type MsgType int

const (
	MsgTypeBaseline MsgType = iota
	MsgTypeClientData
	MsgTypeUpdate
)

// U_* are the "fast update" bit flags. Low bits piggyback the
// servercmd byte; high bits (>= U_ANGLE1) come from a follow-up
// short read in svc_update.
const (
	UMoreBits  = 1 << 0
	UOrigin1   = 1 << 1
	UOrigin2   = 1 << 2
	UOrigin3   = 1 << 3
	UAngle2    = 1 << 4
	UNoLerp    = 1 << 5
	UFrame     = 1 << 6
	USignal    = 1 << 7
	UAngle1    = 1 << 8
	UAngle3    = 1 << 9
	UModel     = 1 << 10
	UColorMap  = 1 << 11
	USkin      = 1 << 12
	UEffects   = 1 << 13
	ULongEntity = 1 << 14

	// FITZ extensions.
	UFitzExtend1    = 1 << 15
	UFitzAlpha      = 1 << 16
	UFitzFrame2     = 1 << 17
	UFitzModel2     = 1 << 18
	UFitzLerpFinish = 1 << 19
	UFitzExtend2    = 1 << 23
)

// SU_* are the svc_clientdata "shortbits" flags describing which
// player-state fields the server is sending this frame.
const (
	SUViewHeight   = 1 << 0
	SUIdealPitch   = 1 << 1
	SUPunch1       = 1 << 2
	SUPunch2       = 1 << 3
	SUPunch3       = 1 << 4
	SUVelocity1    = 1 << 5
	SUVelocity2    = 1 << 6
	SUVelocity3    = 1 << 7
	// bit 8 is reserved (SU_AIMENT in some forks).
	SUItems        = 1 << 9
	SUOnGround     = 1 << 10
	SUInWater      = 1 << 11
	SUWeaponFrame  = 1 << 12
	SUArmor        = 1 << 13
	SUWeapon       = 1 << 14

	// FITZ extensions.
	SUFitzExtend1      = 1 << 15
	SUFitzWeapon2      = 1 << 16
	SUFitzArmor2       = 1 << 17
	SUFitzAmmo2        = 1 << 18
	SUFitzShells2      = 1 << 19
	SUFitzNails2       = 1 << 20
	SUFitzRockets2     = 1 << 21
	SUFitzCells2       = 1 << 22
	SUFitzExtend2      = 1 << 23
	SUFitzWeaponFrame2 = 1 << 24
	SUFitzWeaponAlpha  = 1 << 25
	SUFitzExtend3      = 1 << 31
)

// SND_* are svc_sound flag bits.
const (
	SndVolume      = 1 << 0
	SndAttenuation = 1 << 1
	SndLooping     = 1 << 2

	// FITZ extensions.
	SndFitzLargeEntity = 1 << 3
	SndFitzLargeSound  = 1 << 4
)

// B_FITZ_* extra spawnbaseline flag bits.
const (
	BFitzLargeModel = 1 << 0
	BFitzLargeFrame = 1 << 1
	BFitzAlpha      = 1 << 2
)

// ENTALPHA_* alpha-byte encoding (FITZ extension). 0 means "default
// opaque OR engine-decided water alpha"; 1..255 maps to [0.0, 1.0].
const (
	EntAlphaDefault = 0
	EntAlphaZero    = 1
	EntAlphaOne     = 255
)

// EntAlphaEncode converts a float [0.0, 1.0] into the byte
// representation. 0 maps to EntAlphaDefault; any other input is
// clamped + rounded. tyrquake: ENTALPHA_ENCODE.
func EntAlphaEncode(a float32) byte {
	if a == 0 {
		return EntAlphaDefault
	}
	v := math.Round(float64(a)*254 + 1)
	if v < 1 {
		v = 1
	}
	if v > 255 {
		v = 255
	}
	return byte(v)
}

// EntAlphaDecode is the inverse. EntAlphaDefault yields 1.0 (opaque).
// tyrquake: ENTALPHA_DECODE.
func EntAlphaDecode(a byte) float32 {
	if a == EntAlphaDefault {
		return 1.0
	}
	return (float32(a) - 1.0) / 254.0
}

// EntAlphaToSave converts the byte back to the savegame-side float
// (Default -> 0, Zero -> -1, otherwise [0,1]). tyrquake:
// ENTALPHA_TOSAVE.
func EntAlphaToSave(a byte) float32 {
	switch a {
	case EntAlphaDefault:
		return 0
	case EntAlphaZero:
		return -1
	}
	return (float32(a) - 1.0) / 254.0
}

// Player + game defaults.
const (
	DefaultViewHeight = 22
	MaxClients        = 16
)

// Server-info game-type bytes.
const (
	GameCoop       = 0
	GameDeathMatch = 1
)

// Server-to-client message-type tags. tyrquake: svc_*.
const (
	SvcBad             = 0
	SvcNop             = 1
	SvcDisconnect      = 2
	SvcUpdateStat      = 3
	SvcVersion         = 4
	SvcSetView         = 5
	SvcSound           = 6
	SvcTime            = 7
	SvcPrint           = 8
	SvcStuffText       = 9
	SvcSetAngle        = 10
	SvcServerInfo      = 11
	SvcLightStyle      = 12
	SvcUpdateName      = 13
	SvcUpdateFrags     = 14
	SvcClientData      = 15
	SvcStopSound       = 16
	SvcUpdateColors    = 17
	SvcParticle        = 18
	SvcDamage          = 19
	SvcSpawnStatic     = 20
	// 21 (svc_spawnbinary) is unused.
	SvcSpawnBaseline   = 22
	SvcTempEntity      = 23
	SvcSetPause        = 24
	SvcSignonNum       = 25
	SvcCenterPrint     = 26
	SvcKilledMonster   = 27
	SvcFoundSecret     = 28
	SvcSpawnStaticSound = 29
	SvcIntermission    = 30
	SvcFinale          = 31
	SvcCDTrack         = 32
	SvcSellScreen      = 33
	SvcCutscene        = 34

	// FITZ extensions.
	SvcFitzSkybox            = 37
	SvcFitzBf                = 40
	SvcFitzFog               = 41
	SvcFitzSpawnBaseline2    = 42
	SvcFitzSpawnStatic2      = 43
	SvcFitzSpawnStaticSound2 = 44
)

// Client-to-server message-type tags. tyrquake: clc_*.
const (
	ClcBad        = 0
	ClcNop        = 1
	ClcDisconnect = 2
	ClcMove       = 3
	ClcStringCmd  = 4
)

// Temp-entity (TE_*) event codes. tyrquake: TE_*.
const (
	TESpike        = 0
	TESuperSpike   = 1
	TEGunshot      = 2
	TEExplosion    = 3
	TETarExplosion = 4
	TELightning1   = 5
	TELightning2   = 6
	TEWizSpike     = 7
	TEKnightSpike  = 8
	TELightning3   = 9
	TELavaSplash   = 10
	TETeleport     = 11
	TEExplosion2   = 12
	TEBeam         = 13
)
