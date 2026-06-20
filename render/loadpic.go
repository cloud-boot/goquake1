// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"encoding/binary"
	"errors"
)

// PicHeaderSize is the on-disk byte count of the dpic8_t header:
// two little-endian int32s for width + height.
const PicHeaderSize = 8

// PicMaxDim is a sanity cap on per-axis lump dimensions; the largest
// vanilla Q1 wad pic is conchars at 128x128 and bigpic at 320x200,
// so 4096 leaves room for any reasonable mod without admitting bytes
// that would overflow int arithmetic on a corrupt lump.
const PicMaxDim = 4096

var (
	ErrPicLumpShort  = errors.New("render: pic lump shorter than 8-byte header")
	ErrPicLumpTrunc  = errors.New("render: pic lump body truncated (width*height > remaining bytes)")
	ErrPicDimRange   = errors.New("render: pic dimensions out of range")
)

// ParsePic decodes a WAD `pic` lump (the dpic8_t format) into a
// *Pic. Wire shape: two little-endian int32s (width, height)
// followed by width*height palette-indexed bytes.
//
// Returns the parsed pic + nil on success. Errors:
//
//	ErrPicLumpShort  len(lump) < PicHeaderSize
//	ErrPicDimRange   width or height <= 0 or > PicMaxDim
//	ErrPicLumpTrunc  width*height > len(lump) - 8
//
// Trailing bytes past width*height are ignored (some lumps include
// padding for 4-byte alignment).
func ParsePic(lump []byte) (*Pic, error) {
	if len(lump) < PicHeaderSize {
		return nil, ErrPicLumpShort
	}
	w := int32(binary.LittleEndian.Uint32(lump[0:4]))
	h := int32(binary.LittleEndian.Uint32(lump[4:8]))
	if w <= 0 || h <= 0 || w > PicMaxDim || h > PicMaxDim {
		return nil, ErrPicDimRange
	}
	need := int(w) * int(h)
	if need > len(lump)-PicHeaderSize {
		return nil, ErrPicLumpTrunc
	}
	pixels := make([]byte, need)
	copy(pixels, lump[PicHeaderSize:PicHeaderSize+need])
	return &Pic{
		Width:  int(w),
		Height: int(h),
		Pixels: pixels,
	}, nil
}

// EncodePic serializes a *Pic into the dpic8_t wire format. The
// inverse of ParsePic; useful for tests + for round-tripping pic
// data through the on-disk representation.
//
// Returns ErrPicNilSrc if pic is nil; ErrPicShape if len(pic.Pixels)
// != pic.Width*pic.Height.
func EncodePic(pic *Pic) ([]byte, error) {
	if pic == nil {
		return nil, ErrPicNilSrc
	}
	if pic.Width <= 0 || pic.Height <= 0 || len(pic.Pixels) != pic.Width*pic.Height {
		return nil, ErrPicShape
	}
	out := make([]byte, PicHeaderSize+len(pic.Pixels))
	binary.LittleEndian.PutUint32(out[0:4], uint32(pic.Width))
	binary.LittleEndian.PutUint32(out[4:8], uint32(pic.Height))
	copy(out[PicHeaderSize:], pic.Pixels)
	return out, nil
}
