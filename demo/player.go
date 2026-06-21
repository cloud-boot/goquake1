// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package demo

import (
	"errors"

	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
)

// ErrPlayerNilState is returned by [PlayTick] / [Play] when the
// supplied [client.State] pointer is nil. The C upstream's
// CL_ParseServerMessage dereferences cl.* without checking; the Go
// port refuses up-front so callers get a typed error.
var ErrPlayerNilState = errors.New("demo: nil client state in player")

// ErrPlayerNilTick is returned by [PlayTick] when the supplied
// [DemoTick] pointer is nil. Same rationale as [ErrPlayerNilState].
var ErrPlayerNilTick = errors.New("demo: nil tick in player")

// defaultTickDeltaSec is the per-tic time advance used when
// PlayerOpts.TickDelta is zero -- 1/20s, the historical NetQuake
// server-tic rate (sv_maxspeed/sv_friction etc. are all tuned for
// this cadence).
const defaultTickDeltaSec float32 = 1.0 / 20.0

// PlayerOpts configures [PlayTick] / [Play] behaviour.
//
//   - Protocol selects the protocol version routed into
//     [client.SvcReader.Next]; defaults to [protocol.VersionNQ].
//   - TickDelta is the seconds advanced per tick. Zero means use
//     the default 1/20s cadence; pass a nonzero value to play
//     faster/slower (or to thread a wall-clock delta in).
//   - SkipUnknownSvc, when true, swallows [client.ErrUnknownSvc]
//     from the dispatcher so a demo with a forward-compat opcode
//     can still be walked; when false (default) the sentinel is
//     surfaced verbatim.
type PlayerOpts struct {
	Protocol       int
	TickDelta      float32
	SkipUnknownSvc bool
}

// DefaultPlayerOpts returns the conventional defaults: NetQuake
// protocol, 1/20s tick advance, and strict unknown-svc surfacing.
func DefaultPlayerOpts() PlayerOpts {
	return PlayerOpts{
		Protocol:       protocol.VersionNQ,
		TickDelta:      defaultTickDeltaSec,
		SkipUnknownSvc: false,
	}
}

// PlayTick decodes one [DemoTick]'s message body via
// [client.SvcReader.Next] and applies each [client.Decoded] value to
// state. Every successful apply stamps state.MsgTime with nowSec;
// after the last message of the tick, state.MsgTime is advanced by
// opts.TickDelta (or the default 1/20s when TickDelta == 0). The
// tick's recorded view-angles are written into *outAngles so callers
// can drive their own camera state independently of the wire layer.
//
// Returns:
//
//   - [ErrPlayerNilState]      if state == nil
//   - [ErrPlayerNilTick]       if tick == nil
//   - [client.ErrUnknownSvc]   from the SvcReader unless opts.SkipUnknownSvc
//   - [client.ErrCorruptMessage] from the SvcReader, verbatim
//   - any [client.Apply] error, verbatim
//
// nowSec is the wall-clock-like server time of THIS tick; the
// MsgTime advance done at the end of PlayTick stamps the time of
// the NEXT tick so a subsequent call can pass nowSec+TickDelta to
// keep state.MsgTime monotonic.
func PlayTick(state *client.State, tick *DemoTick, outAngles *[3]float32, opts PlayerOpts) error {
	if state == nil {
		return ErrPlayerNilState
	}
	if tick == nil {
		return ErrPlayerNilTick
	}

	if outAngles != nil {
		*outAngles = tick.ViewAngles
	}

	delta := opts.TickDelta
	if delta == 0 {
		delta = defaultTickDeltaSec
	}

	sr := client.SvcReader{R: msg.NewReader(tick.Message)}
	for {
		decoded, err := sr.Next(opts.Protocol)
		if err != nil {
			if errors.Is(err, client.ErrEOF) {
				break
			}
			if errors.Is(err, client.ErrUnknownSvc) && opts.SkipUnknownSvc {
				continue
			}
			return err
		}
		if aerr := client.Apply(state, decoded, state.MsgTime); aerr != nil {
			return aerr
		}
	}

	state.MsgTime += delta
	return nil
}

// Play runs [PlayTick] on each tick in `ticks` in order, threading
// the running time into [client.Apply] via state.MsgTime. nowSec is
// the starting server time used to stamp the first tick; each
// subsequent tick is stamped at nowSec + i * opts.TickDelta.
//
// Returns the count of successfully played ticks + the first error
// (or nil + len(ticks) on success). On error the returned count is
// the index of the failing tick (i.e. how many ticks completed
// before the failure).
func Play(state *client.State, ticks []DemoTick, outAngles *[3]float32, nowSec float32, opts PlayerOpts) (int, error) {
	if state == nil {
		return 0, ErrPlayerNilState
	}

	state.MsgTime = nowSec
	for i := range ticks {
		if err := PlayTick(state, &ticks[i], outAngles, opts); err != nil {
			return i, err
		}
	}
	return len(ticks), nil
}
