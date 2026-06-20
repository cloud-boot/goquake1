// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// Protocol-constant exact values used here (audit trail):
//
//	protocol.SvcUpdateStat     = 3
//	protocol.SvcUpdateName     = 13
//	protocol.SvcUpdateFrags    = 14
//	protocol.SvcUpdateColors   = 17
//	protocol.SvcKilledMonster  = 27
//	protocol.SvcFoundSecret    = 28
//	protocol.SvcSellScreen     = 33

// --- EncodeUpdateName ----------------------------------------------

func TestEncodeUpdateName_HappyPath(t *testing.T) {
	const name = "BlubBlub"
	buf := sizebuf.New(make([]byte, 32))
	if err := EncodeUpdateName(buf, 5, name); err != nil {
		t.Fatal(err)
	}
	// 1 byte svc + 1 byte slot + len(name) + 1 NUL.
	want := 1 + 1 + len(name) + 1
	if buf.Len() != want {
		t.Errorf("wire size: got %d want %d", buf.Len(), want)
	}
	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcUpdateName {
		t.Errorf("cmd byte: got %d want %d", cmd, protocol.SvcUpdateName)
	}
	if slot := r.ReadU8(); slot != 5 {
		t.Errorf("slot: got %d want 5", slot)
	}
	if got := r.ReadString(); got != name {
		t.Errorf("name: got %q want %q", got, name)
	}
}

func TestEncodeUpdateName_SlotBoundary(t *testing.T) {
	// Both endpoints of the [0, 255] range must be accepted.
	for _, slot := range []int{0, 255} {
		buf := sizebuf.New(make([]byte, 8))
		if err := EncodeUpdateName(buf, slot, "x"); err != nil {
			t.Errorf("slot=%d: %v", slot, err)
		}
		if buf.Bytes()[1] != byte(slot) {
			t.Errorf("slot=%d: byte got %d", slot, buf.Bytes()[1])
		}
	}
}

func TestEncodeUpdateName_SlotRangeReject(t *testing.T) {
	for _, slot := range []int{-1, 256, 1000} {
		buf := sizebuf.New(make([]byte, 16))
		err := EncodeUpdateName(buf, slot, "x")
		if !errors.Is(err, ErrSlotRange) {
			t.Errorf("slot=%d: got %v want ErrSlotRange", slot, err)
		}
		if buf.Len() != 0 {
			t.Errorf("slot=%d: buf was modified (len=%d)", slot, buf.Len())
		}
	}
}

func TestEncodeUpdateName_NilBuf(t *testing.T) {
	if err := EncodeUpdateName(nil, 0, "x"); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// Per-write overflow propagation. cap 0 -> cmd byte fails; cap 1 ->
// slot byte fails; cap 2 -> string write fails.
func TestEncodeUpdateName_Overflow(t *testing.T) {
	for _, cap := range []int{0, 1, 2} {
		buf := sizebuf.New(make([]byte, cap))
		if err := EncodeUpdateName(buf, 0, "abc"); err == nil {
			t.Errorf("cap=%d: expected overflow, got nil", cap)
		}
	}
}

// --- EncodeUpdateColors --------------------------------------------

func TestEncodeUpdateColors_HappyPath(t *testing.T) {
	// 0x47 = shirt 4, pants 7 (the packed nibble layout the
	// scoreboard renders).
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeUpdateColors(buf, 2, 0x47); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 3 {
		t.Errorf("wire size: got %d want 3", buf.Len())
	}
	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcUpdateColors {
		t.Errorf("cmd byte: got %d want %d", cmd, protocol.SvcUpdateColors)
	}
	if slot := r.ReadU8(); slot != 2 {
		t.Errorf("slot: got %d want 2", slot)
	}
	if got := r.ReadU8(); got != 0x47 {
		t.Errorf("colors: got %#x want 0x47", got)
	}
}

func TestEncodeUpdateColors_SlotRangeReject(t *testing.T) {
	for _, slot := range []int{-1, 256} {
		buf := sizebuf.New(make([]byte, 4))
		err := EncodeUpdateColors(buf, slot, 0)
		if !errors.Is(err, ErrSlotRange) {
			t.Errorf("slot=%d: got %v want ErrSlotRange", slot, err)
		}
		if buf.Len() != 0 {
			t.Errorf("slot=%d: buf was modified (len=%d)", slot, buf.Len())
		}
	}
}

func TestEncodeUpdateColors_ColorsRangeReject(t *testing.T) {
	for _, colors := range []int{-1, 256} {
		buf := sizebuf.New(make([]byte, 4))
		err := EncodeUpdateColors(buf, 0, colors)
		if !errors.Is(err, ErrSlotRange) {
			t.Errorf("colors=%d: got %v want ErrSlotRange", colors, err)
		}
		if buf.Len() != 0 {
			t.Errorf("colors=%d: buf was modified (len=%d)", colors, buf.Len())
		}
	}
}

func TestEncodeUpdateColors_NilBuf(t *testing.T) {
	if err := EncodeUpdateColors(nil, 0, 0); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// cap 0 -> cmd byte fails; cap 1 -> slot byte fails; cap 2 -> colors
// byte fails.
func TestEncodeUpdateColors_Overflow(t *testing.T) {
	for _, cap := range []int{0, 1, 2} {
		buf := sizebuf.New(make([]byte, cap))
		if err := EncodeUpdateColors(buf, 0, 0); err == nil {
			t.Errorf("cap=%d: expected overflow, got nil", cap)
		}
	}
}

// --- EncodeUpdateFrags ---------------------------------------------

func TestEncodeUpdateFrags_HappyPath(t *testing.T) {
	// Cover positive, zero, negative, and both int16 endpoints.
	for _, frags := range []int{0, 42, -7, 32767, -32768} {
		buf := sizebuf.New(make([]byte, 8))
		if err := EncodeUpdateFrags(buf, 3, frags); err != nil {
			t.Fatalf("frags=%d: %v", frags, err)
		}
		if buf.Len() != 4 {
			t.Errorf("frags=%d: wire size got %d want 4", frags, buf.Len())
		}
		r := msg.NewReader(buf.Bytes())
		if cmd := r.ReadU8(); cmd != protocol.SvcUpdateFrags {
			t.Errorf("frags=%d: cmd byte got %d want %d", frags, cmd, protocol.SvcUpdateFrags)
		}
		if slot := r.ReadU8(); slot != 3 {
			t.Errorf("frags=%d: slot got %d want 3", frags, slot)
		}
		// ReadShort sign-extends, so the round-trip matches the
		// signed input directly.
		if got := r.ReadShort(); got != frags {
			t.Errorf("frags=%d: round-trip got %d", frags, got)
		}
	}
}

func TestEncodeUpdateFrags_SlotRangeReject(t *testing.T) {
	for _, slot := range []int{-1, 256} {
		buf := sizebuf.New(make([]byte, 8))
		err := EncodeUpdateFrags(buf, slot, 0)
		if !errors.Is(err, ErrSlotRange) {
			t.Errorf("slot=%d: got %v want ErrSlotRange", slot, err)
		}
		if buf.Len() != 0 {
			t.Errorf("slot=%d: buf was modified (len=%d)", slot, buf.Len())
		}
	}
}

func TestEncodeUpdateFrags_FragsRangeReject(t *testing.T) {
	for _, frags := range []int{-32769, 32768, 100000, -100000} {
		buf := sizebuf.New(make([]byte, 8))
		err := EncodeUpdateFrags(buf, 0, frags)
		if !errors.Is(err, ErrFragsRange) {
			t.Errorf("frags=%d: got %v want ErrFragsRange", frags, err)
		}
		if buf.Len() != 0 {
			t.Errorf("frags=%d: buf was modified (len=%d)", frags, buf.Len())
		}
	}
}

func TestEncodeUpdateFrags_NilBuf(t *testing.T) {
	if err := EncodeUpdateFrags(nil, 0, 0); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// cap 0 -> cmd byte fails; cap 1 -> slot byte fails; cap 2 -> short
// fails (needs 2 bytes).
func TestEncodeUpdateFrags_Overflow(t *testing.T) {
	for _, cap := range []int{0, 1, 2} {
		buf := sizebuf.New(make([]byte, cap))
		if err := EncodeUpdateFrags(buf, 0, 0); err == nil {
			t.Errorf("cap=%d: expected overflow, got nil", cap)
		}
	}
}

// --- EncodeUpdateStat ----------------------------------------------

func TestEncodeUpdateStat_HappyPath(t *testing.T) {
	type stCase struct {
		stat  int
		value int32
	}
	cases := []stCase{
		{0, 0},
		{1, 100},  // small stat, positive value
		{255, -1}, // max stat, negative
		{32, math.MaxInt32},
		{32, math.MinInt32},
	}
	for _, c := range cases {
		buf := sizebuf.New(make([]byte, 8))
		if err := EncodeUpdateStat(buf, c.stat, c.value); err != nil {
			t.Fatalf("stat=%d value=%d: %v", c.stat, c.value, err)
		}
		if buf.Len() != 6 {
			t.Errorf("stat=%d value=%d: wire size got %d want 6",
				c.stat, c.value, buf.Len())
		}
		r := msg.NewReader(buf.Bytes())
		if cmd := r.ReadU8(); cmd != protocol.SvcUpdateStat {
			t.Errorf("stat=%d value=%d: cmd byte got %d want %d",
				c.stat, c.value, cmd, protocol.SvcUpdateStat)
		}
		if stat := r.ReadU8(); stat != c.stat {
			t.Errorf("stat=%d value=%d: stat got %d", c.stat, c.value, stat)
		}
		if got := r.ReadLong(); got != c.value {
			t.Errorf("stat=%d value=%d: value round-trip got %d",
				c.stat, c.value, got)
		}
	}
}

func TestEncodeUpdateStat_StatRangeReject(t *testing.T) {
	for _, stat := range []int{-1, 256, 1000} {
		buf := sizebuf.New(make([]byte, 8))
		err := EncodeUpdateStat(buf, stat, 0)
		if !errors.Is(err, ErrStatRange) {
			t.Errorf("stat=%d: got %v want ErrStatRange", stat, err)
		}
		if buf.Len() != 0 {
			t.Errorf("stat=%d: buf was modified (len=%d)", stat, buf.Len())
		}
	}
}

func TestEncodeUpdateStat_NilBuf(t *testing.T) {
	if err := EncodeUpdateStat(nil, 0, 0); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// cap 0 -> cmd byte fails; cap 1 -> stat byte fails; cap 2-4 -> long
// (4-byte) write fails.
func TestEncodeUpdateStat_Overflow(t *testing.T) {
	for _, cap := range []int{0, 1, 2, 3, 4} {
		buf := sizebuf.New(make([]byte, cap))
		if err := EncodeUpdateStat(buf, 0, 0); err == nil {
			t.Errorf("cap=%d: expected overflow, got nil", cap)
		}
	}
}

// --- 3 single-byte encoders ----------------------------------------
//
// Identical wire shape (one svc byte), so they share a parameterised
// test runner.

type byteEncoder struct {
	name string
	fn   func(*sizebuf.Buffer) error
	svc  int
}

func byteEncoders() []byteEncoder {
	return []byteEncoder{
		{"KilledMonster", EncodeKilledMonster, protocol.SvcKilledMonster},
		{"FoundSecret", EncodeFoundSecret, protocol.SvcFoundSecret},
		{"SellScreen", EncodeSellScreen, protocol.SvcSellScreen},
	}
}

func TestByteEncoders_HappyPath(t *testing.T) {
	for _, e := range byteEncoders() {
		t.Run(e.name, func(t *testing.T) {
			buf := sizebuf.New(make([]byte, 4))
			if err := e.fn(buf); err != nil {
				t.Fatal(err)
			}
			if buf.Len() != 1 {
				t.Errorf("wire size: got %d want 1", buf.Len())
			}
			if got := buf.Bytes()[0]; int(got) != e.svc {
				t.Errorf("cmd byte: got %d want %d", got, e.svc)
			}
		})
	}
}

func TestByteEncoders_NilBuf(t *testing.T) {
	for _, e := range byteEncoders() {
		t.Run(e.name, func(t *testing.T) {
			if err := e.fn(nil); !errors.Is(err, ErrNilBuf) {
				t.Errorf("got %v want ErrNilBuf", err)
			}
		})
	}
}

func TestByteEncoders_Overflow(t *testing.T) {
	for _, e := range byteEncoders() {
		t.Run(e.name, func(t *testing.T) {
			buf := sizebuf.New(make([]byte, 0))
			if err := e.fn(buf); err == nil {
				t.Error("expected overflow on zero-cap buf")
			}
		})
	}
}
