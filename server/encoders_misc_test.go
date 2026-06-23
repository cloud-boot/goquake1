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

// Protocol-constant exact values used here (audit trail):
//
//	protocol.SvcNop        = 1
//	protocol.SvcDisconnect = 2
//	protocol.SvcSetView    = 5
//	protocol.SvcStuffText  = 9
//	protocol.SvcSignonNum  = 25
//	protocol.SvcFinale     = 31
//	protocol.SvcCutscene   = 34
//
// (SvcSignonNum and SvcStuffText use the camel-cased spellings the
// protocol package ships -- not Signonnum / Stufftext.)

// --- EncodeNop -----------------------------------------------------

func TestEncodeNop_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeNop(buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("wire size: got %d want 1", buf.Len())
	}
	if got := buf.Bytes()[0]; got != protocol.SvcNop {
		t.Errorf("cmd byte: got %d want %d (SvcNop)", got, protocol.SvcNop)
	}
}

func TestEncodeNop_NilBuf(t *testing.T) {
	if err := EncodeNop(nil); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

func TestEncodeNop_Overflow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeNop(buf); err == nil {
		t.Error("expected overflow error on zero-cap buf")
	}
}

// --- EncodeIntermission --------------------------------------------

func TestEncodeIntermission_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeIntermission(buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("wire size: got %d want 1", buf.Len())
	}
	if got := buf.Bytes()[0]; got != protocol.SvcIntermission {
		t.Errorf("cmd byte: got %d want %d (SvcIntermission)", got, protocol.SvcIntermission)
	}
}

func TestEncodeIntermission_NilBuf(t *testing.T) {
	if err := EncodeIntermission(nil); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

func TestEncodeIntermission_Overflow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeIntermission(buf); err == nil {
		t.Error("expected overflow error on zero-cap buf")
	}
}

// --- EncodeDisconnect ----------------------------------------------

func TestEncodeDisconnect_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeDisconnect(buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("wire size: got %d want 1", buf.Len())
	}
	if got := buf.Bytes()[0]; got != protocol.SvcDisconnect {
		t.Errorf("cmd byte: got %d want %d (SvcDisconnect)", got, protocol.SvcDisconnect)
	}
}

func TestEncodeDisconnect_NilBuf(t *testing.T) {
	if err := EncodeDisconnect(nil); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

func TestEncodeDisconnect_Overflow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeDisconnect(buf); err == nil {
		t.Error("expected overflow error on zero-cap buf")
	}
}

// --- EncodeSetView -------------------------------------------------

func TestEncodeSetView_HappyPath(t *testing.T) {
	for _, ent := range []int{0, 1, 65535} {
		buf := sizebuf.New(make([]byte, 8))
		if err := EncodeSetView(buf, ent); err != nil {
			t.Fatalf("ent=%d: %v", ent, err)
		}
		if buf.Len() != 3 {
			t.Errorf("ent=%d: wire size got %d want 3", ent, buf.Len())
		}
		r := msg.NewReader(buf.Bytes())
		if cmd := r.ReadU8(); cmd != protocol.SvcSetView {
			t.Errorf("ent=%d: cmd byte got %d want %d", ent, cmd, protocol.SvcSetView)
		}
		// ReadShort sign-extends, so 65535 round-trips as -1. Read as
		// little-endian unsigned by hand for the range check.
		lo := buf.Bytes()[1]
		hi := buf.Bytes()[2]
		got := int(lo) | int(hi)<<8
		if got != ent {
			t.Errorf("ent=%d: round-trip got %d", ent, got)
		}
	}
}

func TestEncodeSetView_RangeReject(t *testing.T) {
	for _, ent := range []int{-1, 0x10000, 100000} {
		buf := sizebuf.New(make([]byte, 8))
		err := EncodeSetView(buf, ent)
		if !errors.Is(err, ErrEntityNumRange) {
			t.Errorf("ent=%d: got %v want ErrEntityNumRange", ent, err)
		}
		if buf.Len() != 0 {
			t.Errorf("ent=%d: buf was modified (len=%d)", ent, buf.Len())
		}
	}
}

func TestEncodeSetView_NilBuf(t *testing.T) {
	if err := EncodeSetView(nil, 1); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// Per-write overflow propagation: cap 0 fails on cmd byte, cap 1
// fails on the short.
func TestEncodeSetView_Overflow(t *testing.T) {
	for _, cap := range []int{0, 1} {
		buf := sizebuf.New(make([]byte, cap))
		if err := EncodeSetView(buf, 1); err == nil {
			t.Errorf("cap=%d: expected overflow, got nil", cap)
		}
	}
}

// --- EncodeSignonNum -----------------------------------------------

func TestEncodeSignonNum_HappyStages(t *testing.T) {
	for _, stage := range []int{1, 2, 3, 4} {
		buf := sizebuf.New(make([]byte, 4))
		if err := EncodeSignonNum(buf, stage); err != nil {
			t.Fatalf("stage=%d: %v", stage, err)
		}
		if buf.Len() != 2 {
			t.Errorf("stage=%d: wire size got %d want 2", stage, buf.Len())
		}
		r := msg.NewReader(buf.Bytes())
		if cmd := r.ReadU8(); cmd != protocol.SvcSignonNum {
			t.Errorf("stage=%d: cmd byte got %d want %d", stage, cmd, protocol.SvcSignonNum)
		}
		if got := r.ReadU8(); got != stage {
			t.Errorf("stage=%d: got %d", stage, got)
		}
	}
}

func TestEncodeSignonNum_RangeReject(t *testing.T) {
	for _, stage := range []int{0, 5, -1, 100} {
		buf := sizebuf.New(make([]byte, 4))
		err := EncodeSignonNum(buf, stage)
		if !errors.Is(err, ErrSignonStageRange) {
			t.Errorf("stage=%d: got %v want ErrSignonStageRange", stage, err)
		}
		if buf.Len() != 0 {
			t.Errorf("stage=%d: buf was modified (len=%d)", stage, buf.Len())
		}
	}
}

func TestEncodeSignonNum_NilBuf(t *testing.T) {
	if err := EncodeSignonNum(nil, 1); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

func TestEncodeSignonNum_Overflow(t *testing.T) {
	for _, cap := range []int{0, 1} {
		buf := sizebuf.New(make([]byte, cap))
		if err := EncodeSignonNum(buf, 1); err == nil {
			t.Errorf("cap=%d: expected overflow, got nil", cap)
		}
	}
}

// --- EncodeFinale / EncodeCutscene / EncodeStuffText ---------------
//
// Same wire shape for all three (svc byte + NUL-terminated string),
// so they share a parameterised test runner.

type stringEncoder struct {
	name string
	fn   func(*sizebuf.Buffer, string) error
	svc  int
}

func stringEncoders() []stringEncoder {
	return []stringEncoder{
		{"Finale", EncodeFinale, protocol.SvcFinale},
		{"Cutscene", EncodeCutscene, protocol.SvcCutscene},
		{"StuffText", EncodeStuffText, protocol.SvcStuffText},
		{"CenterPrint", EncodeCenterPrint, protocol.SvcCenterPrint},
	}
}

func TestStringEncoders_HappyPath(t *testing.T) {
	for _, e := range stringEncoders() {
		t.Run(e.name, func(t *testing.T) {
			const payload = "hello"
			buf := sizebuf.New(make([]byte, 32))
			if err := e.fn(buf, payload); err != nil {
				t.Fatal(err)
			}
			// 1 byte svc + len(payload) + 1 NUL.
			want := 1 + len(payload) + 1
			if buf.Len() != want {
				t.Errorf("wire size: got %d want %d", buf.Len(), want)
			}
			r := msg.NewReader(buf.Bytes())
			if cmd := r.ReadU8(); cmd != e.svc {
				t.Errorf("cmd byte: got %d want %d", cmd, e.svc)
			}
			if got := r.ReadString(); got != payload {
				t.Errorf("payload: got %q want %q", got, payload)
			}
		})
	}
}

func TestStringEncoders_EmptyString(t *testing.T) {
	// Empty string still emits the lone NUL.
	for _, e := range stringEncoders() {
		t.Run(e.name, func(t *testing.T) {
			buf := sizebuf.New(make([]byte, 4))
			if err := e.fn(buf, ""); err != nil {
				t.Fatal(err)
			}
			if buf.Len() != 2 {
				t.Errorf("wire size: got %d want 2", buf.Len())
			}
			if buf.Bytes()[1] != 0 {
				t.Errorf("NUL terminator missing: %v", buf.Bytes())
			}
		})
	}
}

func TestStringEncoders_NilBuf(t *testing.T) {
	for _, e := range stringEncoders() {
		t.Run(e.name, func(t *testing.T) {
			if err := e.fn(nil, "x"); !errors.Is(err, ErrNilBuf) {
				t.Errorf("got %v want ErrNilBuf", err)
			}
		})
	}
}

// Per-write overflow propagation. Cap 0 -> cmd byte fails;
// cap 1 -> cmd byte fits, string write fails.
func TestStringEncoders_Overflow(t *testing.T) {
	for _, e := range stringEncoders() {
		t.Run(e.name, func(t *testing.T) {
			for _, cap := range []int{0, 1} {
				buf := sizebuf.New(make([]byte, cap))
				if err := e.fn(buf, "abc"); err == nil {
					t.Errorf("cap=%d: expected overflow, got nil", cap)
				}
			}
		})
	}
}

// --- EncodeCDTrack --------------------------------------------------

func TestEncodeCDTrack_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := EncodeCDTrack(buf, 5, 7); err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
	got := buf.Bytes()
	want := []byte{protocol.SvcCDTrack, 5, 7}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d: got 0x%02x want 0x%02x", i, got[i], want[i])
		}
	}
	// Sanity-decode via msg.Reader so the byte-stream parses back.
	r := msg.NewReader(got)
	if r.ReadU8() != protocol.SvcCDTrack {
		t.Error("cmd byte mismatch on decode")
	}
	if r.ReadU8() != 5 {
		t.Error("track byte mismatch")
	}
	if r.ReadU8() != 7 {
		t.Error("loopTrack byte mismatch")
	}
}

func TestEncodeCDTrack_TrackZero(t *testing.T) {
	// Track == 0 is wire-legal (silence command).
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeCDTrack(buf, 0, 0); err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
}

func TestEncodeCDTrack_NilBuf(t *testing.T) {
	if err := EncodeCDTrack(nil, 1, 1); !errors.Is(err, ErrNilBuf) {
		t.Errorf("err: got %v want ErrNilBuf", err)
	}
}

func TestEncodeCDTrack_TrackOutOfRange(t *testing.T) {
	for _, track := range []int{-1, 256, 999} {
		buf := sizebuf.New(make([]byte, 4))
		err := EncodeCDTrack(buf, track, 0)
		if !errors.Is(err, ErrCDTrackRange) {
			t.Errorf("track=%d: got %v want ErrCDTrackRange", track, err)
		}
	}
}

func TestEncodeCDTrack_LoopTrackOutOfRange(t *testing.T) {
	for _, loop := range []int{-1, 256, 1024} {
		buf := sizebuf.New(make([]byte, 4))
		err := EncodeCDTrack(buf, 1, loop)
		if !errors.Is(err, ErrCDTrackRange) {
			t.Errorf("loopTrack=%d: got %v want ErrCDTrackRange", loop, err)
		}
	}
}

func TestEncodeCDTrack_OverflowOnCmdByte(t *testing.T) {
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeCDTrack(buf, 1, 1); err == nil {
		t.Errorf("expected overflow at cmd byte, got nil")
	}
}

func TestEncodeCDTrack_OverflowOnTrackByte(t *testing.T) {
	buf := sizebuf.New(make([]byte, 1))
	if err := EncodeCDTrack(buf, 1, 1); err == nil {
		t.Errorf("expected overflow at track byte, got nil")
	}
}

func TestEncodeCDTrack_OverflowOnLoopByte(t *testing.T) {
	buf := sizebuf.New(make([]byte, 2))
	if err := EncodeCDTrack(buf, 1, 1); err == nil {
		t.Errorf("expected overflow at loop byte, got nil")
	}
}
