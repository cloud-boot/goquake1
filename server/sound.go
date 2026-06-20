// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"fmt"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// soundReserve is the byte budget EncodeSound reserves at the END of
// the datagram before emitting. tyrquake uses MAX_DATAGRAM - 14 as
// its bail threshold; the maximum svc_sound message is 15 bytes
// (1 cmd + 1 mask + 1 vol + 1 attn + 3 ent-FITZ + 2 sound-FITZ +
// 6 origin), so the 14-byte reserve matches the upstream margin.
const soundReserve = 14

// ErrSoundVolumeRange / ErrSoundAttenRange / ErrSoundChannelRange /
// ErrSoundProtoUnencodable surface the input-validation paths the
// C upstream Sys_Error / silent-return's on. Returning errors lets
// callers log and continue instead of crashing the server tick.
var (
	ErrSoundVolumeRange      = errors.New("server: sound volume outside [0, 255]")
	ErrSoundAttenRange       = errors.New("server: sound attenuation outside [0, 4]")
	ErrSoundChannelRange     = errors.New("server: sound channel outside [0, 7] (FITZ-only otherwise)")
	ErrSoundProtoUnencodable = errors.New("server: entIdx >= 8192 or soundNum >= 256 only encodable on FITZ protocol")
	ErrSoundProtoUnknown     = errors.New("server: unknown protocol version")
)

// writeSoundNum emits the sound index per the protocol's encoding
// rule. tyrquake: SV_WriteSoundNum.
//
//	NQ, BJP        -> 1 byte
//	BJP2, BJP3     -> 2 bytes (always short)
//	FITZ           -> 2 bytes iff fieldMask has SndFitzLargeSound, else 1 byte
func writeSoundNum(buf *sizebuf.Buffer, soundNum int, fieldMask int, proto int) error {
	switch proto {
	case protocol.VersionNQ, protocol.VersionBJP:
		return msg.WriteByte(buf, soundNum)
	case protocol.VersionBJP2, protocol.VersionBJP3:
		return msg.WriteShort(buf, soundNum)
	case protocol.VersionFitz:
		if fieldMask&protocol.SndFitzLargeSound != 0 {
			return msg.WriteShort(buf, soundNum)
		}
		return msg.WriteByte(buf, soundNum)
	default:
		return fmt.Errorf("%w: %d", ErrSoundProtoUnknown, proto)
	}
}

// EncodeSound writes one svc_sound message into buf. tyrquake:
// SV_StartSound's message-writing tail. The C upstream couples
// sound-precache lookup, edict-pointer translation, and the wire
// emit into one function; the Go port splits the wire emit out so
// the server glue layer can do the precache lookup + entity-center
// computation separately.
//
// Parameters:
//
//	entIdx       -- the entity slot the sound originates from
//	channel      -- 0..7 (8+ requires FITZ protocol)
//	soundNum     -- precache slot index (>=1; 0 is reserved/none)
//	origin       -- world-space sound source center; the caller
//	                is responsible for entity.origin + entity-center
//	                computation (origin + 0.5*(mins+maxs))
//	volume       -- 0..255; protocol.DefaultSoundVolume (=255) is
//	                omitted from the wire (no SndVolume bit set)
//	attenuation  -- 0..4 in 1/64 steps; protocol.DefaultSoundAttenuation
//	                (=1.0) is omitted from the wire
//	proto        -- one of protocol.Version{NQ,Fitz,BJP,BJP2,BJP3}
//
// Returns:
//
//	ErrDatagramFull            if buf is within soundReserve bytes of capacity
//	ErrSoundVolumeRange/Atten  on out-of-range parameter
//	ErrSoundChannelRange       on channel < 0 (>=8 is allowed on FITZ)
//	ErrSoundProtoUnencodable   if entIdx >= 8192 or soundNum >= 256 on a
//	                            non-FITZ protocol (those need the FITZ
//	                            field-mask extensions to encode)
//	ErrSoundProtoUnknown       on unrecognised proto value
//	(propagated)               write-time sizebuf overflow
func EncodeSound(buf *sizebuf.Buffer, entIdx, channel, soundNum int, origin [3]float32, volume int, attenuation float32, proto int) error {
	if buf == nil {
		return errors.New("server: nil sizebuf")
	}
	if volume < 0 || volume > 255 {
		return fmt.Errorf("%w: %d", ErrSoundVolumeRange, volume)
	}
	if attenuation < 0 || attenuation > 4 {
		return fmt.Errorf("%w: %v", ErrSoundAttenRange, attenuation)
	}
	if channel < 0 {
		return fmt.Errorf("%w: %d", ErrSoundChannelRange, channel)
	}

	if buf.Len() > MaxDatagram-soundReserve {
		return ErrDatagramFull
	}

	fieldMask := 0
	if volume != protocol.DefaultSoundVolume {
		fieldMask |= protocol.SndVolume
	}
	if attenuation != protocol.DefaultSoundAttenuation {
		fieldMask |= protocol.SndAttenuation
	}
	largeEntity := entIdx >= 8192
	largeSound := soundNum >= 256 || channel >= 8
	if largeEntity {
		if proto != protocol.VersionFitz {
			return fmt.Errorf("%w: entIdx=%d", ErrSoundProtoUnencodable, entIdx)
		}
		fieldMask |= protocol.SndFitzLargeEntity
	}
	if largeSound {
		if proto != protocol.VersionFitz {
			return fmt.Errorf("%w: soundNum=%d channel=%d", ErrSoundProtoUnencodable, soundNum, channel)
		}
		fieldMask |= protocol.SndFitzLargeSound
	}

	if err := msg.WriteByte(buf, protocol.SvcSound); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, fieldMask); err != nil {
		return err
	}
	if fieldMask&protocol.SndVolume != 0 {
		if err := msg.WriteByte(buf, volume); err != nil {
			return err
		}
	}
	if fieldMask&protocol.SndAttenuation != 0 {
		if err := msg.WriteByte(buf, int(attenuation*64)); err != nil {
			return err
		}
	}
	if largeEntity {
		if err := msg.WriteShort(buf, entIdx); err != nil {
			return err
		}
		if err := msg.WriteByte(buf, channel); err != nil {
			return err
		}
	} else {
		if err := msg.WriteShort(buf, (entIdx<<3)|channel); err != nil {
			return err
		}
	}
	if err := writeSoundNum(buf, soundNum, fieldMask, proto); err != nil {
		return err
	}
	for axis := 0; axis < 3; axis++ {
		if err := msg.WriteCoord(buf, origin[axis]); err != nil {
			return err
		}
	}
	return nil
}
