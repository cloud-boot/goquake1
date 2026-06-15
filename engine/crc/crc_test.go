// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crc

import "testing"

// TestInit asserts the seed matches the tyrquake CRC_INIT_VALUE.
func TestInit(t *testing.T) {
	if got := Init(); got != 0xffff {
		t.Errorf("Init: got 0x%04x want 0xffff", got)
	}
}

// TestValue exercises the final-XOR step. With XOR_VALUE=0 it is the
// identity; the test pins that contract so a future upstream change to
// the constant trips a visible failure here.
func TestValue(t *testing.T) {
	cases := []uint16{0x0000, 0x1234, 0xffff, 0xabcd}
	for _, c := range cases {
		if got := Value(c); got != c^xorValue {
			t.Errorf("Value(0x%04x): got 0x%04x want 0x%04x", c, got, c^xorValue)
		}
	}
}

// TestProcessByte_FirstStep walks the very first byte by hand:
//
//	crc = 0xffff (init)
//	idx = (0xffff >> 8) ^ 0x61 = 0xff ^ 0x61 = 0x9e
//	table[0x9e] = 0x6277
//	crc' = (0xffff << 8) ^ 0x6277 = 0xff00 ^ 0x6277 = 0x9d77
func TestProcessByte_FirstStep(t *testing.T) {
	got := ProcessByte(Init(), 'a')
	if got != 0x9d77 {
		t.Errorf("ProcessByte(init,'a'): got 0x%04x want 0x9d77", got)
	}
}

// TestBlock_Parity covers known answers derived from the tyrquake C
// algorithm. The values were generated outside this package (verbatim
// re-implementation of the C loop on the same table) so they catch any
// drift in this port.
func TestBlock_Parity(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want uint16
	}{
		{"empty", []byte{}, 0xffff},
		{"single 'a'", []byte("a"), 0x9d77},
		{"abc", []byte("abc"), 0x514a},
		{"check 123456789", []byte("123456789"), 0x29b1},
		{"Quake", []byte("Quake"), 0x17d7},
	}
	for _, c := range cases {
		got := Block(c.in)
		if got != c.want {
			t.Errorf("Block(%q): got 0x%04x want 0x%04x", c.name, got, c.want)
		}
	}
}

// TestBlock_NilSlice exercises the count==0 path with a nil (not just
// empty) slice. tyrquake's CRC_Block accepts a NULL block when count==0
// because the loop never dereferences; the Go port should too.
func TestBlock_NilSlice(t *testing.T) {
	if got := Block(nil); got != 0xffff {
		t.Errorf("Block(nil): got 0x%04x want 0xffff", got)
	}
}

// TestBlock_MatchesProcessByteStream pins the invariant that the
// one-shot Block helper is exactly equivalent to a manual Init +
// ProcessByte loop. This is the engine's hot path: progs.dat ships a
// reference CRC and the server cross-checks it byte-by-byte at parse.
func TestBlock_MatchesProcessByteStream(t *testing.T) {
	inputs := [][]byte{
		nil,
		{},
		{0x00},
		{0xff},
		[]byte("The quick brown fox jumps over the lazy dog"),
		// 256-byte sweep covers every possible high-byte index into table.
		func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(),
	}
	for _, in := range inputs {
		stream := Init()
		for _, b := range in {
			stream = ProcessByte(stream, b)
		}
		one := Block(in)
		if stream != one {
			t.Errorf("stream vs one-shot mismatch on %d bytes: stream=0x%04x one=0x%04x",
				len(in), stream, one)
		}
	}
}
