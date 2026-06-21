// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package demo

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// DemoTick is one recorded server-tic snapshot. The body is the
// raw payload that would otherwise have travelled over a NetConn:
// a concatenation of svc_* messages a client.SvcReader can decode
// directly.
type DemoTick struct {
	ViewAngles [3]float32 // recorded view angles (pitch / yaw / roll)
	Message    []byte     // raw server message body (svc_* messages concatenated)
}

// Sentinel errors returned by the parser + encoder.
var (
	// ErrDemoNilReader is returned by [ParseHeader] / [ParseTic] /
	// [Parse] when the supplied io.Reader is nil. The upstream C
	// dereferences without checking; the Go port refuses up-front so
	// callers get a typed error instead of a panic.
	ErrDemoNilReader = errors.New("demo: nil reader")

	// ErrDemoBadHeader is returned by [ParseHeader] when no '\n'
	// terminator is found within the first [maxHeaderBytes] bytes
	// (tyrquake's 12-iteration cap in CL_PlayDemo_f).
	ErrDemoBadHeader = errors.New("demo: bad CD-track header")

	// ErrDemoShortRead is returned by [ParseTic] when EOF lands
	// inside a tic record (mid-msglen, mid-angles, or mid-body).
	// A clean EOF between tics is reported as io.EOF instead.
	ErrDemoShortRead = errors.New("demo: unexpected EOF inside tic")

	// ErrDemoNegMsgLen is returned by [ParseTic] when the length
	// prefix is negative or exceeds [MaxDemoMessageLen]. The
	// upstream Sys_Errors on the latter; the Go port surfaces
	// both as one sentinel.
	ErrDemoNegMsgLen = errors.New("demo: negative or oversized message length")
)

// MaxDemoMessageLen is the per-tic message-body size cap. Mirrors
// tyrquake's MAX_MSGLEN (NQ/quakedef.h = 32768); a length prefix
// beyond this value is corrupt demo data.
const MaxDemoMessageLen = 32768

// maxHeaderBytes is the CD-track header cap. tyrquake's
// CL_PlayDemo_f reads exactly 12 bytes looking for '\n'; if the
// 12th byte is not '\n' the demo is rejected. The Go port uses
// the same bound so demos accepted here are accepted upstream.
const maxHeaderBytes = 12

// tickHeaderBytes is the fixed per-tic prefix: int32 msglen +
// 3*float32 angles.
const tickHeaderBytes = 4 + 4*3

// ParseHeader reads the optional CD-track manifest from the start
// of a .dem file: zero or more ASCII characters followed by a
// single '\n'. The manifest is returned (newline excluded) as a
// string for inspection; this parser does not interpret it.
//
// bytesConsumed counts every byte actually read off r, including
// the terminating newline. On error it reflects the bytes consumed
// up to the failure (so a caller can rewind a buffered reader).
//
// Upstream: NQ/cl_demo.c CL_PlayDemo_f, the
// "for (i = 0; i < 12; i++) c = getc(...)" loop.
func ParseHeader(r io.Reader) (cdTrack string, bytesConsumed int, err error) {
	if r == nil {
		return "", 0, ErrDemoNilReader
	}
	var buf [maxHeaderBytes]byte
	var one [1]byte
	for i := 0; i < maxHeaderBytes; i++ {
		n, rerr := r.Read(one[:])
		if n == 1 {
			bytesConsumed++
			if one[0] == '\n' {
				return string(buf[:i]), bytesConsumed, nil
			}
			buf[i] = one[0]
			continue
		}
		// n == 0; rerr must be non-nil per io.Reader contract when
		// no bytes are produced. Map EOF -> ErrDemoBadHeader because
		// a header that runs off the end of the file is invalid;
		// surface any other error verbatim.
		if errors.Is(rerr, io.EOF) {
			return "", bytesConsumed, ErrDemoBadHeader
		}
		return "", bytesConsumed, rerr
	}
	// Loop completed without seeing '\n' -- 12 bytes scanned, none
	// were the terminator. The upstream's `if (c != '\n')` check.
	return "", bytesConsumed, ErrDemoBadHeader
}

// ParseTic reads one [DemoTick] from r. Behaviours:
//
//   - returns (tick, nil) on a complete tic
//   - returns (zero, io.EOF) when r is exhausted cleanly between
//     tics (no bytes available)
//   - returns (zero, [ErrDemoShortRead]) on EOF mid-tic
//   - returns (zero, [ErrDemoNegMsgLen]) when the length prefix is
//     negative or > [MaxDemoMessageLen]
//   - returns (zero, err) on any other reader error
//
// Upstream: NQ/cl_demo.c CL_GetMessage, the cls.demoplayback
// branch (fread msglen, fread 3 floats, fread body).
func ParseTic(r io.Reader) (DemoTick, error) {
	if r == nil {
		return DemoTick{}, ErrDemoNilReader
	}

	// Read the fixed header (msglen + 3 angles) in one shot so a
	// short read in the first byte (= clean EOF) is distinguishable
	// from a short read after at least one byte (= mid-tic).
	var hdr [tickHeaderBytes]byte
	n, err := io.ReadFull(r, hdr[:])
	if err != nil {
		if errors.Is(err, io.EOF) && n == 0 {
			return DemoTick{}, io.EOF
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return DemoTick{}, ErrDemoShortRead
		}
		return DemoTick{}, err
	}

	msglen := int32(binary.LittleEndian.Uint32(hdr[0:4]))
	if msglen < 0 || msglen > MaxDemoMessageLen {
		return DemoTick{}, ErrDemoNegMsgLen
	}

	var tick DemoTick
	tick.ViewAngles[0] = math.Float32frombits(binary.LittleEndian.Uint32(hdr[4:8]))
	tick.ViewAngles[1] = math.Float32frombits(binary.LittleEndian.Uint32(hdr[8:12]))
	tick.ViewAngles[2] = math.Float32frombits(binary.LittleEndian.Uint32(hdr[12:16]))

	if msglen == 0 {
		tick.Message = []byte{}
		return tick, nil
	}

	body := make([]byte, msglen)
	if _, berr := io.ReadFull(r, body); berr != nil {
		if errors.Is(berr, io.EOF) || errors.Is(berr, io.ErrUnexpectedEOF) {
			return DemoTick{}, ErrDemoShortRead
		}
		return DemoTick{}, berr
	}
	tick.Message = body
	return tick, nil
}

// Parse loads an entire .dem file: header followed by every tic up
// to clean EOF. Returns (header, ticks, nil) on success; on the
// first error returns (header-so-far, partial-tick-slice, err).
//
// io.EOF from [ParseTic] is the normal terminator and is NOT
// surfaced as an error; any other ParseTic error is.
func Parse(r io.Reader) (header string, ticks []DemoTick, err error) {
	if r == nil {
		return "", nil, ErrDemoNilReader
	}
	hdr, _, herr := ParseHeader(r)
	if herr != nil {
		return "", nil, herr
	}
	header = hdr
	for {
		tick, terr := ParseTic(r)
		if errors.Is(terr, io.EOF) {
			return header, ticks, nil
		}
		if terr != nil {
			return header, ticks, terr
		}
		ticks = append(ticks, tick)
	}
}

// EncodeHeader writes the CD-track manifest: the given track string
// followed by a single '\n'. Pass "" for the empty manifest (just
// "\n"). A track longer than [maxHeaderBytes]-1 bytes is rejected
// as [ErrDemoBadHeader] so encoder output round-trips through
// [ParseHeader].
func EncodeHeader(w io.Writer, cdTrack string) error {
	if len(cdTrack) >= maxHeaderBytes {
		return ErrDemoBadHeader
	}
	out := make([]byte, 0, len(cdTrack)+1)
	out = append(out, cdTrack...)
	out = append(out, '\n')
	_, err := w.Write(out)
	return err
}

// EncodeTick serializes one [DemoTick] in the on-wire layout: int32
// little-endian length prefix, three float32 angles, body bytes. A
// nil-but-zero-length Message is treated as an empty body. A
// Message longer than [MaxDemoMessageLen] is rejected with
// [ErrDemoNegMsgLen].
func EncodeTick(w io.Writer, tick DemoTick) error {
	if len(tick.Message) > MaxDemoMessageLen {
		return ErrDemoNegMsgLen
	}
	var hdr [tickHeaderBytes]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(int32(len(tick.Message))))
	binary.LittleEndian.PutUint32(hdr[4:8], math.Float32bits(tick.ViewAngles[0]))
	binary.LittleEndian.PutUint32(hdr[8:12], math.Float32bits(tick.ViewAngles[1]))
	binary.LittleEndian.PutUint32(hdr[12:16], math.Float32bits(tick.ViewAngles[2]))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(tick.Message) == 0 {
		return nil
	}
	_, err := w.Write(tick.Message)
	return err
}
