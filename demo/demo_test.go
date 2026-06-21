// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package demo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

// ----- ParseHeader -----------------------------------------------------------

func TestParseHeader_HappyTrackFive(t *testing.T) {
	track, n, err := ParseHeader(bytes.NewReader([]byte("5\n")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track != "5" {
		t.Errorf("track = %q want %q", track, "5")
	}
	if n != 2 {
		t.Errorf("bytesConsumed = %d want 2", n)
	}
}

func TestParseHeader_EmptyHeader(t *testing.T) {
	track, n, err := ParseHeader(bytes.NewReader([]byte("\n")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track != "" {
		t.Errorf("track = %q want empty", track)
	}
	if n != 1 {
		t.Errorf("bytesConsumed = %d want 1", n)
	}
}

func TestParseHeader_MaxLengthBoundary(t *testing.T) {
	// 11 chars + '\n' = 12 bytes exactly -- the upstream upper bound.
	track, n, err := ParseHeader(bytes.NewReader([]byte("12345678901\n")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track != "12345678901" {
		t.Errorf("track = %q", track)
	}
	if n != 12 {
		t.Errorf("bytesConsumed = %d want 12", n)
	}
}

func TestParseHeader_TooLongRejected(t *testing.T) {
	// 12 chars without newline -> all 12 read, then loop exits with c != '\n'.
	_, n, err := ParseHeader(bytes.NewReader([]byte("123456789012\n")))
	if !errors.Is(err, ErrDemoBadHeader) {
		t.Fatalf("err = %v want ErrDemoBadHeader", err)
	}
	if n != maxHeaderBytes {
		t.Errorf("bytesConsumed = %d want %d", n, maxHeaderBytes)
	}
}

func TestParseHeader_TruncatedNoNewline(t *testing.T) {
	_, n, err := ParseHeader(bytes.NewReader([]byte("5")))
	if !errors.Is(err, ErrDemoBadHeader) {
		t.Fatalf("err = %v want ErrDemoBadHeader", err)
	}
	if n != 1 {
		t.Errorf("bytesConsumed = %d want 1", n)
	}
}

func TestParseHeader_EmptyReader(t *testing.T) {
	_, n, err := ParseHeader(bytes.NewReader(nil))
	if !errors.Is(err, ErrDemoBadHeader) {
		t.Fatalf("err = %v want ErrDemoBadHeader", err)
	}
	if n != 0 {
		t.Errorf("bytesConsumed = %d want 0", n)
	}
}

func TestParseHeader_NilReader(t *testing.T) {
	_, _, err := ParseHeader(nil)
	if !errors.Is(err, ErrDemoNilReader) {
		t.Fatalf("err = %v want ErrDemoNilReader", err)
	}
}

// errReader returns the given error on the first Read after `delay`
// successful one-byte reads.
type errReader struct {
	data  []byte
	pos   int
	delay int
	err   error
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.pos < e.delay && e.pos < len(e.data) {
		p[0] = e.data[e.pos]
		e.pos++
		return 1, nil
	}
	return 0, e.err
}

func TestParseHeader_NonEOFReaderErrorPropagated(t *testing.T) {
	want := errors.New("boom")
	_, n, err := ParseHeader(&errReader{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
	if n != 0 {
		t.Errorf("bytesConsumed = %d want 0", n)
	}
}

func TestParseHeader_NonEOFReaderErrorMidHeader(t *testing.T) {
	want := errors.New("boom mid-header")
	r := &errReader{data: []byte("12"), delay: 2, err: want}
	_, n, err := ParseHeader(r)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
	if n != 2 {
		t.Errorf("bytesConsumed = %d want 2", n)
	}
}

// ----- ParseTic --------------------------------------------------------------

func encodeTickBytes(t *testing.T, tick DemoTick) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeTick(&buf, tick); err != nil {
		t.Fatalf("EncodeTick: %v", err)
	}
	return buf.Bytes()
}

func TestParseTic_HappySynthetic(t *testing.T) {
	tick := DemoTick{
		ViewAngles: [3]float32{1.5, -22.25, 0.125},
		Message:    []byte{0x01, 0x02, 0x03, 0x04, 0x05},
	}
	raw := encodeTickBytes(t, tick)
	got, err := ParseTic(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseTic: %v", err)
	}
	if got.ViewAngles != tick.ViewAngles {
		t.Errorf("angles = %v want %v", got.ViewAngles, tick.ViewAngles)
	}
	if !bytes.Equal(got.Message, tick.Message) {
		t.Errorf("body = %v want %v", got.Message, tick.Message)
	}
}

func TestParseTic_EmptyBodyAllowed(t *testing.T) {
	tick := DemoTick{ViewAngles: [3]float32{0, 0, 0}, Message: nil}
	raw := encodeTickBytes(t, tick)
	got, err := ParseTic(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseTic: %v", err)
	}
	if len(got.Message) != 0 {
		t.Errorf("body len = %d want 0", len(got.Message))
	}
}

func TestParseTic_CleanEOFBetweenTics(t *testing.T) {
	_, err := ParseTic(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v want io.EOF", err)
	}
}

func TestParseTic_TruncatedMidMsglen(t *testing.T) {
	// 2 bytes of the 4-byte length prefix.
	_, err := ParseTic(bytes.NewReader([]byte{0x05, 0x00}))
	if !errors.Is(err, ErrDemoShortRead) {
		t.Fatalf("err = %v want ErrDemoShortRead", err)
	}
}

func TestParseTic_TruncatedMidAngles(t *testing.T) {
	// 4-byte length + 6 bytes of the 12-byte angle triple.
	buf := make([]byte, 0, 10)
	var lp [4]byte
	binary.LittleEndian.PutUint32(lp[:], 0)
	buf = append(buf, lp[:]...)
	buf = append(buf, []byte{0, 0, 0, 0, 0, 0}...)
	_, err := ParseTic(bytes.NewReader(buf))
	if !errors.Is(err, ErrDemoShortRead) {
		t.Fatalf("err = %v want ErrDemoShortRead", err)
	}
}

func TestParseTic_TruncatedMidBody(t *testing.T) {
	// Length says 5 bytes but only 2 follow.
	var hdr [tickHeaderBytes]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 5)
	raw := append(hdr[:], []byte{0xaa, 0xbb}...)
	_, err := ParseTic(bytes.NewReader(raw))
	if !errors.Is(err, ErrDemoShortRead) {
		t.Fatalf("err = %v want ErrDemoShortRead", err)
	}
}

func TestParseTic_NegativeMsglen(t *testing.T) {
	var hdr [tickHeaderBytes]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(0xFFFFFFFF)) // int32(-1) two's complement
	_, err := ParseTic(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrDemoNegMsgLen) {
		t.Fatalf("err = %v want ErrDemoNegMsgLen", err)
	}
}

func TestParseTic_OversizedMsglen(t *testing.T) {
	var hdr [tickHeaderBytes]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(MaxDemoMessageLen+1))
	_, err := ParseTic(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrDemoNegMsgLen) {
		t.Fatalf("err = %v want ErrDemoNegMsgLen", err)
	}
}

func TestParseTic_NilReader(t *testing.T) {
	_, err := ParseTic(nil)
	if !errors.Is(err, ErrDemoNilReader) {
		t.Fatalf("err = %v want ErrDemoNilReader", err)
	}
}

func TestParseTic_NonEOFReaderErrorOnHeader(t *testing.T) {
	want := errors.New("io kaboom")
	_, err := ParseTic(&errReader{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
}

func TestParseTic_NonEOFReaderErrorOnBody(t *testing.T) {
	// Header reads cleanly; body read fails with a non-EOF error.
	var hdr [tickHeaderBytes]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 3)
	want := errors.New("body kaboom")
	r := &concatReader{
		first:  bytes.NewReader(hdr[:]),
		second: &errReader{err: want},
	}
	_, err := ParseTic(r)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
}

// concatReader streams `first` to exhaustion, then delegates to
// `second`. Lets us drive the body-read path with a custom error.
type concatReader struct {
	first   io.Reader
	second  io.Reader
	drained bool
}

func (c *concatReader) Read(p []byte) (int, error) {
	if !c.drained {
		n, err := c.first.Read(p)
		if n > 0 {
			return n, nil
		}
		if errors.Is(err, io.EOF) {
			c.drained = true
		} else if err != nil {
			return n, err
		}
	}
	return c.second.Read(p)
}

// ----- Parse -----------------------------------------------------------------

func TestParse_RoundtripHeaderAndTicks(t *testing.T) {
	want := []DemoTick{
		{ViewAngles: [3]float32{1, 2, 3}, Message: []byte{0xde, 0xad}},
		{ViewAngles: [3]float32{-1, 0, 0.5}, Message: []byte{0xbe, 0xef, 0x01}},
		{ViewAngles: [3]float32{math.Pi, math.E, 0}, Message: []byte{}},
	}
	var buf bytes.Buffer
	if err := EncodeHeader(&buf, "7"); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	for _, tk := range want {
		if err := EncodeTick(&buf, tk); err != nil {
			t.Fatalf("EncodeTick: %v", err)
		}
	}
	hdr, got, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if hdr != "7" {
		t.Errorf("header = %q want %q", hdr, "7")
	}
	if len(got) != len(want) {
		t.Fatalf("tick count = %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ViewAngles != want[i].ViewAngles {
			t.Errorf("tick[%d] angles = %v want %v", i, got[i].ViewAngles, want[i].ViewAngles)
		}
		if !bytes.Equal(got[i].Message, want[i].Message) {
			t.Errorf("tick[%d] body = %v want %v", i, got[i].Message, want[i].Message)
		}
	}
}

func TestParse_EmptyDemoOK(t *testing.T) {
	hdr, ticks, err := Parse(bytes.NewReader([]byte("0\n")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if hdr != "0" {
		t.Errorf("header = %q want \"0\"", hdr)
	}
	if len(ticks) != 0 {
		t.Errorf("ticks = %d want 0", len(ticks))
	}
}

func TestParse_NilReader(t *testing.T) {
	_, _, err := Parse(nil)
	if !errors.Is(err, ErrDemoNilReader) {
		t.Fatalf("err = %v want ErrDemoNilReader", err)
	}
}

func TestParse_BadHeader(t *testing.T) {
	_, _, err := Parse(bytes.NewReader([]byte("noNewline")))
	if !errors.Is(err, ErrDemoBadHeader) {
		t.Fatalf("err = %v want ErrDemoBadHeader", err)
	}
}

func TestParse_PartialTickReturnsPartialSlice(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeHeader(&buf, ""); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	good := DemoTick{ViewAngles: [3]float32{9, 8, 7}, Message: []byte{0x42}}
	if err := EncodeTick(&buf, good); err != nil {
		t.Fatalf("EncodeTick: %v", err)
	}
	// Append a truncated next-tic header (mid-msglen).
	buf.Write([]byte{0x05, 0x00})

	hdr, got, err := Parse(&buf)
	if !errors.Is(err, ErrDemoShortRead) {
		t.Fatalf("err = %v want ErrDemoShortRead", err)
	}
	if hdr != "" {
		t.Errorf("header = %q want empty", hdr)
	}
	if len(got) != 1 {
		t.Fatalf("partial slice len = %d want 1", len(got))
	}
	if got[0].ViewAngles != good.ViewAngles {
		t.Errorf("recovered tick angles = %v want %v", got[0].ViewAngles, good.ViewAngles)
	}
}

// ----- EncodeHeader / EncodeTick --------------------------------------------

func TestEncodeHeader_EmptyJustNewline(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeHeader(&buf, ""); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), []byte("\n")) {
		t.Errorf("bytes = %v want \\n", buf.Bytes())
	}
}

func TestEncodeHeader_TooLongRejected(t *testing.T) {
	// 12 chars -- equals the cap, the encoder rejects because the
	// terminator would push the round-trip to 13 bytes.
	err := EncodeHeader(&bytes.Buffer{}, "123456789012")
	if !errors.Is(err, ErrDemoBadHeader) {
		t.Fatalf("err = %v want ErrDemoBadHeader", err)
	}
}

// errWriter fails on every Write with the supplied error.
type errWriter struct{ err error }

func (e errWriter) Write(p []byte) (int, error) { return 0, e.err }

func TestEncodeHeader_WriterErrorPropagated(t *testing.T) {
	want := errors.New("disk full")
	err := EncodeHeader(errWriter{err: want}, "5")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
}

func TestEncodeTick_OversizedBodyRejected(t *testing.T) {
	huge := DemoTick{Message: make([]byte, MaxDemoMessageLen+1)}
	if err := EncodeTick(&bytes.Buffer{}, huge); !errors.Is(err, ErrDemoNegMsgLen) {
		t.Fatalf("err = %v want ErrDemoNegMsgLen", err)
	}
}

func TestEncodeTick_HeaderWriteError(t *testing.T) {
	want := errors.New("header fail")
	err := EncodeTick(errWriter{err: want}, DemoTick{Message: []byte{0x01}})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
}

func TestEncodeTick_BodyWriteError(t *testing.T) {
	// Header succeeds (16 bytes accepted), body Write returns error.
	want := errors.New("body fail")
	w := &partialWriter{acceptFirst: tickHeaderBytes, then: want}
	err := EncodeTick(w, DemoTick{Message: []byte{0x01, 0x02, 0x03}})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
}

func TestEncodeTick_EmptyBodySkipsBodyWrite(t *testing.T) {
	// If empty-body skipped the body write, only the 16-byte header
	// is emitted; verifies the early-return branch.
	var buf bytes.Buffer
	if err := EncodeTick(&buf, DemoTick{}); err != nil {
		t.Fatalf("EncodeTick: %v", err)
	}
	if buf.Len() != tickHeaderBytes {
		t.Errorf("wrote %d bytes, want %d", buf.Len(), tickHeaderBytes)
	}
}

// partialWriter accepts the first `acceptFirst` bytes then returns
// `then` on every subsequent Write.
type partialWriter struct {
	acceptFirst int
	then        error
	wrote       int
}

func (p *partialWriter) Write(b []byte) (int, error) {
	remaining := p.acceptFirst - p.wrote
	if remaining <= 0 {
		return 0, p.then
	}
	if len(b) <= remaining {
		p.wrote += len(b)
		return len(b), nil
	}
	p.wrote += remaining
	return remaining, nil
}
