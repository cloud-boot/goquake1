// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package entparse

import (
	"errors"
	"testing"
)

func TestParseFloat_HappyPath(t *testing.T) {
	cases := []struct {
		in   string
		want float32
	}{
		{"0", 0},
		{"1", 1},
		{"-1", -1},
		{"1.5", 1.5},
		{"0.5", 0.5},
		{"-0.5", -0.5},
		{"1e3", 1000},
	}
	for _, tc := range cases {
		got, err := ParseFloat(tc.in)
		if err != nil {
			t.Errorf("ParseFloat(%q) err = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseFloat(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseFloat_WhitespaceTrimmed(t *testing.T) {
	got, err := ParseFloat(" 1.5 ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 1.5 {
		t.Fatalf("got %v, want 1.5", got)
	}
}

func TestParseFloat_EmptyIsZero(t *testing.T) {
	got, err := ParseFloat("")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

func TestParseFloat_WhitespaceOnlyIsZero(t *testing.T) {
	got, err := ParseFloat("   ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

func TestParseFloat_GarbageErrors(t *testing.T) {
	_, err := ParseFloat("garbage")
	if !errors.Is(err, ErrBadFloat) {
		t.Fatalf("err = %v, want ErrBadFloat", err)
	}
}

func TestParseFloat_TrailingGarbageErrors(t *testing.T) {
	_, err := ParseFloat("1.5garbage")
	if !errors.Is(err, ErrBadFloat) {
		t.Fatalf("err = %v, want ErrBadFloat", err)
	}
}

func TestParseVec3_HappyPath(t *testing.T) {
	cases := []struct {
		in   string
		want [3]float32
	}{
		{"0 0 0", [3]float32{0, 0, 0}},
		{"1 2 3", [3]float32{1, 2, 3}},
		{"-1.5 2.5 -3.5", [3]float32{-1.5, 2.5, -3.5}},
		{"1   2   3", [3]float32{1, 2, 3}},
	}
	for _, tc := range cases {
		got, err := ParseVec3(tc.in)
		if err != nil {
			t.Errorf("ParseVec3(%q) err = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseVec3(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseVec3_EmptyIsZero(t *testing.T) {
	got, err := ParseVec3("")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != ([3]float32{}) {
		t.Fatalf("got %v, want zero vector", got)
	}
}

func TestParseVec3_WhitespaceOnlyIsZero(t *testing.T) {
	got, err := ParseVec3("   ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != ([3]float32{}) {
		t.Fatalf("got %v, want zero vector", got)
	}
}

func TestParseVec3_TooFewAxes(t *testing.T) {
	_, err := ParseVec3("1 2")
	if !errors.Is(err, ErrBadVec3) {
		t.Fatalf("err = %v, want ErrBadVec3", err)
	}
}

func TestParseVec3_TooManyAxes(t *testing.T) {
	_, err := ParseVec3("1 2 3 4")
	if !errors.Is(err, ErrBadVec3) {
		t.Fatalf("err = %v, want ErrBadVec3", err)
	}
}

func TestParseVec3_GarbageAxis(t *testing.T) {
	_, err := ParseVec3("1 2 garbage")
	if !errors.Is(err, ErrBadVec3) {
		t.Fatalf("err = %v, want ErrBadVec3", err)
	}
}

func TestParseEntity_HappyPath(t *testing.T) {
	cases := []struct {
		in   string
		want int32
	}{
		{"0", 0},
		{"1", 1},
		{"100", 100},
		{"-5", -5},
	}
	for _, tc := range cases {
		got, err := ParseEntity(tc.in)
		if err != nil {
			t.Errorf("ParseEntity(%q) err = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseEntity(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseEntity_EmptyIsZero(t *testing.T) {
	got, err := ParseEntity("")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

func TestParseEntity_WhitespaceOnlyIsZero(t *testing.T) {
	got, err := ParseEntity("   ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

func TestParseEntity_GarbageErrors(t *testing.T) {
	_, err := ParseEntity("garbage")
	if !errors.Is(err, ErrBadEntity) {
		t.Fatalf("err = %v, want ErrBadEntity", err)
	}
}

func TestParseString_Verbatim(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo", "foo"},
		{"", ""},
		{"  spaces preserved  ", "  spaces preserved  "},
	}
	for _, tc := range cases {
		got := ParseString(tc.in)
		if got != tc.want {
			t.Errorf("ParseString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFieldType_ConstantsMatchEtypeOrder(t *testing.T) {
	// Pin the iota order so a future progs-runtime layer can rely on
	// these constants matching the QC etype_t values verbatim.
	// tyrquake: include/pr_comp.h.
	cases := []struct {
		got  FieldType
		want int
	}{
		{FieldTypeVoid, 0},
		{FieldTypeString, 1},
		{FieldTypeFloat, 2},
		{FieldTypeVector, 3},
		{FieldTypeEntity, 4},
		{FieldTypeField, 5},
		{FieldTypeFunction, 6},
		{FieldTypePointer, 7},
	}
	for _, tc := range cases {
		if int(tc.got) != tc.want {
			t.Errorf("FieldType %v = %d, want %d", tc.got, int(tc.got), tc.want)
		}
	}
}
