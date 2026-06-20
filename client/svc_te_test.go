// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// teEncodePoint emits a svc_temp_entity message with the point-effect
// shape (cmd byte + kind byte + 3 coord shorts).
func teEncodePoint(t *testing.T, kind TempEntityKind, origin [3]float32) []byte {
	t.Helper()
	buf := sizebuf.New(make([]byte, 32))
	if err := msg.WriteByte(buf, protocol.SvcTempEntity); err != nil {
		t.Fatal(err)
	}
	if err := msg.WriteByte(buf, int(kind)); err != nil {
		t.Fatal(err)
	}
	for _, c := range origin {
		if err := msg.WriteCoord(buf, c); err != nil {
			t.Fatal(err)
		}
	}
	return append([]byte(nil), buf.Bytes()...)
}

// teEncodeExplosion2 emits a TEExplosion2 message (point body + 2
// color bytes).
func teEncodeExplosion2(t *testing.T, origin [3]float32, colorStart, colorLength int) []byte {
	t.Helper()
	buf := sizebuf.New(make([]byte, 32))
	if err := msg.WriteByte(buf, protocol.SvcTempEntity); err != nil {
		t.Fatal(err)
	}
	if err := msg.WriteByte(buf, int(TEExplosion2)); err != nil {
		t.Fatal(err)
	}
	for _, c := range origin {
		if err := msg.WriteCoord(buf, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := msg.WriteByte(buf, colorStart); err != nil {
		t.Fatal(err)
	}
	if err := msg.WriteByte(buf, colorLength); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), buf.Bytes()...)
}

// teEncodeLightning emits a lightning/beam message (cmd + kind + ent
// short + start triple + end triple).
func teEncodeLightning(t *testing.T, kind TempEntityKind, ent int, start, end [3]float32) []byte {
	t.Helper()
	buf := sizebuf.New(make([]byte, 32))
	if err := msg.WriteByte(buf, protocol.SvcTempEntity); err != nil {
		t.Fatal(err)
	}
	if err := msg.WriteByte(buf, int(kind)); err != nil {
		t.Fatal(err)
	}
	if err := msg.WriteShort(buf, ent); err != nil {
		t.Fatal(err)
	}
	for _, c := range start {
		if err := msg.WriteCoord(buf, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range end {
		if err := msg.WriteCoord(buf, c); err != nil {
			t.Fatal(err)
		}
	}
	return append([]byte(nil), buf.Bytes()...)
}

// ------- point-effect roundtrips --------------------------------

func TestNext_TE_PointEffect_Roundtrip(t *testing.T) {
	origin := [3]float32{12.5, -8.0, 64.125}
	cases := []TempEntityKind{
		TESpike, TESuperSpike, TEGunshot, TEExplosion, TETarExplosion,
		TEWizSpike, TEKnightSpike, TELavaSplash, TETeleport,
	}
	for _, k := range cases {
		k := k
		t.Run(temptyName(k), func(t *testing.T) {
			data := teEncodePoint(t, k, origin)
			sr := newReader(data)
			v, err := sr.Next(protocol.VersionNQ)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := v.(DecodedTempEntity)
			if !ok {
				t.Fatalf("type: got %T want DecodedTempEntity", v)
			}
			if got.Kind != k {
				t.Errorf("Kind: got %d want %d", got.Kind, k)
			}
			if got.Origin != origin {
				t.Errorf("Origin: got %v want %v", got.Origin, origin)
			}
			if got.EntityNum != 0 || got.Start != ([3]float32{}) || got.End != ([3]float32{}) {
				t.Errorf("lightning fields leaked: %+v", got)
			}
			if got.ColorStart != 0 || got.ColorLength != 0 {
				t.Errorf("explosion2 fields leaked: %+v", got)
			}
		})
	}
}

// ------- TEExplosion2 roundtrip ---------------------------------

func TestNext_TE_Explosion2_Roundtrip(t *testing.T) {
	origin := [3]float32{1.5, 2.5, 3.5}
	const colorStart, colorLength = 73, 24
	data := teEncodeExplosion2(t, origin, colorStart, colorLength)
	sr := newReader(data)
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.(DecodedTempEntity)
	if !ok {
		t.Fatalf("type: got %T want DecodedTempEntity", v)
	}
	if got.Kind != TEExplosion2 {
		t.Errorf("Kind: got %d want %d", got.Kind, TEExplosion2)
	}
	if got.Origin != origin {
		t.Errorf("Origin: got %v want %v", got.Origin, origin)
	}
	if got.ColorStart != colorStart || got.ColorLength != colorLength {
		t.Errorf("color: got (%d,%d) want (%d,%d)",
			got.ColorStart, got.ColorLength, colorStart, colorLength)
	}
}

// ------- lightning/beam roundtrips ------------------------------

func TestNext_TE_Lightning_Roundtrip(t *testing.T) {
	start := [3]float32{0.5, 1.5, 2.5}
	end := [3]float32{100.0, -50.0, 32.0}
	const ent = 12345
	cases := []TempEntityKind{TELightning1, TELightning2, TELightning3, TEBeam}
	for _, k := range cases {
		k := k
		t.Run(temptyName(k), func(t *testing.T) {
			data := teEncodeLightning(t, k, ent, start, end)
			sr := newReader(data)
			v, err := sr.Next(protocol.VersionNQ)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := v.(DecodedTempEntity)
			if !ok {
				t.Fatalf("type: got %T want DecodedTempEntity", v)
			}
			if got.Kind != k {
				t.Errorf("Kind: got %d want %d", got.Kind, k)
			}
			if got.EntityNum != ent {
				t.Errorf("EntityNum: got %d want %d", got.EntityNum, ent)
			}
			if got.Start != start {
				t.Errorf("Start: got %v want %v", got.Start, start)
			}
			if got.End != end {
				t.Errorf("End: got %v want %v", got.End, end)
			}
			if got.Origin != ([3]float32{}) {
				t.Errorf("point-effect Origin leaked: %v", got.Origin)
			}
		})
	}
}

// Lightning ent number wide enough to exercise the unsigned-widening
// branch in decodeTELightning (raw short = -1 round-trips to 65535).
func TestNext_TE_Lightning_EntityNumWideShort(t *testing.T) {
	data := teEncodeLightning(t, TELightning1, 65535,
		[3]float32{}, [3]float32{})
	sr := newReader(data)
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedTempEntity)
	if got.EntityNum != 65535 {
		t.Errorf("EntityNum: got %d want 65535", got.EntityNum)
	}
}

// ------- ErrTEUnknownKind ---------------------------------------

func TestNext_TE_UnknownKind(t *testing.T) {
	// kind=14 is the first wire value past TEBeam (=13).
	for _, kindByte := range []byte{14, 0xFF} {
		sr := newReader([]byte{protocol.SvcTempEntity, kindByte})
		v, err := sr.Next(protocol.VersionNQ)
		if !errors.Is(err, ErrTEUnknownKind) {
			t.Errorf("kind=0x%02x: err: got %v want ErrTEUnknownKind", kindByte, err)
		}
		if v != nil {
			t.Errorf("kind=0x%02x: value: got %v want nil", kindByte, v)
		}
	}
}

// ------- short-read failure paths -------------------------------

// Cursor at end of buffer right after the cmd byte: the kind read
// itself trips Bad.
func TestNext_TE_ShortRead_NoKindByte(t *testing.T) {
	sr := newReader([]byte{protocol.SvcTempEntity})
	v, err := sr.Next(protocol.VersionNQ)
	if !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
	if v != nil {
		t.Errorf("value: got %v want nil", v)
	}
}

// Cursor runs out mid-origin for the point-effect arm.
func TestNext_TE_ShortRead_PointEffectMidOrigin(t *testing.T) {
	full := teEncodePoint(t, TESpike, [3]float32{1, 2, 3})
	// Truncate one byte off the end (last coord half-byte missing).
	sr := newReader(full[:len(full)-1])
	v, err := sr.Next(protocol.VersionNQ)
	if !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
	if v != nil {
		t.Errorf("value: got %v want nil", v)
	}
}

// Cursor runs out before colorLength byte for TEExplosion2.
func TestNext_TE_ShortRead_Explosion2MidColors(t *testing.T) {
	full := teEncodeExplosion2(t, [3]float32{1, 2, 3}, 73, 24)
	// Drop the trailing colorLength byte.
	sr := newReader(full[:len(full)-1])
	v, err := sr.Next(protocol.VersionNQ)
	if !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
	if v != nil {
		t.Errorf("value: got %v want nil", v)
	}
}

// Cursor runs out mid-end-coord-triple for a lightning kind.
func TestNext_TE_ShortRead_LightningMidEnd(t *testing.T) {
	full := teEncodeLightning(t, TELightning1, 7,
		[3]float32{1, 2, 3}, [3]float32{4, 5, 6})
	// Drop final byte.
	sr := newReader(full[:len(full)-1])
	v, err := sr.Next(protocol.VersionNQ)
	if !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
	if v != nil {
		t.Errorf("value: got %v want nil", v)
	}
}

// ------- TempEntityKind constant-drift detector -----------------

// Pins the TempEntityKind values to the canonical protocol.TE_* wire
// codes. A drift in either side (re-ordering, off-by-one, dropped
// entry) breaks this test instantly.
func TestTempEntityKind_NoDrift(t *testing.T) {
	cases := []struct {
		name string
		got  TempEntityKind
		want int
	}{
		{"TESpike", TESpike, 0},
		{"TESuperSpike", TESuperSpike, 1},
		{"TEGunshot", TEGunshot, 2},
		{"TEExplosion", TEExplosion, 3},
		{"TETarExplosion", TETarExplosion, 4},
		{"TELightning1", TELightning1, 5},
		{"TELightning2", TELightning2, 6},
		{"TEWizSpike", TEWizSpike, 7},
		{"TEKnightSpike", TEKnightSpike, 8},
		{"TELightning3", TELightning3, 9},
		{"TELavaSplash", TELavaSplash, 10},
		{"TETeleport", TETeleport, 11},
		{"TEExplosion2", TEExplosion2, 12},
		{"TEBeam", TEBeam, 13},
	}
	for _, c := range cases {
		if int(c.got) != c.want {
			t.Errorf("%s: got %d want %d", c.name, c.got, c.want)
		}
	}
	// Pin the linkage to the protocol package too: client.TE* must
	// equal protocol.TE* (one source of truth on the wire).
	pin := map[TempEntityKind]int{
		TESpike: protocol.TESpike, TESuperSpike: protocol.TESuperSpike,
		TEGunshot: protocol.TEGunshot, TEExplosion: protocol.TEExplosion,
		TETarExplosion: protocol.TETarExplosion, TELightning1: protocol.TELightning1,
		TELightning2: protocol.TELightning2, TEWizSpike: protocol.TEWizSpike,
		TEKnightSpike: protocol.TEKnightSpike, TELightning3: protocol.TELightning3,
		TELavaSplash: protocol.TELavaSplash, TETeleport: protocol.TETeleport,
		TEExplosion2: protocol.TEExplosion2, TEBeam: protocol.TEBeam,
	}
	for k, v := range pin {
		if int(k) != v {
			t.Errorf("client/protocol mismatch on kind=%d (protocol value %d)", int(k), v)
		}
	}
}

// temptyNames maps a TempEntityKind to a stable subtest label. Used
// by table-driven tests for readable -run filters.
var temptyNames = map[TempEntityKind]string{
	TESpike:        "TESpike",
	TESuperSpike:   "TESuperSpike",
	TEGunshot:      "TEGunshot",
	TEExplosion:    "TEExplosion",
	TETarExplosion: "TETarExplosion",
	TELightning1:   "TELightning1",
	TELightning2:   "TELightning2",
	TEWizSpike:     "TEWizSpike",
	TEKnightSpike:  "TEKnightSpike",
	TELightning3:   "TELightning3",
	TELavaSplash:   "TELavaSplash",
	TETeleport:     "TETeleport",
	TEExplosion2:   "TEExplosion2",
	TEBeam:         "TEBeam",
}

func temptyName(k TempEntityKind) string { return temptyNames[k] }
