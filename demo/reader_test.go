// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package demo

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// makeDemoBlob constructs an in-memory .dem byte stream with the
// given CD-track header + tick sequence. Used by [TestReader_*] to
// drive NewReader / NextFrame without touching a real pak.
func makeDemoBlob(t *testing.T, cd string, ticks []DemoTick) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeHeader(&buf, cd); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	for i := range ticks {
		if err := EncodeTick(&buf, ticks[i]); err != nil {
			t.Fatalf("EncodeTick[%d]: %v", i, err)
		}
	}
	return buf.Bytes()
}

func TestNewReader_HappyHeader(t *testing.T) {
	blob := makeDemoBlob(t, "5", nil)
	rd, err := NewReader(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if rd.CdTrack != "5" {
		t.Errorf("CdTrack = %q want %q", rd.CdTrack, "5")
	}
}

func TestNewReader_NilSourceRejected(t *testing.T) {
	if _, err := NewReader(nil); !errors.Is(err, ErrDemoNilReader) {
		t.Fatalf("err = %v want ErrDemoNilReader", err)
	}
}

func TestNewReader_BadHeaderPropagated(t *testing.T) {
	// 12 chars without newline -> ParseHeader returns ErrDemoBadHeader.
	if _, err := NewReader(bytes.NewReader([]byte("123456789012"))); !errors.Is(err, ErrDemoBadHeader) {
		t.Fatalf("err = %v want ErrDemoBadHeader", err)
	}
}

func TestReader_NextFrame_ThreeTicksThenEOF(t *testing.T) {
	want := []DemoTick{
		{ViewAngles: [3]float32{1, 2, 3}, Message: []byte{0x01, 0x02}},
		{ViewAngles: [3]float32{4, 5, 6}, Message: []byte{0x03}},
		{ViewAngles: [3]float32{7, 8, 9}, Message: []byte{}},
	}
	blob := makeDemoBlob(t, "0", want)

	rd, err := NewReader(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	for i, w := range want {
		got, gerr := rd.NextFrame()
		if gerr != nil {
			t.Fatalf("tick %d NextFrame: %v", i, gerr)
		}
		if got.ViewAngles != w.ViewAngles {
			t.Errorf("tick %d angles = %v want %v", i, got.ViewAngles, w.ViewAngles)
		}
		if !bytes.Equal(got.Message, w.Message) {
			t.Errorf("tick %d body = %v want %v", i, got.Message, w.Message)
		}
	}
	if _, err := rd.NextFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("post-stream err = %v want io.EOF", err)
	}
}

func TestReader_NextFrame_CorruptLengthPropagated(t *testing.T) {
	// Build a valid header followed by an oversized length prefix.
	var buf bytes.Buffer
	if err := EncodeHeader(&buf, ""); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	// msglen = MaxDemoMessageLen + 1, little-endian; angles ignored.
	buf.Write([]byte{0x01, 0x80, 0x00, 0x00}) // = 0x8001 = 32769 > 32768
	buf.Write(make([]byte, 12))               // 3 floats

	rd, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if _, err := rd.NextFrame(); !errors.Is(err, ErrDemoNegMsgLen) {
		t.Fatalf("err = %v want ErrDemoNegMsgLen", err)
	}
}
