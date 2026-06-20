// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// Minimal update: just the U_SIGNAL byte (bits == 0) + the entity
// byte. Nothing else on the wire.
func TestEncodeUpdate_EmptyBits(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeUpdate(buf, 7, EntityUpdate{}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 2 {
		t.Fatalf("wire size: got %d want 2", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	if got := r.ReadU8(); got != protocol.USignal {
		t.Errorf("bits byte: got 0x%02x want 0x%02x (U_SIGNAL)", got, protocol.USignal)
	}
	if got := r.ReadU8(); got != 7 {
		t.Errorf("entity byte: got %d want 7", got)
	}
}

// Single-axis origin delta: bits = U_ORIGIN1, byte + entity + coord.
func TestEncodeUpdate_OriginX(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UOrigin1, Origin: [3]float32{32, 0, 0}}
	if err := EncodeUpdate(buf, 1, u); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1+1+2 {
		t.Fatalf("wire size: got %d want 4", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	if got := r.ReadU8(); got != protocol.UOrigin1|protocol.USignal {
		t.Errorf("bits byte: got 0x%02x want 0x%02x", got, protocol.UOrigin1|protocol.USignal)
	}
	if got := r.ReadU8(); got != 1 {
		t.Errorf("entity byte: got %d want 1", got)
	}
	if got := r.ReadCoord(); got != 32 {
		t.Errorf("origin[0]: got %v want 32", got)
	}
}

// All three origin axes set together.
func TestEncodeUpdate_AllOrigins(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{
		Bits:   protocol.UOrigin1 | protocol.UOrigin2 | protocol.UOrigin3,
		Origin: [3]float32{8, 16, 24},
	}
	if err := EncodeUpdate(buf, 2, u); err != nil {
		t.Fatal(err)
	}
	// 1 bits + 1 entity + 3*2 coord = 8
	if buf.Len() != 8 {
		t.Fatalf("wire size: got %d want 8", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // bits
	_ = r.ReadU8() // entity
	for axis, want := range [3]float32{8, 16, 24} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("origin[%d]: got %v want %v", axis, got, want)
		}
	}
}

// All three angle axes set together. Uses angles inside [-180, 180)
// because ReadAngle treats the byte as signed (msg.ReadChar) and
// would wrap 180/270 -> -180/-90 on the read side -- a quirk of
// the wire format, not of the encoder.
func TestEncodeUpdate_AllAngles(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	// U_ANGLE1 lives in the high byte -> U_MOREBITS too.
	u := EntityUpdate{
		Bits:   protocol.UAngle1 | protocol.UAngle2 | protocol.UAngle3 | protocol.UMoreBits,
		Angles: [3]float32{45, 90, -45},
	}
	if err := EncodeUpdate(buf, 3, u); err != nil {
		t.Fatal(err)
	}
	// 1 lo-bits + 1 hi-bits + 1 entity + 3*1 angle = 6
	if buf.Len() != 6 {
		t.Fatalf("wire size: got %d want 6", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // lo bits
	_ = r.ReadU8() // hi bits
	_ = r.ReadU8() // entity
	for axis, want := range [3]float32{45, 90, -45} {
		got := r.ReadAngle()
		// 1/256-circle quantisation introduces rounding.
		diff := got - want
		if diff < -2 || diff > 2 {
			t.Errorf("angle[%d]: got %v want ~%v (1/256-circle)", axis, got, want)
		}
	}
}

// U_LONGENTITY -> entity goes out as a short.
func TestEncodeUpdate_LongEntity(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	// U_LONGENTITY is in the high byte -> U_MOREBITS too.
	u := EntityUpdate{Bits: protocol.ULongEntity | protocol.UMoreBits}
	if err := EncodeUpdate(buf, 300, u); err != nil {
		t.Fatal(err)
	}
	// 1 lo-bits + 1 hi-bits + 2 entity = 4
	if buf.Len() != 4 {
		t.Fatalf("wire size: got %d want 4", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()
	_ = r.ReadU8()
	if got := r.ReadShort(); got != 300 {
		t.Errorf("entity short: got %d want 300", got)
	}
}

// U_MOREBITS forces a second bits byte carrying bits>>8.
func TestEncodeUpdate_MoreBits(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	// Any high-byte bit pulls in U_MOREBITS. Use U_MODEL (1<<10) here.
	u := EntityUpdate{
		Bits:  protocol.UMoreBits | protocol.UModel,
		Model: 0x42,
	}
	if err := EncodeUpdate(buf, 5, u); err != nil {
		t.Fatal(err)
	}
	// 1 lo-bits + 1 hi-bits + 1 entity + 1 model = 4
	if buf.Len() != 4 {
		t.Fatalf("wire size: got %d want 4", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	lo := r.ReadU8()
	hi := r.ReadU8()
	combined := lo | (hi << 8)
	wantBits := (protocol.UMoreBits | protocol.UModel) | protocol.USignal
	if combined != wantBits {
		t.Errorf("combined bits: got 0x%04x want 0x%04x", combined, wantBits)
	}
	_ = r.ReadU8() // entity
	if got := r.ReadU8(); got != 0x42 {
		t.Errorf("model: got 0x%02x want 0x42", got)
	}
}

// Model byte.
func TestEncodeUpdate_Model(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UMoreBits | protocol.UModel, Model: 17}
	if err := EncodeUpdate(buf, 4, u); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // lo
	_ = r.ReadU8() // hi
	_ = r.ReadU8() // entity
	if got := r.ReadU8(); got != 17 {
		t.Errorf("model: got %d want 17", got)
	}
}

// Frame byte.
func TestEncodeUpdate_Frame(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UFrame, Frame: 9}
	if err := EncodeUpdate(buf, 4, u); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	if got := r.ReadU8(); got != protocol.UFrame|protocol.USignal {
		t.Errorf("bits: got 0x%02x want 0x%02x", got, protocol.UFrame|protocol.USignal)
	}
	_ = r.ReadU8() // entity
	if got := r.ReadU8(); got != 9 {
		t.Errorf("frame: got %d want 9", got)
	}
}

// Skin byte (high-byte gated -> needs U_MOREBITS).
func TestEncodeUpdate_Skin(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UMoreBits | protocol.USkin, Skin: 3}
	if err := EncodeUpdate(buf, 4, u); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // lo
	_ = r.ReadU8() // hi
	_ = r.ReadU8() // entity
	if got := r.ReadU8(); got != 3 {
		t.Errorf("skin: got %d want 3", got)
	}
}

// Effects byte (high-byte gated).
func TestEncodeUpdate_Effects(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UMoreBits | protocol.UEffects, Effects: 0xAB}
	if err := EncodeUpdate(buf, 4, u); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()
	_ = r.ReadU8()
	_ = r.ReadU8()
	if got := r.ReadU8(); got != 0xAB {
		t.Errorf("effects: got 0x%02x want 0xAB", got)
	}
}

// ColorMap byte (high-byte gated).
func TestEncodeUpdate_ColorMap(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UMoreBits | protocol.UColorMap, ColorMap: 2}
	if err := EncodeUpdate(buf, 4, u); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()
	_ = r.ReadU8()
	_ = r.ReadU8()
	if got := r.ReadU8(); got != 2 {
		t.Errorf("colormap: got %d want 2", got)
	}
}

// U_NOLERP is a flag-only bit -- it just rides along in the bits
// byte without contributing any extra wire data.
func TestEncodeUpdate_NoLerp(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	u := EntityUpdate{Bits: protocol.UNoLerp}
	if err := EncodeUpdate(buf, 4, u); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 2 {
		t.Errorf("wire size: got %d want 2", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	if got := r.ReadU8(); got != protocol.UNoLerp|protocol.USignal {
		t.Errorf("bits: got 0x%02x want 0x%02x", got, protocol.UNoLerp|protocol.USignal)
	}
}

// Combined: a realistic per-tick delta touching origin, angle,
// frame, model, skin, effects, colormap, and the more-bits + long
// entity flags.
func TestEncodeUpdate_Combined(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bits := protocol.UOrigin1 | protocol.UOrigin2 | protocol.UOrigin3 |
		protocol.UAngle1 | protocol.UAngle2 | protocol.UAngle3 |
		protocol.UFrame | protocol.UModel | protocol.UColorMap |
		protocol.USkin | protocol.UEffects |
		protocol.ULongEntity | protocol.UMoreBits | protocol.UNoLerp
	u := EntityUpdate{
		Bits:     bits,
		Origin:   [3]float32{1, 2, 3},
		Angles:   [3]float32{0, 0, 0},
		Frame:    0xAA,
		Model:    0xBB,
		ColorMap: 0xCC,
		Skin:     0xDD,
		Effects:  0xEE,
	}
	if err := EncodeUpdate(buf, 1000, u); err != nil {
		t.Fatal(err)
	}
	// 1 lo + 1 hi + 2 entity + 1 model + 1 frame + 1 colormap + 1 skin
	// + 1 effects + 3*2 coord + 3*1 angle = 18
	if buf.Len() != 18 {
		t.Fatalf("wire size: got %d want 18", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	lo := r.ReadU8()
	hi := r.ReadU8()
	if combined := lo | (hi << 8); combined != bits|protocol.USignal {
		t.Errorf("bits: got 0x%04x want 0x%04x", combined, bits|protocol.USignal)
	}
	if got := r.ReadShort(); got != 1000 {
		t.Errorf("entity: got %d want 1000", got)
	}
	if got := r.ReadU8(); got != 0xBB {
		t.Errorf("model: got 0x%02x want 0xBB", got)
	}
	if got := r.ReadU8(); got != 0xAA {
		t.Errorf("frame: got 0x%02x want 0xAA", got)
	}
	if got := r.ReadU8(); got != 0xCC {
		t.Errorf("colormap: got 0x%02x want 0xCC", got)
	}
	if got := r.ReadU8(); got != 0xDD {
		t.Errorf("skin: got 0x%02x want 0xDD", got)
	}
	if got := r.ReadU8(); got != 0xEE {
		t.Errorf("effects: got 0x%02x want 0xEE", got)
	}
	// Interleaved: origin[0], angle[0], origin[1], angle[1], origin[2], angle[2]
	if got := r.ReadCoord(); got != 1 {
		t.Errorf("origin[0]: got %v want 1", got)
	}
	if got := r.ReadAngle(); got != 0 {
		t.Errorf("angle[0]: got %v want 0", got)
	}
	if got := r.ReadCoord(); got != 2 {
		t.Errorf("origin[1]: got %v want 2", got)
	}
	if got := r.ReadAngle(); got != 0 {
		t.Errorf("angle[1]: got %v want 0", got)
	}
	if got := r.ReadCoord(); got != 3 {
		t.Errorf("origin[2]: got %v want 3", got)
	}
	if got := r.ReadAngle(); got != 0 {
		t.Errorf("angle[2]: got %v want 0", got)
	}
}

// Nil sizebuf -> ErrNilBuf.
func TestEncodeUpdate_NilBufErrors(t *testing.T) {
	if err := EncodeUpdate(nil, 0, EntityUpdate{}); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// Entity index out of range -> ErrEntityNumRange. Both ends.
func TestEncodeUpdate_EntityNumRange(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeUpdate(buf, -1, EntityUpdate{}); !errors.Is(err, ErrEntityNumRange) {
		t.Errorf("negative: got %v want ErrEntityNumRange", err)
	}
	if err := EncodeUpdate(buf, 0x10000, EntityUpdate{}); !errors.Is(err, ErrEntityNumRange) {
		t.Errorf("too large: got %v want ErrEntityNumRange", err)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer modified on range error: len=%d", buf.Len())
	}
}

// Per-write overflow propagation: walk a capacity ladder so each
// msg.Write* site inside EncodeUpdate gets exercised on its
// error-return branch. The configuration sets every U_* bit we
// support so every write site is reachable.
//
// Wire shape for this configuration (18 bytes):
//
//	 0: lo-bits byte
//	 1: hi-bits byte                  (U_MOREBITS)
//	 2..3: entity short               (U_LONGENTITY)
//	 4: model byte                    (U_MODEL)
//	 5: frame byte                    (U_FRAME)
//	 6: colormap byte                 (U_COLORMAP)
//	 7: skin byte                     (U_SKIN)
//	 8: effects byte                  (U_EFFECTS)
//	 9..10: origin[0] coord           (U_ORIGIN1)
//	11: angle[0] byte                 (U_ANGLE1)
//	12..13: origin[1] coord           (U_ORIGIN2)
//	14: angle[1] byte                 (U_ANGLE2)
//	15..16: origin[2] coord           (U_ORIGIN3)
//	17: angle[2] byte                 (U_ANGLE3)
//
// A capacity of N succeeds the first N bytes and fails the (N+1)th
// write. We probe every site by feeding a slice that's exactly one
// byte short of the cumulative offset above.
func TestEncodeUpdate_PerWriteOverflowPropagates(t *testing.T) {
	bits := protocol.UMoreBits | protocol.ULongEntity |
		protocol.UModel | protocol.UFrame | protocol.UColorMap |
		protocol.USkin | protocol.UEffects |
		protocol.UOrigin1 | protocol.UAngle1 |
		protocol.UOrigin2 | protocol.UAngle2 |
		protocol.UOrigin3 | protocol.UAngle3
	u := EntityUpdate{Bits: bits}

	// Capacities chosen so each Write* error branch is the FIRST to
	// fire: byte-writes need cap = prevOffset, coord/short writes
	// (2 bytes) need cap = prevOffset (still fails because GetSpace(2)
	// > 1 free byte).
	caps := []int{
		0,  // fails on lo-bits byte
		1,  // fails on hi-bits byte
		2,  // fails on entity short (needs 2 bytes; 0 free for short)
		3,  // fails on entity short (1 free, needs 2)
		4,  // fails on model byte
		5,  // fails on frame byte
		6,  // fails on colormap byte
		7,  // fails on skin byte
		8,  // fails on effects byte
		9,  // fails on origin[0] coord (needs 2, 0 free)
		10, // fails on origin[0] coord (needs 2, 1 free)
		11, // fails on angle[0] byte
		12, // fails on origin[1] coord (needs 2, 0 free)
		13, // fails on origin[1] coord (needs 2, 1 free)
		14, // fails on angle[1] byte
		15, // fails on origin[2] coord (needs 2, 0 free)
		16, // fails on origin[2] coord (needs 2, 1 free)
		17, // fails on angle[2] byte
	}
	for _, cap := range caps {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, cap))
			err := EncodeUpdate(buf, 1000, u)
			if err == nil {
				t.Errorf("cap=%d: expected overflow error, got nil", cap)
			}
		})
	}
	// Sanity: cap 18 succeeds clean.
	buf := sizebuf.New(make([]byte, 18))
	if err := EncodeUpdate(buf, 1000, u); err != nil {
		t.Errorf("cap=18: expected success, got %v", err)
	}
}

// Short-entity byte-write overflow: the U_LONGENTITY-cleared branch
// of the entity write needs its own probe because the cap-ladder
// above always sets U_LONGENTITY (and thus writes a short, not a
// byte) for the entity slot.
func TestEncodeUpdate_ShortEntityOverflow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 1)) // 1 byte: bits fits, entity byte fails.
	if err := EncodeUpdate(buf, 1, EntityUpdate{}); err == nil {
		t.Errorf("expected overflow on entity byte write, got nil")
	}
}
