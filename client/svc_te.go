// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"
	"fmt"

	"github.com/go-quake1/engine/protocol"
)

// TempEntityKind enumerates the sub-types of svc_temp_entity. The
// values are kept in lockstep with the TE_* wire constants in the
// protocol package (a drift detector test pins both lists). tyrquake:
// cl_tent.c (CL_ParseTEnt) + protocol.h (TE_* defines).
type TempEntityKind int

// The svc_temp_entity sub-type byte. Values match the TE_* wire codes
// the C upstream emits. The three "lightning" kinds share one body
// shape (entity short + start coord triple + end coord triple); the
// nine "point effect" kinds share another (origin coord triple);
// TEExplosion2 extends the point-effect body with two color bytes;
// TEBeam is wire-identical to the lightning kinds (PGM 01/21/97
// grappling-hook beam in tyrquake -- cl_tent.c case TE_BEAM).
const (
	TESpike        TempEntityKind = protocol.TESpike        // 0
	TESuperSpike   TempEntityKind = protocol.TESuperSpike   // 1
	TEGunshot      TempEntityKind = protocol.TEGunshot      // 2
	TEExplosion    TempEntityKind = protocol.TEExplosion    // 3
	TETarExplosion TempEntityKind = protocol.TETarExplosion // 4
	TELightning1   TempEntityKind = protocol.TELightning1   // 5
	TELightning2   TempEntityKind = protocol.TELightning2   // 6
	TEWizSpike     TempEntityKind = protocol.TEWizSpike     // 7
	TEKnightSpike  TempEntityKind = protocol.TEKnightSpike  // 8
	TELightning3   TempEntityKind = protocol.TELightning3   // 9
	TELavaSplash   TempEntityKind = protocol.TELavaSplash   // 10
	TETeleport     TempEntityKind = protocol.TETeleport     // 11
	TEExplosion2   TempEntityKind = protocol.TEExplosion2   // 12
	TEBeam         TempEntityKind = protocol.TEBeam         // 13
)

// DecodedTempEntity is the umbrella result from svc_temp_entity. The
// Kind selector tells callers which sub-fields are populated:
//
//   - point-effect kinds (TESpike, TESuperSpike, TEGunshot,
//     TEExplosion, TETarExplosion, TEWizSpike, TEKnightSpike,
//     TELavaSplash, TETeleport): Origin only.
//   - TEExplosion2: Origin + ColorStart + ColorLength.
//   - lightning kinds (TELightning1, TELightning2, TELightning3,
//     TEBeam): EntityNum + Start + End.
//
// Fields not relevant to the active Kind are left at their zero
// value. tyrquake: cl_tent.c CL_ParseTEnt.
type DecodedTempEntity struct {
	Kind        TempEntityKind
	Origin      [3]float32 // point-effect kinds
	Start       [3]float32 // lightning beam start
	End         [3]float32 // lightning beam end
	EntityNum   int        // owning entity for the lightning beam
	ColorStart  int        // TEExplosion2 only
	ColorLength int        // TEExplosion2 only
}

func (DecodedTempEntity) isDecoded() {}

// ErrTEUnknownKind is returned by [SvcReader] when the temp-entity
// sub-type byte does not dispatch to any TE_* kind this build knows.
// The C upstream Sys_Error's instead, which kills the host; the Go
// port surfaces a recoverable error so callers can log + drop the
// frame.
var ErrTEUnknownKind = errors.New("client: unknown TE_* kind")

// decodeTempEntity reads the svc_temp_entity body (the leading cmd
// byte is already consumed by [SvcReader.Next]). The body starts with
// a single TE_* sub-type byte and is followed by a per-kind payload;
// see DecodedTempEntity for the field shapes.
//
// Wire shapes (mirror cl_tent.c CL_ParseTEnt):
//
//	point-effect kinds:
//	  coord coord coord                    (origin)
//
//	TEExplosion2:
//	  coord coord coord                    (origin)
//	  byte                                 (colorstart)
//	  byte                                 (colorlength)
//
//	lightning kinds (incl. TEBeam):
//	  short                                (owning entity)
//	  coord coord coord                    (start)
//	  coord coord coord                    (end)
func (sr *SvcReader) decodeTempEntity() (Decoded, error) {
	kindByte := sr.R.ReadU8()
	if sr.R.Bad() {
		return nil, ErrCorruptMessage
	}
	kind := TempEntityKind(kindByte)

	switch kind {
	case TESpike, TESuperSpike, TEGunshot, TEExplosion, TETarExplosion,
		TEWizSpike, TEKnightSpike, TELavaSplash, TETeleport:
		return sr.decodeTEPointEffect(kind)
	case TEExplosion2:
		return sr.decodeTEExplosion2()
	case TELightning1, TELightning2, TELightning3, TEBeam:
		return sr.decodeTELightning(kind)
	}
	return nil, fmt.Errorf("%w: kind=%d", ErrTEUnknownKind, kindByte)
}

// decodeTEPointEffect reads the three-coord origin payload shared by
// the spike / gunshot / explosion / tarbaby / wizspike / knightspike /
// lavasplash / teleport sub-types.
func (sr *SvcReader) decodeTEPointEffect(kind TempEntityKind) (Decoded, error) {
	var origin [3]float32
	for i := 0; i < 3; i++ {
		origin[i] = sr.R.ReadCoord()
	}
	if sr.R.Bad() {
		return nil, ErrCorruptMessage
	}
	return DecodedTempEntity{Kind: kind, Origin: origin}, nil
}

// decodeTEExplosion2 reads the color-mapped explosion body: origin
// coord triple + colorstart byte + colorlength byte.
func (sr *SvcReader) decodeTEExplosion2() (Decoded, error) {
	var origin [3]float32
	for i := 0; i < 3; i++ {
		origin[i] = sr.R.ReadCoord()
	}
	colorStart := sr.R.ReadU8()
	colorLength := sr.R.ReadU8()
	if sr.R.Bad() {
		return nil, ErrCorruptMessage
	}
	return DecodedTempEntity{
		Kind:        TEExplosion2,
		Origin:      origin,
		ColorStart:  colorStart,
		ColorLength: colorLength,
	}, nil
}

// decodeTELightning reads the shared lightning/beam body: an entity
// short followed by start + end coord triples. tyrquake: cl_tent.c
// CL_ParseBeam.
func (sr *SvcReader) decodeTELightning(kind TempEntityKind) (Decoded, error) {
	entNum := int(uint16(int16(sr.R.ReadShort())))
	var start, end [3]float32
	for i := 0; i < 3; i++ {
		start[i] = sr.R.ReadCoord()
	}
	for i := 0; i < 3; i++ {
		end[i] = sr.R.ReadCoord()
	}
	if sr.R.Bad() {
		return nil, ErrCorruptMessage
	}
	return DecodedTempEntity{
		Kind:      kind,
		EntityNum: entNum,
		Start:     start,
		End:       end,
	}, nil
}
