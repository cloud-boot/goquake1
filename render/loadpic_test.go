// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"encoding/binary"
	"errors"
	"testing"
)

func makePicLump(w, h int, fill byte, padding int) []byte {
	out := make([]byte, PicHeaderSize+w*h+padding)
	binary.LittleEndian.PutUint32(out[0:4], uint32(w))
	binary.LittleEndian.PutUint32(out[4:8], uint32(h))
	for i := PicHeaderSize; i < PicHeaderSize+w*h; i++ {
		out[i] = fill
	}
	return out
}

// ----- ParsePic happy paths ----------------------------------------

func TestParsePic_Happy(t *testing.T) {
	lump := makePicLump(8, 4, 0x42, 0)
	pic, err := ParsePic(lump)
	if err != nil {
		t.Fatalf("ParsePic: %v", err)
	}
	if pic.Width != 8 || pic.Height != 4 {
		t.Fatalf("dim = %dx%d want 8x4", pic.Width, pic.Height)
	}
	if len(pic.Pixels) != 32 {
		t.Fatalf("pixels len = %d want 32", len(pic.Pixels))
	}
	for i, p := range pic.Pixels {
		if p != 0x42 {
			t.Fatalf("pixels[%d] = %#x want 0x42", i, p)
		}
	}
}

func TestParsePic_TrailingPaddingIgnored(t *testing.T) {
	lump := makePicLump(4, 4, 0x77, 16)
	pic, err := ParsePic(lump)
	if err != nil {
		t.Fatalf("ParsePic with padding: %v", err)
	}
	if len(pic.Pixels) != 16 {
		t.Fatalf("pixels len = %d want 16 (padding dropped)", len(pic.Pixels))
	}
}

// ----- ParsePic error paths ----------------------------------------

func TestParsePic_LumpShort(t *testing.T) {
	for n := 0; n < PicHeaderSize; n++ {
		_, err := ParsePic(make([]byte, n))
		if !errors.Is(err, ErrPicLumpShort) {
			t.Fatalf("ParsePic(%d-byte lump) err = %v want ErrPicLumpShort", n, err)
		}
	}
}

func TestParsePic_DimRange(t *testing.T) {
	cases := []struct {
		w, h  int32
		label string
	}{
		{0, 4, "width zero"},
		{4, 0, "height zero"},
		{-1, 4, "width negative"},
		{4, -1, "height negative"},
		{PicMaxDim + 1, 4, "width above cap"},
		{4, PicMaxDim + 1, "height above cap"},
	}
	for _, c := range cases {
		buf := make([]byte, PicHeaderSize)
		binary.LittleEndian.PutUint32(buf[0:4], uint32(c.w))
		binary.LittleEndian.PutUint32(buf[4:8], uint32(c.h))
		_, err := ParsePic(buf)
		if !errors.Is(err, ErrPicDimRange) {
			t.Fatalf("ParsePic(%s) err = %v want ErrPicDimRange", c.label, err)
		}
	}
}

func TestParsePic_BodyTruncated(t *testing.T) {
	// Header says 16x16 = 256 bytes; supply only 100.
	buf := make([]byte, PicHeaderSize+100)
	binary.LittleEndian.PutUint32(buf[0:4], 16)
	binary.LittleEndian.PutUint32(buf[4:8], 16)
	_, err := ParsePic(buf)
	if !errors.Is(err, ErrPicLumpTrunc) {
		t.Fatalf("ParsePic body-truncated err = %v want ErrPicLumpTrunc", err)
	}
}

// ----- EncodePic ---------------------------------------------------

func TestEncodePic_Happy(t *testing.T) {
	pic := &Pic{Width: 4, Height: 3, Pixels: make([]byte, 12)}
	for i := range pic.Pixels {
		pic.Pixels[i] = byte(i + 1)
	}
	wire, err := EncodePic(pic)
	if err != nil {
		t.Fatalf("EncodePic: %v", err)
	}
	if len(wire) != PicHeaderSize+12 {
		t.Fatalf("wire len = %d want %d", len(wire), PicHeaderSize+12)
	}
	if w := binary.LittleEndian.Uint32(wire[0:4]); w != 4 {
		t.Fatalf("encoded width = %d want 4", w)
	}
	if h := binary.LittleEndian.Uint32(wire[4:8]); h != 3 {
		t.Fatalf("encoded height = %d want 3", h)
	}
	for i := 0; i < 12; i++ {
		if wire[PicHeaderSize+i] != byte(i+1) {
			t.Fatalf("wire pixel[%d] = %#x want %#x", i, wire[PicHeaderSize+i], byte(i+1))
		}
	}
}

func TestEncodePic_NilSrc(t *testing.T) {
	_, err := EncodePic(nil)
	if !errors.Is(err, ErrPicNilSrc) {
		t.Fatalf("err = %v want ErrPicNilSrc", err)
	}
}

func TestEncodePic_BadShape(t *testing.T) {
	cases := []*Pic{
		{Width: 0, Height: 4, Pixels: make([]byte, 0)},
		{Width: 4, Height: 0, Pixels: make([]byte, 0)},
		{Width: -1, Height: 4, Pixels: make([]byte, 0)},
		{Width: 4, Height: 4, Pixels: make([]byte, 10)}, // 10 != 4*4
	}
	for i, c := range cases {
		_, err := EncodePic(c)
		if !errors.Is(err, ErrPicShape) {
			t.Fatalf("case %d err = %v want ErrPicShape", i, err)
		}
	}
}

// ----- Roundtrip ---------------------------------------------------

func TestPicRoundtrip(t *testing.T) {
	orig := &Pic{Width: 7, Height: 5, Pixels: make([]byte, 35)}
	for i := range orig.Pixels {
		orig.Pixels[i] = byte(i ^ 0xAA)
	}
	wire, err := EncodePic(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := ParsePic(wire)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Width != orig.Width || back.Height != orig.Height {
		t.Fatalf("roundtrip dim drift: %dx%d -> %dx%d",
			orig.Width, orig.Height, back.Width, back.Height)
	}
	for i, p := range back.Pixels {
		if p != orig.Pixels[i] {
			t.Fatalf("roundtrip pixel[%d] drift: %#x -> %#x", i, orig.Pixels[i], p)
		}
	}
}
