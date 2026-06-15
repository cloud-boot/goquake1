// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package spr

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

// buildSpec wires a synthesised .spr from typed frame specs.
type singleSpec struct {
	originX, originY, w, h int32
	bitmap                 []byte
}

type groupSpec struct {
	intervals []float32
	frames    []singleSpec
}

type frameSpec struct {
	kind   int32 // FrameSingle or FrameGroup (or invalid for negative tests)
	single singleSpec
	group  groupSpec
}

type buildSpec struct {
	ident   uint32 // 0 -> IDSpriteHeader
	version int32  // 0 -> Version
	type_   int32
	width   int32
	height  int32
	frames  []frameSpec
}

func putU32(b *bytes.Buffer, v uint32)   { _ = binary.Write(b, binary.LittleEndian, v) }
func putI32(b *bytes.Buffer, v int32)    { _ = binary.Write(b, binary.LittleEndian, v) }
func putF32(b *bytes.Buffer, v float32)  { _ = binary.Write(b, binary.LittleEndian, v) }

func encodeSingle(b *bytes.Buffer, s singleSpec) {
	putI32(b, s.originX)
	putI32(b, s.originY)
	putI32(b, s.w)
	putI32(b, s.h)
	b.Write(s.bitmap)
}

func build(s buildSpec) ([]byte, int64) {
	ident := s.ident
	if ident == 0 {
		ident = IDSpriteHeader
	}
	ver := s.version
	if ver == 0 {
		ver = Version
	}
	buf := &bytes.Buffer{}
	putU32(buf, ident)
	putI32(buf, ver)
	putI32(buf, s.type_)
	putF32(buf, 16.0) // bounding_radius
	putI32(buf, s.width)
	putI32(buf, s.height)
	putI32(buf, int32(len(s.frames)))
	putF32(buf, 0)  // beam length
	putI32(buf, 0)  // sync type
	for _, f := range s.frames {
		putI32(buf, f.kind)
		switch f.kind {
		case FrameSingle:
			encodeSingle(buf, f.single)
		case FrameGroup:
			putI32(buf, int32(len(f.group.intervals)))
			for _, iv := range f.group.intervals {
				putF32(buf, iv)
			}
			for _, sf := range f.group.frames {
				encodeSingle(buf, sf)
			}
		}
	}
	out := buf.Bytes()
	return out, int64(len(out))
}

// --- happy paths ----------------------------------------------------------

func TestLoad_SingleFrame(t *testing.T) {
	raw, sz := build(buildSpec{
		width: 32, height: 32,
		frames: []frameSpec{{kind: FrameSingle, single: singleSpec{
			originX: -16, originY: -16, w: 32, h: 32,
			bitmap: bytes.Repeat([]byte{0xAA}, 32*32),
		}}},
	})
	sp, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Header.NumFrames != 1 || len(sp.Frames) != 1 {
		t.Fatalf("count")
	}
	if sp.Frames[0].Type != FrameSingle {
		t.Errorf("type: %d", sp.Frames[0].Type)
	}
	if sp.Frames[0].Single.Width != 32 || sp.Frames[0].Single.Height != 32 {
		t.Errorf("dims: %+v", sp.Frames[0].Single)
	}
	if len(sp.Frames[0].Single.Bitmap) != 32*32 {
		t.Errorf("bitmap size: %d", len(sp.Frames[0].Single.Bitmap))
	}
	if sp.Frames[0].Single.Bitmap[0] != 0xAA {
		t.Errorf("payload corruption")
	}
}

func TestLoad_GroupFrame(t *testing.T) {
	g := groupSpec{
		intervals: []float32{0.1, 0.25, 0.5},
		frames: []singleSpec{
			{w: 4, h: 4, bitmap: bytes.Repeat([]byte{1}, 16)},
			{w: 4, h: 4, bitmap: bytes.Repeat([]byte{2}, 16)},
			{w: 4, h: 4, bitmap: bytes.Repeat([]byte{3}, 16)},
		},
	}
	raw, sz := build(buildSpec{
		width: 4, height: 4,
		frames: []frameSpec{{kind: FrameGroup, group: g}},
	})
	sp, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Frames[0].Type != FrameGroup || sp.Frames[0].Group == nil {
		t.Fatalf("group type")
	}
	gf := sp.Frames[0].Group
	if len(gf.Intervals) != 3 || gf.Intervals[1] != 0.25 {
		t.Errorf("intervals: %v", gf.Intervals)
	}
	if len(gf.Frames) != 3 {
		t.Errorf("frames: %d", len(gf.Frames))
	}
	for i, sf := range gf.Frames {
		if sf.Bitmap[0] != byte(i+1) {
			t.Errorf("frame %d payload", i)
		}
	}
}

// --- error paths ----------------------------------------------------------

func TestLoad_NilSrc(t *testing.T) {
	if _, err := Load(nil, 100); err == nil {
		t.Error("expected nil-src error")
	}
}

func TestLoad_ShortHeader(t *testing.T) {
	if _, err := Load(bytes.NewReader([]byte{1, 2}), 2); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

type errReader struct{}

func (errReader) ReadAt([]byte, int64) (int, error) { return 0, errors.New("io fail") }

func TestLoad_ReadFails(t *testing.T) {
	if _, err := Load(errReader{}, 100); err == nil {
		t.Error("expected io error")
	}
}

type shortReader struct{}

func (shortReader) ReadAt(p []byte, _ int64) (int, error) { return len(p) / 2, io.EOF }

func TestLoad_ShortRead(t *testing.T) {
	if _, err := Load(shortReader{}, 100); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

func TestLoad_BadMagic(t *testing.T) {
	raw, sz := build(buildSpec{ident: 0xDEADBEEF})
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrBadMagic) {
		t.Errorf("got %v want ErrBadMagic", err)
	}
}

func TestLoad_BadVersion(t *testing.T) {
	raw, sz := build(buildSpec{version: 99})
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrBadVersion) {
		t.Errorf("got %v want ErrBadVersion", err)
	}
}

func TestLoad_UnknownFrameType(t *testing.T) {
	raw, sz := build(buildSpec{
		frames: []frameSpec{{kind: 42}}, // invalid tag
	})
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrBadFrameType) {
		t.Errorf("got %v want ErrBadFrameType", err)
	}
}

// --- truncation in each section ------------------------------------------

func TestLoad_TruncatedFrameTag(t *testing.T) {
	// Header says 1 frame, but the file ends right after the header.
	raw, sz := build(buildSpec{width: 1, height: 1})
	// Patch num_frames to 1 even though no frames follow.
	binary.LittleEndian.PutUint32(raw[24:28], 1)
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_TruncatedSingleHeader(t *testing.T) {
	// Single-frame tag is present but the frame header is cut off.
	buf := &bytes.Buffer{}
	putU32(buf, IDSpriteHeader)
	putI32(buf, Version)
	putI32(buf, 0)              // type
	putF32(buf, 0)              // boundingradius
	putI32(buf, 1); putI32(buf, 1)
	putI32(buf, 1)              // numframes
	putF32(buf, 0); putI32(buf, 0)
	putI32(buf, FrameSingle)    // tag
	putI32(buf, 0)              // origin_x only -- truncated here
	raw := buf.Bytes()
	if _, err := Load(bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_TruncatedBitmap(t *testing.T) {
	// Single frame header says 4x4 but only 1 bitmap byte follows.
	buf := &bytes.Buffer{}
	putU32(buf, IDSpriteHeader)
	putI32(buf, Version)
	putI32(buf, 0); putF32(buf, 0)
	putI32(buf, 4); putI32(buf, 4)
	putI32(buf, 1); putF32(buf, 0); putI32(buf, 0)
	putI32(buf, FrameSingle)
	putI32(buf, 0); putI32(buf, 0)
	putI32(buf, 4); putI32(buf, 4)
	buf.WriteByte(0xFF) // only 1 byte, need 16
	raw := buf.Bytes()
	if _, err := Load(bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_NegativeSingleDims(t *testing.T) {
	raw, sz := build(buildSpec{
		frames: []frameSpec{{kind: FrameSingle, single: singleSpec{w: -1, h: 4}}},
	})
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_TruncatedGroupHeader(t *testing.T) {
	// Frame tag is FrameGroup but no numframes int32 follows.
	buf := &bytes.Buffer{}
	putU32(buf, IDSpriteHeader)
	putI32(buf, Version)
	putI32(buf, 0); putF32(buf, 0)
	putI32(buf, 1); putI32(buf, 1)
	putI32(buf, 1); putF32(buf, 0); putI32(buf, 0)
	putI32(buf, FrameGroup) // tag
	// no group header follows
	raw := buf.Bytes()
	if _, err := Load(bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_NegativeGroupCount(t *testing.T) {
	buf := &bytes.Buffer{}
	putU32(buf, IDSpriteHeader)
	putI32(buf, Version)
	putI32(buf, 0); putF32(buf, 0)
	putI32(buf, 1); putI32(buf, 1)
	putI32(buf, 1); putF32(buf, 0); putI32(buf, 0)
	putI32(buf, FrameGroup)
	putI32(buf, -1) // negative numframes
	raw := buf.Bytes()
	if _, err := Load(bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_TruncatedGroupIntervals(t *testing.T) {
	buf := &bytes.Buffer{}
	putU32(buf, IDSpriteHeader)
	putI32(buf, Version)
	putI32(buf, 0); putF32(buf, 0)
	putI32(buf, 1); putI32(buf, 1)
	putI32(buf, 1); putF32(buf, 0); putI32(buf, 0)
	putI32(buf, FrameGroup)
	putI32(buf, 3) // 3 intervals expected
	// no intervals follow
	raw := buf.Bytes()
	if _, err := Load(bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_TruncatedGroupSubFrame(t *testing.T) {
	buf := &bytes.Buffer{}
	putU32(buf, IDSpriteHeader)
	putI32(buf, Version)
	putI32(buf, 0); putF32(buf, 0)
	putI32(buf, 1); putI32(buf, 1)
	putI32(buf, 1); putF32(buf, 0); putI32(buf, 0)
	putI32(buf, FrameGroup)
	putI32(buf, 1) // 1 frame
	putF32(buf, 0.1) // 1 interval
	// no sub-frame header
	raw := buf.Bytes()
	if _, err := Load(bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

// --- float precision sanity ------------------------------------------------

func TestLoad_FloatRoundTrip(t *testing.T) {
	raw, sz := build(buildSpec{
		frames: []frameSpec{{kind: FrameSingle}},
	})
	// Patch bounding_radius to a value tricky for naive casts.
	binary.LittleEndian.PutUint32(raw[12:16], math.Float32bits(math.Pi))
	sp, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Header.BoundingRadius != float32(math.Pi) {
		t.Errorf("got %v want pi", sp.Header.BoundingRadius)
	}
}

// --- zero-frame sprite is valid (header only) ------------------------------

func TestLoad_ZeroFrames(t *testing.T) {
	raw, sz := build(buildSpec{width: 4, height: 4})
	sp, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Header.NumFrames != 0 || len(sp.Frames) != 0 {
		t.Errorf("expected zero frames")
	}
}
