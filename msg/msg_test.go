// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package msg

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/sizebuf"
)

func newBuf(cap int) *sizebuf.Buffer { return sizebuf.New(make([]byte, cap)) }

// --- write side --------------------------------------------------------------

func TestWriteByteChar_Encoding(t *testing.T) {
	b := newBuf(8)
	if err := WriteChar(b, -1); err != nil {
		t.Fatal(err)
	}
	if err := WriteByte(b, 0xFE); err != nil {
		t.Fatal(err)
	}
	want := []byte{0xFF, 0xFE}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteShortLong_LittleEndian(t *testing.T) {
	b := newBuf(16)
	if err := WriteShort(b, -1); err != nil {
		t.Fatal(err)
	}
	if err := WriteLong(b, 0x12345678); err != nil {
		t.Fatal(err)
	}
	want := []byte{0xFF, 0xFF, 0x78, 0x56, 0x34, 0x12}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteFloat_RoundTrip(t *testing.T) {
	for _, f := range []float32{0, 1.5, -3.25, math.Pi, math.MaxFloat32, math.SmallestNonzeroFloat32} {
		b := newBuf(8)
		if err := WriteFloat(b, f); err != nil {
			t.Fatal(err)
		}
		r := NewReader(b.Bytes())
		got := r.ReadFloat()
		if got != f {
			t.Errorf("%v -> %v", f, got)
		}
	}
}

func TestWriteString_NULTerminated(t *testing.T) {
	b := newBuf(16)
	if err := WriteString(b, "hello"); err != nil {
		t.Fatal(err)
	}
	want := []byte("hello\x00")
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteString_Empty(t *testing.T) {
	b := newBuf(4)
	if err := WriteString(b, ""); err != nil {
		t.Fatal(err)
	}
	want := []byte{0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteCoord_FixedPoint(t *testing.T) {
	b := newBuf(4)
	if err := WriteCoord(b, 1.0); err != nil {
		t.Fatal(err)
	}
	// 1.0 * 8 = 8 as int16 = 0x0008 little-endian.
	want := []byte{0x08, 0x00}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteAngle_ByteScaled(t *testing.T) {
	b := newBuf(4)
	if err := WriteAngle(b, 0); err != nil {
		t.Fatal(err)
	}
	// 0 -> 0; 90 -> 64; 360 -> 0 (mod 256).
	if err := WriteAngle(b, 90); err != nil {
		t.Fatal(err)
	}
	if err := WriteAngle(b, 360); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 64, 0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteAngle16_ShortScaled(t *testing.T) {
	b := newBuf(4)
	if err := WriteAngle16(b, 0); err != nil {
		t.Fatal(err)
	}
	// 0 maps to 0.
	want := []byte{0, 0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("got %v want %v", b.Bytes(), want)
	}
}

func TestWriteControlHeader(t *testing.T) {
	b := newBuf(16)
	// Reserve 4 header bytes + 6 payload bytes = cursize 10.
	if _, err := b.GetSpace(4); err != nil {
		t.Fatal(err)
	}
	if err := b.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := WriteControlHeader(b); err != nil {
		t.Fatal(err)
	}
	// Expect BIG-endian (NETFLAG_CTL | 10) at offset 0.
	want := uint32(NETFLAG_CTL) | uint32(b.Len())
	got := uint32(b.Bytes()[0])<<24 | uint32(b.Bytes()[1])<<16 | uint32(b.Bytes()[2])<<8 | uint32(b.Bytes()[3])
	if got != want {
		t.Errorf("got %#x want %#x", got, want)
	}
}

func TestWriteControlHeader_NoSpace(t *testing.T) {
	b := newBuf(2) // less than 4 -> can't even hold the header
	if err := WriteControlHeader(b); !errors.Is(err, ErrControlHeaderNoSpace) {
		t.Errorf("got %v want ErrControlHeaderNoSpace", err)
	}
}

func TestWrite_OverflowPropagates(t *testing.T) {
	// All Write* surface the underlying GetSpace overflow.
	for name, write := range map[string]func(*sizebuf.Buffer) error{
		"char":  func(b *sizebuf.Buffer) error { return WriteChar(b, 0) },
		"byte":  func(b *sizebuf.Buffer) error { return WriteByte(b, 0) },
		"short": func(b *sizebuf.Buffer) error { return WriteShort(b, 0) },
		"long":  func(b *sizebuf.Buffer) error { return WriteLong(b, 0) },
		"float": func(b *sizebuf.Buffer) error { return WriteFloat(b, 0) },
		"str":   func(b *sizebuf.Buffer) error { return WriteString(b, "long enough") },
		"coord": func(b *sizebuf.Buffer) error { return WriteCoord(b, 0) },
		"angle": func(b *sizebuf.Buffer) error { return WriteAngle(b, 0) },
		"a16":   func(b *sizebuf.Buffer) error { return WriteAngle16(b, 0) },
	} {
		b := newBuf(0) // cap=0, ANY write must overflow
		if err := write(b); err == nil {
			t.Errorf("%s on cap=0 buf: expected error", name)
		}
	}
}

// --- read side ---------------------------------------------------------------

func TestReader_BasicReadBack(t *testing.T) {
	b := newBuf(64)
	_ = WriteByte(b, 1)
	_ = WriteChar(b, -1)
	_ = WriteShort(b, 0x1234)
	_ = WriteLong(b, 0x12345678)
	_ = WriteFloat(b, math.Pi)
	_ = WriteString(b, "hi")
	r := NewReader(b.Bytes())
	r.Begin()
	if r.ReadByte() != 1 {
		t.Error("ReadByte")
	}
	if r.ReadChar() != -1 {
		t.Error("ReadChar")
	}
	if r.ReadShort() != 0x1234 {
		t.Error("ReadShort")
	}
	if r.ReadLong() != 0x12345678 {
		t.Error("ReadLong")
	}
	if f := r.ReadFloat(); f != math.Pi {
		t.Errorf("ReadFloat: %v", f)
	}
	if s := r.ReadString(); s != "hi" {
		t.Errorf("ReadString: %q", s)
	}
	if r.Bad() {
		t.Error("Bad should be false after clean reads")
	}
	if r.Pos() != b.Len() {
		t.Errorf("Pos: got %d want %d", r.Pos(), b.Len())
	}
}

func TestReader_EOFEachType(t *testing.T) {
	r := NewReader(nil)
	if r.ReadByte() != -1 || !r.Bad() {
		t.Error("ReadByte EOF")
	}
	r = NewReader(nil)
	if r.ReadChar() != -1 || !r.Bad() {
		t.Error("ReadChar EOF")
	}
	r = NewReader(nil)
	if r.ReadShort() != -1 || !r.Bad() {
		t.Error("ReadShort EOF")
	}
	r = NewReader(nil)
	if r.ReadLong() != -1 || !r.Bad() {
		t.Error("ReadLong EOF")
	}
	r = NewReader(nil)
	if f := r.ReadFloat(); f != 0 || !r.Bad() {
		t.Errorf("ReadFloat EOF: %v %v", f, r.Bad())
	}
}

func TestReader_StringUnterminated(t *testing.T) {
	r := NewReader([]byte("hello-no-nul"))
	got := r.ReadString()
	if got != "hello-no-nul" {
		t.Errorf("got %q", got)
	}
	if !r.Bad() {
		t.Error("Bad should be true after unterminated string")
	}
}

func TestReader_StringEmpty(t *testing.T) {
	r := NewReader([]byte{0, 'x'})
	if s := r.ReadString(); s != "" {
		t.Errorf("empty: got %q", s)
	}
	if r.Pos() != 1 {
		t.Errorf("Pos after empty string: %d", r.Pos())
	}
	if r.ReadByte() != 'x' {
		t.Error("ReadByte after empty string failed")
	}
}

func TestReader_CoordAngleRoundTrip(t *testing.T) {
	b := newBuf(16)
	_ = WriteCoord(b, 12.5)
	_ = WriteAngle(b, 90)
	_ = WriteAngle16(b, 180)
	r := NewReader(b.Bytes())
	if c := r.ReadCoord(); c != 12.5 {
		t.Errorf("Coord round-trip: %v", c)
	}
	// Angle round-trip is lossy due to byte quantisation; check approximate.
	if a := r.ReadAngle(); a < 89 || a > 91 {
		t.Errorf("Angle round-trip: %v", a)
	}
	// tyrquake quirk preserved: WriteAngle16(180) stores 0x8000 which
	// ReadShort treats as int16 = -32768, then ReadAngle16 yields -180.
	// The engine consumes these via AngleMod so the sign asymmetry is
	// invisible upstream; we pin the byte-equal upstream behaviour here.
	if a := r.ReadAngle16(); a < -180.01 || a > -179.99 {
		t.Errorf("Angle16 round-trip: got %v want ~-180 (upstream parity)", a)
	}
}

func TestReader_ControlHeader(t *testing.T) {
	// Build buffer with control header at offset 0 + 6 bytes payload.
	b := newBuf(16)
	if _, err := b.GetSpace(4); err != nil {
		t.Fatal(err)
	}
	if err := b.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := WriteControlHeader(b); err != nil {
		t.Fatal(err)
	}
	r := NewReader(b.Bytes())
	hdr := r.ReadControlHeader()
	want := int32(uint32(NETFLAG_CTL) | uint32(b.Len()))
	if hdr != want {
		t.Errorf("hdr: got %#x want %#x", hdr, want)
	}
}

func TestReader_ControlHeaderEOF(t *testing.T) {
	r := NewReader([]byte{1, 2})
	if h := r.ReadControlHeader(); h != -1 || !r.Bad() {
		t.Errorf("EOF: got %d bad=%v", h, r.Bad())
	}
}
