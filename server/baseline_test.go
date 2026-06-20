// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// Vanilla NQ baseline: byte fields throughout, svc_spawnbaseline
// opcode, no fitz-bits, no alpha byte.
func TestEncodeBaseline_VanillaNQ(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{
		ModelIndex: 7,
		Frame:      3,
		ColorMap:   2,
		SkinNum:    1,
		Origin:     [3]float32{8, 16, 24},
		Angles:     [3]float32{0, 90, 180},
		Alpha:      0,
	}
	if err := EncodeBaseline(buf, 42, bl, protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	// 1 (cmd) + 2 (entnum) + 1 (model) + 1 (frame) + 1 (colormap) +
	// 1 (skin) + 3*(2 coord + 1 angle) = 16.
	if buf.Len() != 16 {
		t.Errorf("wire size: got %d want 16", buf.Len())
	}

	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcSpawnBaseline {
		t.Errorf("cmd: got %d want %d (SvcSpawnBaseline)", cmd, protocol.SvcSpawnBaseline)
	}
	if ent := r.ReadShort(); ent != 42 {
		t.Errorf("entnum: got %d want 42", ent)
	}
	if mi := r.ReadU8(); mi != 7 {
		t.Errorf("modelIndex: got %d want 7", mi)
	}
	if f := r.ReadU8(); f != 3 {
		t.Errorf("frame: got %d want 3", f)
	}
	if cm := r.ReadU8(); cm != 2 {
		t.Errorf("colormap: got %d want 2", cm)
	}
	if sk := r.ReadU8(); sk != 1 {
		t.Errorf("skin: got %d want 1", sk)
	}
	for axis, wantOrg := range [3]float32{8, 16, 24} {
		if got := r.ReadCoord(); got != wantOrg {
			t.Errorf("origin[%d]: got %v want %v", axis, got, wantOrg)
		}
		// Don't check angle bit-exact (WriteAngle quantises); just consume.
		_ = r.ReadAngle()
	}
	if r.Bad() {
		t.Errorf("unexpected EOF reading wire")
	}
}

// FITZ with LARGEMODEL: opcode flips to SvcFitzSpawnBaseline2,
// fitz-bits byte carries BFitzLargeModel, modelIndex is a short.
func TestEncodeBaseline_FitzLargeModel(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{ModelIndex: 500, Frame: 1}
	if err := EncodeBaseline(buf, 1, bl, protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcFitzSpawnBaseline2 {
		t.Errorf("cmd: got %d want SvcFitzSpawnBaseline2 (%d)", cmd, protocol.SvcFitzSpawnBaseline2)
	}
	_ = r.ReadShort() // entnum
	bits := r.ReadU8()
	if bits&protocol.BFitzLargeModel == 0 {
		t.Errorf("expected BFitzLargeModel set, got bits=0x%02x", bits)
	}
	if bits&(protocol.BFitzLargeFrame|protocol.BFitzAlpha) != 0 {
		t.Errorf("expected only BFitzLargeModel, got bits=0x%02x", bits)
	}
	if mi := r.ReadShort(); mi != 500 {
		t.Errorf("modelIndex (short): got %d want 500", mi)
	}
	if f := r.ReadU8(); f != 1 {
		t.Errorf("frame (byte): got %d want 1", f)
	}
}

// FITZ with LARGEFRAME: bits carry BFitzLargeFrame, frame is a short.
func TestEncodeBaseline_FitzLargeFrame(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{ModelIndex: 7, Frame: 300}
	if err := EncodeBaseline(buf, 1, bl, protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcFitzSpawnBaseline2 {
		t.Errorf("cmd: got %d want SvcFitzSpawnBaseline2", cmd)
	}
	_ = r.ReadShort()
	bits := r.ReadU8()
	if bits&protocol.BFitzLargeFrame == 0 {
		t.Errorf("expected BFitzLargeFrame set, got bits=0x%02x", bits)
	}
	if bits&protocol.BFitzLargeModel != 0 {
		t.Errorf("did not expect BFitzLargeModel, got bits=0x%02x", bits)
	}
	if mi := r.ReadU8(); mi != 7 {
		t.Errorf("modelIndex (byte): got %d want 7", mi)
	}
	if f := r.ReadShort(); f != 300 {
		t.Errorf("frame (short): got %d want 300", f)
	}
}

// FITZ with Alpha only: bits carry BFitzAlpha, trailing alpha byte
// is appended after the angles.
func TestEncodeBaseline_FitzAlpha(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{ModelIndex: 7, Frame: 3, Alpha: 128}
	if err := EncodeBaseline(buf, 1, bl, protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // cmd
	_ = r.ReadShort()
	bits := r.ReadU8()
	if bits != protocol.BFitzAlpha {
		t.Errorf("bits: got 0x%02x want BFitzAlpha (0x%02x)", bits, protocol.BFitzAlpha)
	}
	_ = r.ReadU8() // model
	_ = r.ReadU8() // frame
	_ = r.ReadU8() // colormap
	_ = r.ReadU8() // skin
	for i := 0; i < 3; i++ {
		_ = r.ReadCoord()
		_ = r.ReadAngle()
	}
	if a := r.ReadU8(); a != 128 {
		t.Errorf("alpha: got %d want 128", a)
	}
}

// FITZ with all three wide bits at once.
func TestEncodeBaseline_FitzAllBits(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{ModelIndex: 500, Frame: 300, Alpha: 200}
	if err := EncodeBaseline(buf, 1, bl, protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()
	_ = r.ReadShort()
	bits := r.ReadU8()
	want := protocol.BFitzLargeModel | protocol.BFitzLargeFrame | protocol.BFitzAlpha
	if bits != want {
		t.Errorf("bits: got 0x%02x want 0x%02x", bits, want)
	}
	if mi := r.ReadShort(); mi != 500 {
		t.Errorf("modelIndex: got %d want 500", mi)
	}
	if f := r.ReadShort(); f != 300 {
		t.Errorf("frame: got %d want 300", f)
	}
	_ = r.ReadU8() // colormap
	_ = r.ReadU8() // skin
	for i := 0; i < 3; i++ {
		_ = r.ReadCoord()
		_ = r.ReadAngle()
	}
	if a := r.ReadU8(); a != 200 {
		t.Errorf("alpha: got %d want 200", a)
	}
}

// NQ + modelIndex >= 256 -> ErrBaselineNeedsFitz, nothing written.
func TestEncodeBaseline_NQModelIndexNeedsFitz(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{ModelIndex: 500}
	err := EncodeBaseline(buf, 1, bl, protocol.VersionNQ)
	if !errors.Is(err, ErrBaselineNeedsFitz) {
		t.Errorf("got %v want ErrBaselineNeedsFitz", err)
	}
	if buf.Len() != 0 {
		t.Errorf("buf modified on rejected encode: len=%d", buf.Len())
	}
}

// NQ + frame >= 256 -> ErrBaselineNeedsFitz, nothing written.
func TestEncodeBaseline_NQFrameNeedsFitz(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{Frame: 300}
	err := EncodeBaseline(buf, 1, bl, protocol.VersionNQ)
	if !errors.Is(err, ErrBaselineNeedsFitz) {
		t.Errorf("got %v want ErrBaselineNeedsFitz", err)
	}
	if buf.Len() != 0 {
		t.Errorf("buf modified on rejected encode: len=%d", buf.Len())
	}
}

// BJP* + frame >= 256: still rejected (frame is byte-width on every
// non-FITZ protocol).
func TestEncodeBaseline_BJPFrameNeedsFitz(t *testing.T) {
	for _, v := range []int{protocol.VersionBJP, protocol.VersionBJP2, protocol.VersionBJP3} {
		buf := sizebuf.New(make([]byte, 64))
		bl := EntityBaseline{Frame: 300}
		err := EncodeBaseline(buf, 1, bl, v)
		if !errors.Is(err, ErrBaselineNeedsFitz) {
			t.Errorf("v=%d: got %v want ErrBaselineNeedsFitz", v, err)
		}
	}
}

// entityNum out of range -> ErrEntityRange.
func TestEncodeBaseline_EntityRange(t *testing.T) {
	for _, ent := range []int{-1, 0x10000, 1 << 20} {
		buf := sizebuf.New(make([]byte, 64))
		err := EncodeBaseline(buf, ent, EntityBaseline{}, protocol.VersionNQ)
		if !errors.Is(err, ErrEntityRange) {
			t.Errorf("ent=%d: got %v want ErrEntityRange", ent, err)
		}
	}
}

// Nil sizebuf -> error (not a sentinel; just non-nil).
func TestEncodeBaseline_NilBuf(t *testing.T) {
	if err := EncodeBaseline(nil, 0, EntityBaseline{}, protocol.VersionNQ); err == nil {
		t.Error("expected error on nil sizebuf")
	}
}

// BJP / BJP2 / BJP3 write modelIndex as a 2-byte short regardless
// of value: a small modelIndex still consumes 2 bytes.
func TestEncodeBaseline_BJPModelIndexAlwaysShort(t *testing.T) {
	for _, v := range []int{protocol.VersionBJP, protocol.VersionBJP2, protocol.VersionBJP3} {
		// small modelIndex (7) still consumed as short.
		buf := sizebuf.New(make([]byte, 64))
		bl := EntityBaseline{ModelIndex: 7, Frame: 1}
		if err := EncodeBaseline(buf, 1, bl, v); err != nil {
			t.Fatalf("v=%d small: %v", v, err)
		}
		// Vanilla NQ uses 16 bytes; BJP* uses one extra byte (short
		// modelIndex instead of byte).
		if buf.Len() != 17 {
			t.Errorf("v=%d small: wire size %d want 17", v, buf.Len())
		}
		r := msg.NewReader(buf.Bytes())
		if cmd := r.ReadU8(); cmd != protocol.SvcSpawnBaseline {
			t.Errorf("v=%d small: cmd %d want SvcSpawnBaseline", v, cmd)
		}
		_ = r.ReadShort()
		if mi := r.ReadShort(); mi != 7 {
			t.Errorf("v=%d small: modelIndex %d want 7", v, mi)
		}

		// modelIndex >= 256 is fine on BJP* (no fitz-needed gate),
		// short carries 500 cleanly.
		buf2 := sizebuf.New(make([]byte, 64))
		bl2 := EntityBaseline{ModelIndex: 500, Frame: 1}
		if err := EncodeBaseline(buf2, 1, bl2, v); err != nil {
			t.Fatalf("v=%d large: %v", v, err)
		}
		r2 := msg.NewReader(buf2.Bytes())
		_ = r2.ReadU8() // cmd
		_ = r2.ReadShort()
		if mi := r2.ReadShort(); mi != 500 {
			t.Errorf("v=%d large: modelIndex %d want 500", v, mi)
		}
	}
}

// Unknown protocol falls back to NQ-style byte modelIndex.
// Exercises the trailing fall-through in writeBaselineModelIndex
// so coverage hits the unreachable-from-canonical-versions branch.
func TestEncodeBaseline_UnknownProtocolFallsBackToByte(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	bl := EntityBaseline{ModelIndex: 7, Frame: 1}
	if err := EncodeBaseline(buf, 1, bl, 0); err != nil {
		t.Fatal(err)
	}
	// 16 bytes -- same shape as NQ.
	if buf.Len() != 16 {
		t.Errorf("unknown-protocol size: got %d want 16", buf.Len())
	}
}

// Per-write overflow propagation. Each successful msg.Write* in
// EncodeBaseline has its own err-return. Walk through every site
// by tightening cap to one byte below each write boundary. The
// vanilla NQ + alpha-bearing FITZ paths together cover all sites
// (the FITZ-bits byte, the LARGEMODEL/LARGEFRAME short writes, and
// the trailing alpha byte).
func TestEncodeBaseline_PerWriteOverflowPropagates(t *testing.T) {
	// Vanilla NQ wire boundaries (cumulative bytes after each write):
	//   1 (cmd), 3 (entnum), 4 (model), 5 (frame), 6 (colormap),
	//   7 (skin), then per-axis 9/10, 12/13, 15/16.
	// So failure caps: 0,1,3,4,5,6,7,9,10,12,13,15.
	nqCaps := []int{0, 1, 3, 4, 5, 6, 7, 9, 10, 12, 13, 15}
	for _, c := range nqCaps {
		t.Run("nq", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, c))
			err := EncodeBaseline(buf, 1, EntityBaseline{ModelIndex: 1, Frame: 1}, protocol.VersionNQ)
			if err == nil {
				t.Errorf("cap=%d: expected propagated err, got nil", c)
			}
			if errors.Is(err, ErrEntityRange) || errors.Is(err, ErrBaselineNeedsFitz) {
				t.Errorf("cap=%d: unexpected sentinel %v", c, err)
			}
		})
	}
	// Sanity: cap=16 succeeds clean for vanilla NQ.
	buf := sizebuf.New(make([]byte, 16))
	if err := EncodeBaseline(buf, 1, EntityBaseline{ModelIndex: 1, Frame: 1}, protocol.VersionNQ); err != nil {
		t.Errorf("cap=16 nq: unexpected err %v", err)
	}

	// FITZ-bits byte fail-cap (write #3 in the bits!=0 branch):
	// 1 cmd + 2 entnum fits in cap=3; the bits byte fails at cap<4.
	bufFitzBits := sizebuf.New(make([]byte, 3))
	err := EncodeBaseline(bufFitzBits, 1, EntityBaseline{ModelIndex: 500}, protocol.VersionFitz)
	if err == nil {
		t.Error("fitz cap=3: expected fitz-bits write to fail")
	}

	// Cap that fails JUST on the entnum short in the bits!=0 branch:
	// cap=1 fits cmd byte, fails on entnum short.
	bufFitzEnt := sizebuf.New(make([]byte, 1))
	err = EncodeBaseline(bufFitzEnt, 1, EntityBaseline{ModelIndex: 500}, protocol.VersionFitz)
	if err == nil {
		t.Error("fitz cap=1: expected entnum short to fail")
	}

	// Cap that fails on the short modelIndex (LARGEMODEL):
	// cmd+entnum+bits = 4 bytes fit at cap=4; modelIndex short needs 5.
	bufFitzShortModel := sizebuf.New(make([]byte, 4))
	err = EncodeBaseline(bufFitzShortModel, 1, EntityBaseline{ModelIndex: 500}, protocol.VersionFitz)
	if err == nil {
		t.Error("fitz cap=4 largemodel: expected modelIndex short to fail")
	}

	// Cap that fails on the short frame (LARGEFRAME, no LARGEMODEL):
	// cmd(1)+entnum(2)+bits(1)+model_byte(1) = 5; frame short needs 7.
	bufFitzShortFrame := sizebuf.New(make([]byte, 5))
	err = EncodeBaseline(bufFitzShortFrame, 1, EntityBaseline{ModelIndex: 7, Frame: 300}, protocol.VersionFitz)
	if err == nil {
		t.Error("fitz cap=5 largeframe: expected frame short to fail")
	}

	// Cap that fails on the trailing alpha byte:
	// FITZ + alpha-only: cmd(1)+entnum(2)+bits(1)+model_byte(1)+
	// frame_byte(1)+colormap(1)+skin(1)+3*(coord(2)+angle(1))=8+9=17.
	// Alpha at offset 17 needs cap >= 18.
	bufAlpha := sizebuf.New(make([]byte, 17))
	err = EncodeBaseline(bufAlpha, 1, EntityBaseline{ModelIndex: 7, Frame: 1, Alpha: 128}, protocol.VersionFitz)
	if err == nil {
		t.Error("fitz cap=17 alpha: expected alpha-byte write to fail")
	}

	// BJP modelIndex short-write fail: cmd(1)+entnum(2)=3 fits at cap=3,
	// modelIndex short fails.
	bufBJP := sizebuf.New(make([]byte, 3))
	err = EncodeBaseline(bufBJP, 1, EntityBaseline{ModelIndex: 7, Frame: 1}, protocol.VersionBJP)
	if err == nil {
		t.Error("bjp cap=3: expected modelIndex short to fail")
	}

	// Cap=0 with FITZ wide bits exercises the bits!=0 branch's cmd
	// byte write failure (mirrors the vanilla cap=0 case but via the
	// other opcode path).
	bufFitzCmd := sizebuf.New(make([]byte, 0))
	err = EncodeBaseline(bufFitzCmd, 1, EntityBaseline{ModelIndex: 500}, protocol.VersionFitz)
	if err == nil {
		t.Error("fitz cap=0: expected SvcFitzSpawnBaseline2 byte write to fail")
	}
}
