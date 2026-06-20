// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package model

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/mdl"
	"github.com/go-quake1/engine/spr"
)

// buildMinimalMdl synthesises just enough .mdl bytes for mdl.Load
// to succeed: an 84-byte header with zero counts.
func buildMinimalMdl() []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, mdl.IDPolyHeader)
	_ = binary.Write(buf, binary.LittleEndian, int32(mdl.Version))
	// 19 more int32-equivalents = 76 bytes of zero (scale/origin/
	// radius/eye/skin/vert/tri/frame counts + sync/flags/size). The
	// upstream's mdl.Header struct serialises as 84 bytes total.
	buf.Write(make([]byte, 84-8))
	return buf.Bytes()
}

// buildMinimalSpr synthesises just enough .spr bytes for spr.Load.
func buildMinimalSpr() []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, spr.IDSpriteHeader)
	_ = binary.Write(buf, binary.LittleEndian, int32(spr.Version))
	// 7 more int32 / float fields = 28 bytes (type, bounding_radius,
	// width, height, numframes, beamlength, synctype).
	buf.Write(make([]byte, 36-8))
	return buf.Bytes()
}

// buildMinimalBSP synthesises a 124-byte BSP header with version 29
// + all-zero lump table.
func buildMinimalBSP() []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(bspfile.Version29))
	buf.Write(make([]byte, 15*8)) // 15 lump_t entries = 120 bytes
	return buf.Bytes()
}

// --- detection-by-magic dispatch ---

func TestLoad_DispatchesAlias(t *testing.T) {
	data := buildMinimalMdl()
	m, err := Load(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindAlias || m.Alias == nil || m.Brush != nil || m.Sprite != nil {
		t.Errorf("dispatch: %+v", m)
	}
}

func TestLoad_DispatchesSprite(t *testing.T) {
	data := buildMinimalSpr()
	m, err := Load(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindSprite || m.Sprite == nil || m.Brush != nil || m.Alias != nil {
		t.Errorf("dispatch: %+v", m)
	}
}

func TestLoad_DispatchesBrush(t *testing.T) {
	data := buildMinimalBSP()
	m, err := Load(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindBrush || m.Brush == nil || m.Alias != nil || m.Sprite != nil {
		t.Errorf("dispatch: %+v", m)
	}
}

// --- per-loader error propagation ---

func TestLoad_AliasLoaderRejects(t *testing.T) {
	// Magic OK but version wrong -> mdl.ErrBadVersion -> our
	// ErrLoaderFail wrapper.
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, mdl.IDPolyHeader)
	_ = binary.Write(buf, binary.LittleEndian, int32(99)) // bad version
	buf.Write(make([]byte, 84-8))
	if _, err := Load(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrLoaderFail) {
		t.Errorf("got %v want ErrLoaderFail", err)
	}
}

func TestLoad_SpriteLoaderRejects(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, spr.IDSpriteHeader)
	_ = binary.Write(buf, binary.LittleEndian, int32(99))
	buf.Write(make([]byte, 36-8))
	if _, err := Load(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrLoaderFail) {
		t.Errorf("got %v want ErrLoaderFail", err)
	}
}

func TestLoad_BrushLoaderRejects(t *testing.T) {
	// Non-matching magic + bad BSP version -> bspfile.ErrBadVersion
	// surfaces through our wrapper.
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(99))
	buf.Write(make([]byte, 124-4))
	if _, err := Load(bytes.NewReader(buf.Bytes()), int64(buf.Len())); !errors.Is(err, ErrLoaderFail) {
		t.Errorf("got %v want ErrLoaderFail", err)
	}
}

// --- top-level error paths ---

func TestLoad_NilSrc(t *testing.T) {
	if _, err := Load(nil, 100); err == nil {
		t.Error("expected nil-src error")
	}
}

func TestLoad_TooSmall(t *testing.T) {
	if _, err := Load(bytes.NewReader([]byte{1, 2}), 2); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

type errReader struct{}

func (errReader) ReadAt(p []byte, _ int64) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestLoad_HeaderReadFails(t *testing.T) {
	if _, err := Load(errReader{}, 100); err == nil {
		t.Error("expected header read error")
	}
}

func TestKindLayout(t *testing.T) {
	if KindBrush != 0 || KindAlias != 1 || KindSprite != 2 {
		t.Error("Kind layout drift")
	}
}
