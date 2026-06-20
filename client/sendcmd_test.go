// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sizebuf"
)

// --- EncodeClcOpcode ------------------------------------------------

func TestEncodeClcOpcode_Nop(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeClcOpcode(buf, protocol.ClcNop); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if len(got) != 1 || got[0] != protocol.ClcNop {
		t.Errorf("wire: got %v want [%d]", got, protocol.ClcNop)
	}
}

func TestEncodeClcOpcode_Disconnect(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeClcOpcode(buf, protocol.ClcDisconnect); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if len(got) != 1 || got[0] != protocol.ClcDisconnect {
		t.Errorf("wire: got %v want [%d]", got, protocol.ClcDisconnect)
	}
}

func TestEncodeClcOpcode_NilBuf(t *testing.T) {
	if err := EncodeClcOpcode(nil, protocol.ClcNop); !errors.Is(err, ErrSendNilBuf) {
		t.Errorf("got %v want ErrSendNilBuf", err)
	}
}

func TestEncodeClcOpcode_Bad(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	for _, op := range []int{protocol.ClcBad, protocol.ClcMove, protocol.ClcStringCmd, 42} {
		if err := EncodeClcOpcode(buf, op); !errors.Is(err, ErrSendBadOpcode) {
			t.Errorf("op=%d: got %v want ErrSendBadOpcode", op, err)
		}
	}
}

// --- EncodeClcNop ---------------------------------------------------

func TestEncodeClcNop_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeClcNop(buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("wire size: got %d want 1", buf.Len())
	}
	if got := buf.Bytes()[0]; got != protocol.ClcNop {
		t.Errorf("cmd byte: got %d want %d (ClcNop)", got, protocol.ClcNop)
	}
}

func TestEncodeClcNop_NilBuf(t *testing.T) {
	if err := EncodeClcNop(nil); !errors.Is(err, ErrSendNilBuf) {
		t.Errorf("got %v want ErrSendNilBuf", err)
	}
}

func TestEncodeClcNop_Overflow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeClcNop(buf); err == nil {
		t.Error("expected overflow error on zero-cap buf")
	}
}

// --- EncodeClcDisconnect -------------------------------------------

func TestEncodeClcDisconnect_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 4))
	if err := EncodeClcDisconnect(buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("wire size: got %d want 1", buf.Len())
	}
	if got := buf.Bytes()[0]; got != protocol.ClcDisconnect {
		t.Errorf("cmd byte: got %d want %d (ClcDisconnect)", got, protocol.ClcDisconnect)
	}
}

func TestEncodeClcDisconnect_NilBuf(t *testing.T) {
	if err := EncodeClcDisconnect(nil); !errors.Is(err, ErrSendNilBuf) {
		t.Errorf("got %v want ErrSendNilBuf", err)
	}
}

func TestEncodeClcDisconnect_Overflow(t *testing.T) {
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeClcDisconnect(buf); err == nil {
		t.Error("expected overflow error on zero-cap buf")
	}
}

// --- EncodeClcStringCmd --------------------------------------------

func TestEncodeClcStringCmd_HappyPath(t *testing.T) {
	cases := []string{
		"prespawn",
		"begin",
		"name \"player\"\n",
		"say hello",
		"x", // one-byte command
	}
	for _, cmd := range cases {
		buf := sizebuf.New(make([]byte, 64))
		if err := EncodeClcStringCmd(buf, cmd); err != nil {
			t.Fatalf("cmd=%q: %v", cmd, err)
		}
		got := buf.Bytes()
		want := append([]byte{protocol.ClcStringCmd}, cmd...)
		want = append(want, 0)
		if !equalBytes(got, want) {
			t.Errorf("cmd=%q: got % x want % x", cmd, got, want)
		}
	}
}

func TestEncodeClcStringCmd_NilBuf(t *testing.T) {
	if err := EncodeClcStringCmd(nil, "kill"); !errors.Is(err, ErrSendNilBuf) {
		t.Errorf("got %v want ErrSendNilBuf", err)
	}
}

func TestEncodeClcStringCmd_Empty(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := EncodeClcStringCmd(buf, ""); !errors.Is(err, ErrSendEmptyString) {
		t.Errorf("got %v want ErrSendEmptyString", err)
	}
	if buf.Len() != 0 {
		t.Errorf("buf should not be written to: len=%d", buf.Len())
	}
}

func TestEncodeClcStringCmd_OpcodeOverflow(t *testing.T) {
	// Zero-cap buf: even the opcode byte cannot fit.
	buf := sizebuf.New(make([]byte, 0))
	if err := EncodeClcStringCmd(buf, "kill"); err == nil {
		t.Error("expected overflow on opcode write")
	}
}

func TestEncodeClcStringCmd_StringOverflow(t *testing.T) {
	// 1-cap buf: opcode fits, the NUL-terminated string does not.
	buf := sizebuf.New(make([]byte, 1))
	if err := EncodeClcStringCmd(buf, "kill"); err == nil {
		t.Error("expected overflow on string write")
	}
}

// --- EncodeClcMove --------------------------------------------------

func TestEncodeClcMove_RoundTrip(t *testing.T) {
	const buttons uint8 = 0b00000011 // attack + jump
	const impulse uint8 = 5
	cmd := server.UserCmd{
		ViewAngles:  [3]float32{12, 45, -30}, // pitch / yaw / roll
		ForwardMove: 320,
		SideMove:    -175,
		UpMove:      0,
		Buttons:     buttons,
		Impulse:     impulse,
	}
	const sendTime float32 = 17.5

	buf := sizebuf.New(make([]byte, 32))
	if err := EncodeClcMove(buf, sendTime, cmd); err != nil {
		t.Fatal(err)
	}

	// Wire size: 1 + 4 + 3*1 + 3*2 + 1 + 1 = 16 bytes.
	if buf.Len() != 16 {
		t.Errorf("wire size: got %d want 16", buf.Len())
	}

	r := msg.NewReader(buf.Bytes())
	if op := r.ReadU8(); op != protocol.ClcMove {
		t.Errorf("opcode: got %d want %d", op, protocol.ClcMove)
	}
	if ts := r.ReadFloat(); ts != sendTime {
		t.Errorf("sendTime: got %v want %v", ts, sendTime)
	}
	// Angles round-trip modulo 1/256-circle quantization (~1.4 deg).
	for axis := 0; axis < 3; axis++ {
		got := r.ReadAngle()
		want := cmd.ViewAngles[axis]
		// WriteAngle uses round-to-nearest in 360/256 steps; the
		// quantization step is 360/256 ~= 1.40625. Allow that as
		// the round-trip tolerance.
		diff := got - want
		if diff < 0 {
			diff = -diff
		}
		if diff > 1.41 {
			t.Errorf("angle[%d]: got %v want %v (diff %v)", axis, got, want, diff)
		}
	}
	if v := r.ReadShort(); v != int(cmd.ForwardMove) {
		t.Errorf("forwardmove: got %d want %d", v, int(cmd.ForwardMove))
	}
	if v := r.ReadShort(); v != int(cmd.SideMove) {
		t.Errorf("sidemove: got %d want %d", v, int(cmd.SideMove))
	}
	if v := r.ReadShort(); v != int(cmd.UpMove) {
		t.Errorf("upmove: got %d want %d", v, int(cmd.UpMove))
	}
	if v := r.ReadU8(); v != int(buttons) {
		t.Errorf("buttons: got %d want %d", v, buttons)
	}
	if v := r.ReadU8(); v != int(impulse) {
		t.Errorf("impulse: got %d want %d", v, impulse)
	}
	if r.Bad() {
		t.Error("reader hit EOF mid-decode")
	}
}

func TestEncodeClcMove_ByteLayout(t *testing.T) {
	// Hand-roll a known UserCmd and compare the wire bytes verbatim,
	// without going through the symmetric msg.Reader.
	const buttons uint8 = 0x0A
	const impulse uint8 = 0xFE
	cmd := server.UserCmd{
		ViewAngles:  [3]float32{0, 0, 0}, // all three angles -> byte 0
		ForwardMove: 1,
		SideMove:    2,
		UpMove:      3,
		Buttons:     buttons,
		Impulse:     impulse,
	}
	const sendTime float32 = 0

	buf := sizebuf.New(make([]byte, 16))
	if err := EncodeClcMove(buf, sendTime, cmd); err != nil {
		t.Fatal(err)
	}

	want := make([]byte, 0, 16)
	want = append(want, protocol.ClcMove)
	// float32(0) -> 4 zero bytes
	want = append(want, 0, 0, 0, 0)
	// angles 0, 0, 0 -> 0, 0, 0
	want = append(want, 0, 0, 0)
	// shorts 1, 2, 3 (little-endian)
	var shortBuf [2]byte
	for _, v := range []int16{1, 2, 3} {
		binary.LittleEndian.PutUint16(shortBuf[:], uint16(v))
		want = append(want, shortBuf[0], shortBuf[1])
	}
	want = append(want, buttons, impulse)

	got := buf.Bytes()
	if !equalBytes(got, want) {
		t.Errorf("wire bytes:\n got % x\nwant % x", got, want)
	}
}

func TestEncodeClcMove_FloatSendTime(t *testing.T) {
	// Verify the float field is little-endian IEEE-754.
	cmd := server.UserCmd{}
	const sendTime float32 = 3.14159
	buf := sizebuf.New(make([]byte, 16))
	if err := EncodeClcMove(buf, sendTime, cmd); err != nil {
		t.Fatal(err)
	}
	bits := binary.LittleEndian.Uint32(buf.Bytes()[1:5])
	if got := math.Float32frombits(bits); got != sendTime {
		t.Errorf("float decode: got %v want %v", got, sendTime)
	}
}

func TestEncodeClcMove_NilBuf(t *testing.T) {
	err := EncodeClcMove(nil, 0, server.UserCmd{})
	if !errors.Is(err, ErrSendNilBuf) {
		t.Errorf("got %v want ErrSendNilBuf", err)
	}
}

// Each msg.Write* failure inside EncodeClcMove must surface. The
// sizebuf returns ErrSizeBufOverflow once the cumulative write
// would exceed cap; sizing the buf one byte short of each field
// boundary forces the corresponding Write* to be the failing arm.
//
// Cumulative offsets in the wire layout:
//
//	0  opcode    (writes 1, cap 0  -> opcode fails)
//	1  sendTime  (writes 4, cap 1  -> sendTime fails)
//	5  angle0    (writes 1, cap 5  -> angle0 fails)
//	6  angle1    (writes 1, cap 6  -> angle1 fails)
//	7  angle2    (writes 1, cap 7  -> angle2 fails)
//	8  forward   (writes 2, cap 8  -> forward fails)
//	10 side      (writes 2, cap 10 -> side fails)
//	12 up        (writes 2, cap 12 -> up fails)
//	14 buttons   (writes 1, cap 14 -> buttons fails)
//	15 impulse   (writes 1, cap 15 -> impulse fails)
func TestEncodeClcMove_OverflowAtEveryField(t *testing.T) {
	cmd := server.UserCmd{
		ViewAngles:  [3]float32{1, 2, 3},
		ForwardMove: 10,
		SideMove:    20,
		UpMove:      30,
		Buttons:     0xAB,
		Impulse:     0xCD,
	}
	for _, n := range []int{0, 1, 5, 6, 7, 8, 10, 12, 14, 15} {
		buf := sizebuf.New(make([]byte, n))
		err := EncodeClcMove(buf, 1.0, cmd)
		if err == nil {
			t.Errorf("cap=%d: expected overflow", n)
		}
	}
	// And the 16-byte buf succeeds (cap == exact size).
	buf := sizebuf.New(make([]byte, 16))
	if err := EncodeClcMove(buf, 1.0, cmd); err != nil {
		t.Errorf("cap=16: got %v, want success", err)
	}
}

// --- helpers --------------------------------------------------------

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
