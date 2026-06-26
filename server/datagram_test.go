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

// Happy path: every field round-trips through msg.Read* in the order
// the wire format defines.
func TestEncodeParticle_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeParticle(buf, [3]float32{8, 16, 24}, [3]float32{0.5, -1, 2}, 73, 5); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 12 {
		t.Errorf("wire size: got %d want 12", buf.Len())
	}

	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcParticle {
		t.Errorf("cmd byte: got %d want %d (SvcParticle)", cmd, protocol.SvcParticle)
	}
	for axis, want := range [3]float32{8, 16, 24} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("org[%d]: got %v want %v", axis, got, want)
		}
	}
	// dir bytes: dir*16 clamped to int8. For (0.5, -1, 2) -> (8, -16, 32).
	for axis, want := range [3]int{8, -16, 32} {
		if got := r.ReadChar(); got != want {
			t.Errorf("dir[%d]: got %d want %d", axis, got, want)
		}
	}
	if got := r.ReadU8(); got != 5 {
		t.Errorf("count: got %d want 5", got)
	}
	if got := r.ReadU8(); got != 73 {
		t.Errorf("color: got %d want 73", got)
	}
}

// Direction clamping at the upper bound: anything that would push
// past 127 saturates at 127.
func TestEncodeParticle_DirClampHigh(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	// dir*16 == 16*16 == 256, way past 127. Should clamp.
	if err := EncodeParticle(buf, [3]float32{0, 0, 0}, [3]float32{16, 16, 16}, 0, 1); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // cmd
	for i := 0; i < 3; i++ {
		_ = r.ReadCoord()
	}
	for axis := 0; axis < 3; axis++ {
		if got := r.ReadChar(); got != 127 {
			t.Errorf("dir[%d] should clamp to 127, got %d", axis, got)
		}
	}
}

// Direction clamping at the lower bound: anything that would push
// past -128 saturates at -128.
func TestEncodeParticle_DirClampLow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeParticle(buf, [3]float32{0, 0, 0}, [3]float32{-16, -16, -16}, 0, 1); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()
	for i := 0; i < 3; i++ {
		_ = r.ReadCoord()
	}
	for axis := 0; axis < 3; axis++ {
		if got := r.ReadChar(); got != -128 {
			t.Errorf("dir[%d] should clamp to -128, got %d", axis, got)
		}
	}
}

// Nil sizebuf -> error.
func TestEncodeParticle_NilBufErrors(t *testing.T) {
	if err := EncodeParticle(nil, [3]float32{}, [3]float32{}, 0, 0); err == nil {
		t.Error("expected error on nil sizebuf")
	}
}

// Datagram nearly full -> ErrDatagramFull, no bytes written.
func TestEncodeParticle_DatagramFull(t *testing.T) {
	buf := sizebuf.New(make([]byte, MaxDatagram))
	// Fill within particleReserve bytes of MaxDatagram.
	filler := make([]byte, MaxDatagram-particleReserve+1)
	if err := buf.Write(filler); err != nil {
		t.Fatal(err)
	}
	prevLen := buf.Len()
	err := EncodeParticle(buf, [3]float32{}, [3]float32{}, 0, 0)
	if !errors.Is(err, ErrDatagramFull) {
		t.Errorf("got %v want ErrDatagramFull", err)
	}
	if buf.Len() != prevLen {
		t.Errorf("buffer modified on overflow: was %d, now %d", prevLen, buf.Len())
	}
}

// Per-write error propagation: each msg.Write* call inside
// EncodeParticle has its own err-return branch. To cover every
// branch we run the encoder with successively-larger buffers that
// each fail one byte LATER than the previous, walking through
// every write site.
//
// Wire layout (12 bytes): byte cmd | 3*2 coord | 3*1 char | byte count | byte color
//
//	cap 0  -> fails on cmd byte (line 55)
//	cap 1  -> fits cmd (1), fails on coord[0] (line 59)
//	cap 8  -> fits cmd + 3 coords (7) + 1 char, fails on the 2nd char (line 72)
//	cap 10 -> fits cmd + 3 coords + 3 chars (10), fails on count (line 76)
//	cap 11 -> fits all prior + count (11), fails on color (line 79)
//
// (cap 12 succeeds clean.)
func TestEncodeParticle_PerWriteOverflowPropagates(t *testing.T) {
	for _, cap := range []int{0, 1, 8, 10, 11} {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, cap))
			err := EncodeParticle(buf, [3]float32{}, [3]float32{}, 0, 0)
			if err == nil || errors.Is(err, ErrDatagramFull) {
				t.Errorf("cap=%d: expected propagated write error, got %v", cap, err)
			}
		})
	}
	// Sanity: cap 12 must succeed.
	buf := sizebuf.New(make([]byte, 12))
	if err := EncodeParticle(buf, [3]float32{}, [3]float32{}, 0, 0); err != nil {
		t.Errorf("cap=12: expected success, got %v", err)
	}
}

// particleReserve drift detector: catch any accidental change to the
// upstream's MAX_DATAGRAM - 16 margin.
func TestParticleReserve_TyrquakeValue(t *testing.T) {
	if particleReserve != 16 {
		t.Errorf("particleReserve drift: got %d want 16 (tyrquake)", particleReserve)
	}
}

// --- EncodeLightning -------------------------------------------------------

// Happy path: every field round-trips through msg.Read* in the order
// the wire format defines.
func TestEncodeLightning_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeLightning(buf, protocol.TELightning2, 7,
		[3]float32{1, 2, 3}, [3]float32{61, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 16 {
		t.Errorf("wire size: got %d want 16", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcTempEntity {
		t.Errorf("cmd: got %d want SvcTempEntity(%d)", cmd, protocol.SvcTempEntity)
	}
	if k := r.ReadU8(); k != protocol.TELightning2 {
		t.Errorf("kind: got %d want TELightning2(%d)", k, protocol.TELightning2)
	}
	if e := r.ReadShort(); e != 7 {
		t.Errorf("entity: got %d want 7", e)
	}
	for axis, want := range [3]float32{1, 2, 3} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("start[%d]: got %v want %v", axis, got, want)
		}
	}
	for axis, want := range [3]float32{61, 2, 3} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("end[%d]: got %v want %v", axis, got, want)
		}
	}
}

// All four kinds round-trip.
func TestEncodeLightning_AllKindsAccepted(t *testing.T) {
	kinds := []int{
		protocol.TELightning1,
		protocol.TELightning2,
		protocol.TELightning3,
		protocol.TEBeam,
	}
	for _, k := range kinds {
		buf := sizebuf.New(make([]byte, 64))
		if err := EncodeLightning(buf, k, 1,
			[3]float32{}, [3]float32{30, 0, 0}); err != nil {
			t.Errorf("kind=%d: %v", k, err)
		}
		r := msg.NewReader(buf.Bytes())
		_ = r.ReadU8()
		if got := r.ReadU8(); got != k {
			t.Errorf("kind wire byte: got %d want %d", got, k)
		}
	}
}

// Bad kind -> ErrLightningKind, no bytes written.
func TestEncodeLightning_RejectsUnknownKind(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeLightning(buf, 0x42, 1, [3]float32{}, [3]float32{30, 0, 0})
	if !errors.Is(err, ErrLightningKind) {
		t.Errorf("got %v want ErrLightningKind", err)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer mutated on bad-kind reject: %d bytes", buf.Len())
	}
}

// Nil sizebuf -> error.
func TestEncodeLightning_NilBufErrors(t *testing.T) {
	if err := EncodeLightning(nil, protocol.TELightning2, 0,
		[3]float32{}, [3]float32{}); err == nil {
		t.Error("expected error on nil sizebuf")
	}
}

// Datagram nearly full -> ErrDatagramFull, no bytes written.
func TestEncodeLightning_DatagramFull(t *testing.T) {
	buf := sizebuf.New(make([]byte, MaxDatagram))
	filler := make([]byte, MaxDatagram-lightningReserve+1)
	if err := buf.Write(filler); err != nil {
		t.Fatal(err)
	}
	prevLen := buf.Len()
	err := EncodeLightning(buf, protocol.TELightning2, 1,
		[3]float32{}, [3]float32{30, 0, 0})
	if !errors.Is(err, ErrDatagramFull) {
		t.Errorf("got %v want ErrDatagramFull", err)
	}
	if buf.Len() != prevLen {
		t.Errorf("buffer modified on overflow: was %d, now %d", prevLen, buf.Len())
	}
}

// Per-write error propagation walk: each msg.Write* inside
// EncodeLightning has its own err-return branch. Wire layout (16
// bytes): cmd | kind | short ent (2) | 3 coord start (6) | 3 coord
// end (6). Successively larger capacities each fail one byte LATER
// than the previous, walking every write site.
func TestEncodeLightning_PerWriteOverflowPropagates(t *testing.T) {
	caps := []int{0, 1, 2, 4, 5, 9, 11, 15}
	for _, capN := range caps {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, capN))
			err := EncodeLightning(buf, protocol.TELightning2, 1,
				[3]float32{}, [3]float32{30, 0, 0})
			if err == nil || errors.Is(err, ErrDatagramFull) {
				t.Errorf("cap=%d: expected propagated write error, got %v", capN, err)
			}
		})
	}
	// Sanity: cap 16 succeeds.
	buf := sizebuf.New(make([]byte, 16))
	if err := EncodeLightning(buf, protocol.TELightning2, 1,
		[3]float32{}, [3]float32{30, 0, 0}); err != nil {
		t.Errorf("cap=16: expected success, got %v", err)
	}
}

// lightningReserve drift detector.
func TestLightningReserve_DefaultValue(t *testing.T) {
	if lightningReserve != 24 {
		t.Errorf("lightningReserve drift: got %d want 24", lightningReserve)
	}
}
