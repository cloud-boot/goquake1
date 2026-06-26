// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package demo

import (
	"io"
)

// Reader is a stateful streaming wrapper around an [io.Reader] that
// holds a .dem file's CD-track header + lets the caller pull one
// [DemoTick] at a time via [Reader.NextFrame]. Built so the runloop
// can feed demo ticks per-frame without buffering the entire demo
// in memory + without re-parsing the header on every tick.
//
// Construction: [NewReader] reads + validates the CD-track header
// once, then returns a *Reader positioned at the first tick body.
// Subsequent [Reader.NextFrame] calls advance through the tick
// stream; [io.EOF] is returned at the clean end of stream so the
// caller can decide whether to stop, restart, or chain another
// source.
//
// The wrapper is intentionally thin: the per-frame parse path goes
// straight through the existing [ParseTic] helper (which already
// handles short reads + length-prefix bounds), so the streaming
// API and the slurp-everything [Parse] API share the same
// underlying decoder.
type Reader struct {
	// CdTrack is the manifest string read off the demo's first line
	// (newline excluded). Vanilla demos store the soundtrack index
	// here (e.g. "5"); the field is exposed for inspection but not
	// interpreted by the playback path.
	CdTrack string

	// r is the underlying byte source positioned just past the
	// CD-track newline. Owned by the caller (NewReader does not
	// close it); freshly-constructed Readers point at the start of
	// the first tick.
	r io.Reader
}

// NewReader builds a [Reader] from src. The CD-track header is
// parsed immediately so a bad header is surfaced at construction
// time (NOT lazily on the first NextFrame call); on success the
// returned reader is positioned at the first tick body.
//
// Returns [ErrDemoNilReader] when src is nil + the matching header
// errors ([ErrDemoBadHeader]) verbatim. Any other reader error is
// propagated as-is so the caller can distinguish IO faults from
// malformed-demo data.
func NewReader(src io.Reader) (*Reader, error) {
	if src == nil {
		return nil, ErrDemoNilReader
	}
	cd, _, err := ParseHeader(src)
	if err != nil {
		return nil, err
	}
	return &Reader{CdTrack: cd, r: src}, nil
}

// NextFrame returns the next [DemoTick] from the stream. Behaviours
// mirror [ParseTic] verbatim:
//
//   - returns (tick, nil) on a complete tic
//   - returns (zero, [io.EOF]) at the clean end of stream
//   - returns (zero, [ErrDemoShortRead]) on EOF mid-tic
//   - returns (zero, [ErrDemoNegMsgLen]) on a corrupt length prefix
//   - propagates any other underlying reader error verbatim
//
// Convenience over ParseTic: NextFrame keeps the per-Reader
// position implicit so callers don't have to thread the src reader
// + the CD-track string around themselves.
func (rd *Reader) NextFrame() (DemoTick, error) {
	return ParseTic(rd.r)
}
