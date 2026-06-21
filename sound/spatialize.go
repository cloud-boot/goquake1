// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sound

import "math"

// SoundAttenuation is the per-sound distance-falloff coefficient.
// Larger = sound fades faster with distance. tyrquake: the
// `sound->attenuation` field on each fired sound (typically 1.0 for
// gameplay sounds, 0.0 for ambient looped tracks, 2.0 for footsteps).
type SoundAttenuation float32

const (
	AttenuationNone   SoundAttenuation = 0   // no distance falloff (UI / global)
	AttenuationNormal SoundAttenuation = 1   // standard gameplay
	AttenuationIdle   SoundAttenuation = 2   // monster idle sounds (short range)
	AttenuationStatic SoundAttenuation = 3   // env_sound entities (very short range)
)

// SoundFalloffDist is the distance (in world units) at which an
// AttenuationNormal sound's volume reaches zero. tyrquake hardcodes
// this as the 1.0 / (dist * 0.001 * attenuation) formula's saturation
// point; the Go port exposes the constant explicitly.
const SoundFalloffDist = 1000

// MaxVolume is the per-channel output ceiling. tyrquake uses 255 as
// the 8-bit volume scale (then mixes against an int16 accumulator).
const MaxVolume = 255

// SpatializeIn is the per-sound bundle passed to Spatialize.
type SpatializeIn struct {
	ListenerOrigin  [3]float32     // viewer position
	ListenerRight   [3]float32     // viewer right axis (for L/R stereo split)
	SoundOrigin     [3]float32     // sound's world position
	MasterVolume    int            // 0..MaxVolume, the sound's base volume
	Attenuation     SoundAttenuation
}

// SpatializeOut is the per-sound result the mixer consumes.
type SpatializeOut struct {
	LeftVol  int  // 0..MaxVolume (clamped)
	RightVol int  // 0..MaxVolume (clamped)
}

// Spatialize computes the per-ear volumes for one sound at the
// listener position. tyrquake: SND_Spatialize in snd_dma.c.
//
// Algorithm:
//
//  1. Compute the vector from listener to source.
//  2. Compute the source distance.
//  3. Compute a stereo balance scalar via (relativeVec . listenerRight) / dist.
//     +1 = fully right ear; -1 = fully left ear; 0 = centered.
//  4. Compute a master scale via 1 - dist * 0.001 * attenuation; clamp
//     negative -> 0; clamp >1 -> 1.
//  5. LeftVol  = masterVolume * masterScale * (1 - balance) / 2
//     RightVol = masterVolume * masterScale * (1 + balance) / 2
//
// SOURCE-AT-LISTENER edge case (dist == 0): no spatial separation
// (balance = 0 -> both ears at half volume per the formula). This
// matches the upstream behavior on the listener's own foot-step
// sounds.
//
// AttenuationNone short-circuit: master scale is always 1 regardless
// of distance; balance still computed for stereo positioning.
func Spatialize(in SpatializeIn) SpatializeOut {
	dx := in.SoundOrigin[0] - in.ListenerOrigin[0]
	dy := in.SoundOrigin[1] - in.ListenerOrigin[1]
	dz := in.SoundOrigin[2] - in.ListenerOrigin[2]
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))

	// Master scale: 1 at listener, 0 at SoundFalloffDist (modulated
	// by attenuation factor).
	master := float32(1.0)
	if in.Attenuation != AttenuationNone {
		master = 1 - dist*0.001*float32(in.Attenuation)
		if master < 0 {
			master = 0
		} else if master > 1 {
			master = 1
		}
	}

	// Stereo balance: project relative position onto right axis.
	// dist == 0 -> balance == 0 (no separation).
	balance := float32(0)
	if dist > 0 {
		balance = (dx*in.ListenerRight[0] + dy*in.ListenerRight[1] + dz*in.ListenerRight[2]) / dist
	}

	scaledBase := float32(in.MasterVolume) * master
	leftScale := (1 - balance) * 0.5
	rightScale := (1 + balance) * 0.5

	left := int(scaledBase * leftScale)
	right := int(scaledBase * rightScale)

	if left < 0 {
		left = 0
	} else if left > MaxVolume {
		left = MaxVolume
	}
	if right < 0 {
		right = 0
	} else if right > MaxVolume {
		right = MaxVolume
	}
	return SpatializeOut{LeftVol: left, RightVol: right}
}
