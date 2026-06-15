// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qstr

import "testing"

// --- Atoi --------------------------------------------------------------------

func TestAtoi(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		// Decimal, signed, edge.
		{"0", 0},
		{"7", 7},
		{"123", 123},
		{"-42", -42},
		{"", 0},
		{"-", 0},
		// Decimal that stops at garbage tail (tyrquake returns prefix).
		{"42abc", 42},
		{"-9xx", -9},
		// Hex, lower- and upper-case, both alphabetical and decimal nibbles.
		{"0x0", 0},
		{"0X10", 16},
		{"0xff", 255},
		{"0xFF", 255},
		{"-0xa", -10},
		// Hex that stops at garbage tail.
		{"0xfeZ", 0xfe},
		// Hex with no digits after prefix.
		{"0x", 0},
		// Quoted-character literal.
		{"'A", 65},
		{"-'A", -65},
		// Unterminated `'` (Go bound-checks; C is UB) -> 0.
		{"'", 0},
		// String starting with garbage -> 0 (loop returns immediately).
		{"abc", 0},
		// Leading-zero-not-x stays in the decimal path (yields 0 then
		// keeps going as decimal digit, so "012" is parsed as 12).
		{"012", 12},
	}
	for _, c := range cases {
		if got := Atoi(c.in); got != c.want {
			t.Errorf("Atoi(%q): got %d want %d", c.in, got, c.want)
		}
	}
}

// Hex parser must walk to end-of-string without a non-hex terminator.
// Covered above via "0xff" / "0xFF"; this case pins the
// negative-sign + hex-to-EOF path explicitly.
func TestAtoiHexToEOF(t *testing.T) {
	if got := Atoi("-0x100"); got != -256 {
		t.Errorf("Atoi(-0x100): got %d want -256", got)
	}
}

// --- Atof --------------------------------------------------------------------

func feq(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= 1e-5
}

func TestAtof(t *testing.T) {
	cases := []struct {
		in   string
		want float32
	}{
		// Decimal, no fraction.
		{"0", 0},
		{"7", 7},
		{"-42", -42},
		{"", 0},
		{"-", 0},
		// Decimal with fraction.
		{"1.5", 1.5},
		{"-2.25", -2.25},
		{"0.125", 0.125},
		// Garbage tail truncates parse.
		{"3.14abc", 3.14},
		{"42xx", 42},
		// Exponent notation is NOT honoured (tyrquake breaks on 'e').
		{"1e5", 1},
		// Hex path: integer-only since tyrquake's float-hex has no `.`.
		{"0x10", 16},
		{"0XFF", 255},
		{"-0xa", -10},
		{"0xfeZ", 254},
		{"0x", 0},
		// Quoted-character literal.
		{"'A", 65},
		{"-'A", -65},
		{"'", 0},
		// Pure-garbage string -> 0.
		{"abc", 0},
		// Trailing '.' with no fractional digits: decimal=0, total=0
		// (since '.' itself doesn't increment total), so the
		// `while total > decimal` loop is a no-op -> value unchanged.
		{"3.", 3},
	}
	for _, c := range cases {
		if got := Atof(c.in); !feq(got, c.want) {
			t.Errorf("Atof(%q): got %v want %v", c.in, got, c.want)
		}
	}
}

// Hex-to-EOF + sign path for Atof.
func TestAtofHexToEOF(t *testing.T) {
	if got := Atof("-0x100"); !feq(got, -256) {
		t.Errorf("Atof(-0x100): got %v want -256", got)
	}
}

// --- StrBuf ------------------------------------------------------------------

// StrBuf returns 8 distinct backing arrays, cycling through them with
// pre-increment indexing (slot 1 is returned first).
func TestStrBufRotation(t *testing.T) {
	// Reset the rotating index so this test does not depend on
	// previously-run package tests.
	strBufIndex = 0

	first := StrBuf()
	if len(first) != StrBufLen {
		t.Fatalf("first StrBuf len: got %d want %d", len(first), StrBufLen)
	}

	// Collect 16 buffer pointers; expect the second 8 to alias the
	// first 8 (slot 1, 2, ..., 7, 0, 1, 2, ...).
	bufs := make([][]byte, 16)
	bufs[0] = first
	for i := 1; i < 16; i++ {
		bufs[i] = StrBuf()
		if len(bufs[i]) != StrBufLen {
			t.Fatalf("StrBuf #%d len: got %d want %d", i, len(bufs[i]), StrBufLen)
		}
	}

	// Pairs (i, i+8) must alias because the index mask is 7.
	for i := 0; i < 8; i++ {
		if &bufs[i][0] != &bufs[i+8][0] {
			t.Errorf("StrBuf rotation: slot %d and slot %d should alias", i, i+8)
		}
	}
	// Adjacent slots within one cycle must be distinct.
	for i := 0; i < 7; i++ {
		if &bufs[i][0] == &bufs[i+1][0] {
			t.Errorf("StrBuf rotation: slot %d and slot %d should NOT alias", i, i+1)
		}
	}
}
