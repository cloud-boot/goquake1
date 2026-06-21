// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package demo

import (
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/protocol"
)

// unknownSvcOpcode is a low-bit opcode value not dispatched by
// client.SvcReader.Next (SvcVersion=4 is the historical "client
// protocol-version probe" reserved opcode the wire decoder
// intentionally doesn't handle).
const unknownSvcOpcode = byte(protocol.SvcVersion)

// signonStageOneBody is the two-byte SvcSignonNum/stage=1 body.
var signonStageOneBody = []byte{byte(protocol.SvcSignonNum), 0x01}

// nopByte is the single-byte SvcNop heartbeat.
var nopByte = byte(protocol.SvcNop)

// ----- PlayTick happy path ---------------------------------------------------

func TestPlayTick_NopAndSignonAdvanceTime(t *testing.T) {
	var state client.State
	state.MsgTime = 10.0

	tick := &DemoTick{
		ViewAngles: [3]float32{1.5, -2.25, 0.5},
		Message:    append([]byte{nopByte}, signonStageOneBody...),
	}
	var angles [3]float32

	if err := PlayTick(&state, tick, &angles, DefaultPlayerOpts()); err != nil {
		t.Fatalf("PlayTick: %v", err)
	}

	// Default delta = 1/20s, starting from 10.0 -> 10.05.
	want := float32(10.0) + 1.0/20.0
	if math.Abs(float64(state.MsgTime-want)) > 1e-6 {
		t.Errorf("MsgTime = %v want %v", state.MsgTime, want)
	}
	if angles != tick.ViewAngles {
		t.Errorf("angles = %v want %v", angles, tick.ViewAngles)
	}
}

func TestPlayTick_CustomTickDeltaHonored(t *testing.T) {
	var state client.State
	state.MsgTime = 0
	tick := &DemoTick{Message: []byte{nopByte}}

	opts := DefaultPlayerOpts()
	opts.TickDelta = 0.125
	if err := PlayTick(&state, tick, nil, opts); err != nil {
		t.Fatalf("PlayTick: %v", err)
	}
	if math.Abs(float64(state.MsgTime-0.125)) > 1e-6 {
		t.Errorf("MsgTime = %v want 0.125", state.MsgTime)
	}
}

func TestPlayTick_NilOutAnglesIsOK(t *testing.T) {
	var state client.State
	tick := &DemoTick{ViewAngles: [3]float32{1, 2, 3}, Message: []byte{nopByte}}
	if err := PlayTick(&state, tick, nil, DefaultPlayerOpts()); err != nil {
		t.Fatalf("PlayTick(nil angles): %v", err)
	}
}

// ----- PlayTick guards -------------------------------------------------------

func TestPlayTick_NilStateRejected(t *testing.T) {
	tick := &DemoTick{Message: []byte{nopByte}}
	if err := PlayTick(nil, tick, nil, DefaultPlayerOpts()); !errors.Is(err, ErrPlayerNilState) {
		t.Fatalf("err = %v want ErrPlayerNilState", err)
	}
}

func TestPlayTick_NilTickRejected(t *testing.T) {
	var state client.State
	if err := PlayTick(&state, nil, nil, DefaultPlayerOpts()); !errors.Is(err, ErrPlayerNilTick) {
		t.Fatalf("err = %v want ErrPlayerNilTick", err)
	}
}

// ----- PlayTick unknown-svc handling ----------------------------------------

func TestPlayTick_UnknownSvc_SkipSwallows(t *testing.T) {
	var state client.State
	tick := &DemoTick{Message: []byte{unknownSvcOpcode, nopByte}}

	opts := DefaultPlayerOpts()
	opts.SkipUnknownSvc = true
	if err := PlayTick(&state, tick, nil, opts); err != nil {
		t.Fatalf("PlayTick: %v", err)
	}
	// Delta still advanced exactly once.
	if math.Abs(float64(state.MsgTime-1.0/20.0)) > 1e-6 {
		t.Errorf("MsgTime = %v want 0.05", state.MsgTime)
	}
}

func TestPlayTick_UnknownSvc_StrictPropagates(t *testing.T) {
	var state client.State
	tick := &DemoTick{Message: []byte{unknownSvcOpcode}}

	err := PlayTick(&state, tick, nil, DefaultPlayerOpts())
	if !errors.Is(err, client.ErrUnknownSvc) {
		t.Fatalf("err = %v want client.ErrUnknownSvc", err)
	}
}

// ----- PlayTick corrupt-body propagation ------------------------------------

func TestPlayTick_CorruptSvcSoundPropagates(t *testing.T) {
	var state client.State
	// SvcSound cmd byte with no body bytes -> decodeSound's first
	// ReadU8 trips Bad -> ErrCorruptMessage.
	tick := &DemoTick{Message: []byte{byte(protocol.SvcSound)}}

	err := PlayTick(&state, tick, nil, DefaultPlayerOpts())
	if !errors.Is(err, client.ErrCorruptMessage) {
		t.Fatalf("err = %v want client.ErrCorruptMessage", err)
	}
}

// ----- PlayTick Apply-error propagation -------------------------------------

func TestPlayTick_ApplyErrorPropagates(t *testing.T) {
	// SignonNum stage 4 from StateDisconnected triggers MarkSpawned,
	// which rejects the transition -> Apply returns ErrApplyBadState.
	var state client.State
	tick := &DemoTick{Message: []byte{byte(protocol.SvcSignonNum), 0x04}}

	err := PlayTick(&state, tick, nil, DefaultPlayerOpts())
	if !errors.Is(err, client.ErrApplyBadState) {
		t.Fatalf("err = %v want client.ErrApplyBadState", err)
	}
}

// ----- Play happy / sad paths ------------------------------------------------

func TestPlay_HappyPathThreeTicks(t *testing.T) {
	var state client.State
	ticks := []DemoTick{
		{ViewAngles: [3]float32{1, 0, 0}, Message: []byte{nopByte}},
		{ViewAngles: [3]float32{2, 0, 0}, Message: append([]byte{nopByte}, signonStageOneBody...)},
		{ViewAngles: [3]float32{3, 0, 0}, Message: []byte{nopByte}},
	}
	var angles [3]float32

	n, err := Play(&state, ticks, &angles, 100.0, DefaultPlayerOpts())
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d want 3", n)
	}
	if angles != ([3]float32{3, 0, 0}) {
		t.Errorf("angles = %v want last tick's angles", angles)
	}
	// 100 + 3 * 1/20 = 100.15. Three sequential float32 additions of
	// 0.05 (which is not representable exactly) drift past 1e-6, so
	// use a coarser tolerance befitting the cumulative round-off.
	want := float32(100.0) + 3*(1.0/20.0)
	if math.Abs(float64(state.MsgTime-want)) > 1e-3 {
		t.Errorf("MsgTime = %v want %v", state.MsgTime, want)
	}
}

// TestPlayTick_ZeroTickDeltaUsesDefault drives the "TickDelta == 0
// -> use 1/20s" fallback inside PlayTick directly (the Play happy-
// path test goes through Play, which doesn't exercise the same
// branch when callers construct opts by hand).
func TestPlayTick_ZeroTickDeltaUsesDefault(t *testing.T) {
	var state client.State
	tick := &DemoTick{Message: []byte{nopByte}}

	opts := PlayerOpts{Protocol: protocol.VersionNQ, TickDelta: 0}
	if err := PlayTick(&state, tick, nil, opts); err != nil {
		t.Fatalf("PlayTick: %v", err)
	}
	if math.Abs(float64(state.MsgTime-1.0/20.0)) > 1e-6 {
		t.Errorf("MsgTime = %v want 0.05 (default delta)", state.MsgTime)
	}
}

func TestPlay_SecondTickMalformedReturnsPartialCount(t *testing.T) {
	var state client.State
	ticks := []DemoTick{
		{Message: []byte{nopByte}},
		// Truncated SvcSound -> ErrCorruptMessage on the second tick.
		{Message: []byte{byte(protocol.SvcSound)}},
		{Message: []byte{nopByte}},
	}

	n, err := Play(&state, ticks, nil, 0, DefaultPlayerOpts())
	if !errors.Is(err, client.ErrCorruptMessage) {
		t.Fatalf("err = %v want client.ErrCorruptMessage", err)
	}
	if n != 1 {
		t.Errorf("count = %d want 1", n)
	}
}

func TestPlay_NilStateRejected(t *testing.T) {
	n, err := Play(nil, nil, nil, 0, DefaultPlayerOpts())
	if !errors.Is(err, ErrPlayerNilState) {
		t.Fatalf("err = %v want ErrPlayerNilState", err)
	}
	if n != 0 {
		t.Errorf("count = %d want 0", n)
	}
}

func TestPlay_EmptyTicksReturnsZero(t *testing.T) {
	var state client.State
	n, err := Play(&state, nil, nil, 42.0, DefaultPlayerOpts())
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d want 0", n)
	}
	if state.MsgTime != 42.0 {
		t.Errorf("MsgTime = %v want 42.0 (start time stamped)", state.MsgTime)
	}
}

// ----- DefaultPlayerOpts drift detector --------------------------------------

func TestDefaultPlayerOpts_Defaults(t *testing.T) {
	got := DefaultPlayerOpts()
	want := PlayerOpts{
		Protocol:       protocol.VersionNQ,
		TickDelta:      1.0 / 20.0,
		SkipUnknownSvc: false,
	}
	if got != want {
		t.Errorf("DefaultPlayerOpts() = %+v want %+v", got, want)
	}
}
