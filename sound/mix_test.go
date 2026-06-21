// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sound

import (
	"errors"
	"testing"
)

// ----- helpers -----------------------------------------------------

// makeSample8 builds an 8-bit PCM sample whose Data bytes are the
// caller-supplied int8s reinterpreted as bytes.
func makeSample8(name string, samples []int8) *Sample {
	data := make([]byte, len(samples))
	for i, s := range samples {
		data[i] = byte(s)
	}
	return &Sample{
		Name:       name,
		SampleRate: DefaultSampleRate,
		BitsPerSam: 8,
		LoopStart:  -1,
		NumSamples: len(samples),
		Data:       data,
	}
}

// ----- Paint: argument validation ---------------------------------

func TestPaint_NilPool(t *testing.T) {
	out := make([]StereoSample, 4)
	if err := Paint(nil, out, 4); !errors.Is(err, ErrMixNilPool) {
		t.Fatalf("Paint(nil pool) err = %v want ErrMixNilPool", err)
	}
}

func TestPaint_NilOutput(t *testing.T) {
	p, _ := NewPool(0)
	if err := Paint(p, nil, 4); !errors.Is(err, ErrMixNilOutput) {
		t.Fatalf("Paint(nil output) err = %v want ErrMixNilOutput", err)
	}
}

func TestPaint_NumFramesExceedsMax(t *testing.T) {
	p, _ := NewPool(0)
	// Big enough slice so the len(output) check doesn't fire first.
	out := make([]StereoSample, MaxMixOutputFrames+10)
	if err := Paint(p, out, MaxMixOutputFrames+1); !errors.Is(err, ErrMixOutputTooLarge) {
		t.Fatalf("Paint(numFrames>Max) err = %v want ErrMixOutputTooLarge", err)
	}
}

func TestPaint_NumFramesExceedsOutputLen(t *testing.T) {
	p, _ := NewPool(0)
	out := make([]StereoSample, 4)
	if err := Paint(p, out, 5); !errors.Is(err, ErrMixOutputTooLarge) {
		t.Fatalf("Paint(numFrames>len) err = %v want ErrMixOutputTooLarge", err)
	}
}

// ----- Paint: mix semantics ---------------------------------------

func TestPaint_EmptyPoolLeavesOutputUntouched(t *testing.T) {
	p, _ := NewPool(8)
	out := []StereoSample{{L: 11, R: 22}, {L: 33, R: 44}, {L: 55, R: 66}}
	if err := Paint(p, out, 3); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	want := []StereoSample{{11, 22}, {33, 44}, {55, 66}}
	for i, s := range out {
		if s != want[i] {
			t.Fatalf("frame %d = %+v want %+v (empty pool must not touch output)", i, s, want[i])
		}
	}
}

func TestPaint_SingleChannelFullDuration(t *testing.T) {
	p, _ := NewPool(8)
	// 4 samples; per-frame: L = s * 128 / 256 = s/2, R = s * 64 / 256 = s/4.
	samples := []int8{100, -100, 50, -50}
	p.Channels[10] = Channel{
		Sfx:      makeSample8("x", samples),
		Position: 0,
		EndPos:   4,
		LeftVol:  128,
		RightVol: 64,
	}
	out := make([]StereoSample, 4)
	if err := Paint(p, out, 4); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	want := []StereoSample{
		{int16(100 * 128 / 256), int16(100 * 64 / 256)},
		{int16(-100 * 128 / 256), int16(-100 * 64 / 256)},
		{int16(50 * 128 / 256), int16(50 * 64 / 256)},
		{int16(-50 * 128 / 256), int16(-50 * 64 / 256)},
	}
	for i, s := range out {
		if s != want[i] {
			t.Fatalf("frame %d = %+v want %+v", i, s, want[i])
		}
	}
	// numFrames consumed the whole sample -> channel auto-stops.
	if !p.Channels[10].Free() {
		t.Fatalf("channel not auto-stopped after EndPos reached: %+v", p.Channels[10])
	}
}

func TestPaint_SingleChannelEndsMidCall(t *testing.T) {
	p, _ := NewPool(8)
	// 2 samples available; ask for 5 frames. Last 3 frames must be
	// left untouched by this channel (zeros from the initial buffer).
	samples := []int8{40, 60}
	p.Channels[12] = Channel{
		Sfx:      makeSample8("e", samples),
		Position: 0,
		EndPos:   2,
		LeftVol:  256, // s*256/256 = s
		RightVol: 256,
	}
	out := make([]StereoSample, 5)
	if err := Paint(p, out, 5); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	if out[0] != (StereoSample{40, 40}) || out[1] != (StereoSample{60, 60}) {
		t.Fatalf("frames 0/1 = %+v / %+v want {40,40} / {60,60}", out[0], out[1])
	}
	for i := 2; i < 5; i++ {
		if out[i] != (StereoSample{0, 0}) {
			t.Fatalf("frame %d = %+v want {0,0}", i, out[i])
		}
	}
	if !p.Channels[12].Free() {
		t.Fatalf("channel must auto-stop once Position >= EndPos")
	}
}

func TestPaint_TwoChannelsAccumulate(t *testing.T) {
	p, _ := NewPool(8)
	a := []int8{10, 20, 30}
	b := []int8{1, 2, 3}
	p.Channels[8] = Channel{
		Sfx:      makeSample8("a", a),
		Position: 0,
		EndPos:   3,
		LeftVol:  256,
		RightVol: 256,
	}
	p.Channels[9] = Channel{
		Sfx:      makeSample8("b", b),
		Position: 0,
		EndPos:   3,
		LeftVol:  256,
		RightVol: 256,
	}
	out := make([]StereoSample, 3)
	if err := Paint(p, out, 3); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	want := []StereoSample{{11, 11}, {22, 22}, {33, 33}}
	for i, s := range out {
		if s != want[i] {
			t.Fatalf("frame %d = %+v want %+v (two-channel accumulation)", i, s, want[i])
		}
	}
}

func TestPaint_StaticReservedSlotMixesToo(t *testing.T) {
	// The pool reserves slots 0..ReservedStatic-1 for ambient
	// sounds at allocation time; playback (mixing) must cover the
	// whole Channels array. Verify by manually planting a sample
	// in slot 0 and confirming Paint mixes it.
	p, _ := NewPool(8)
	p.Channels[0] = Channel{
		Sfx:      makeSample8("ambient", []int8{77}),
		Position: 0,
		EndPos:   1,
		LeftVol:  256,
		RightVol: 256,
	}
	out := make([]StereoSample, 1)
	if err := Paint(p, out, 1); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	if out[0] != (StereoSample{77, 77}) {
		t.Fatalf("static slot not mixed: %+v want {77,77}", out[0])
	}
}

func TestPaint_BadFormat16BitChannelRejected(t *testing.T) {
	p, _ := NewPool(0)
	p.Channels[5] = Channel{
		Sfx: &Sample{
			Name:       "bad",
			BitsPerSam: 16,
			LoopStart:  -1,
			NumSamples: 2,
			Data:       []byte{0, 0, 0, 0},
		},
		EndPos:   2,
		LeftVol:  128,
		RightVol: 128,
	}
	out := make([]StereoSample, 4)
	if err := Paint(p, out, 4); !errors.Is(err, ErrMixBadFormat) {
		t.Fatalf("Paint(16-bit) err = %v want ErrMixBadFormat", err)
	}
	// Bad-format check must run before any state mutation: position
	// + sfx must be intact for the caller to handle/retry.
	if p.Channels[5].Free() {
		t.Fatalf("bad-format channel must NOT be auto-stopped")
	}
	if p.Channels[5].Position != 0 {
		t.Fatalf("bad-format channel Position mutated: %d", p.Channels[5].Position)
	}
}

// Exercises the int8 sign-extension path: 0xFF byte -> -1 sample,
// not +255. Guards against a future refactor that drops the int8
// reinterpretation.
func TestPaint_SampleBytesAreSigned(t *testing.T) {
	p, _ := NewPool(0)
	// 0xFF == -1 as int8. With vol 256 the per-frame value is -1.
	p.Channels[0] = Channel{
		Sfx:      &Sample{Name: "s", BitsPerSam: 8, LoopStart: -1, NumSamples: 1, Data: []byte{0xFF}},
		EndPos:   1,
		LeftVol:  256,
		RightVol: 256,
	}
	out := make([]StereoSample, 1)
	if err := Paint(p, out, 1); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	if out[0] != (StereoSample{-1, -1}) {
		t.Fatalf("signed sample mishandled: %+v want {-1,-1}", out[0])
	}
}

// ----- ClampToInt8 ------------------------------------------------

func TestClampToInt8_HappyPath(t *testing.T) {
	in := []StereoSample{{50, 70}, {-20, -40}, {0, 0}}
	out := make([]int8, len(in))
	if err := ClampToInt8(in, out); err != nil {
		t.Fatalf("ClampToInt8: %v", err)
	}
	// (50+70)/2 = 60, (-20-40)/2 = -30, 0
	want := []int8{60, -30, 0}
	for i, v := range out {
		if v != want[i] {
			t.Fatalf("out[%d] = %d want %d", i, v, want[i])
		}
	}
}

func TestClampToInt8_ClampHigh(t *testing.T) {
	in := []StereoSample{{30000, 30000}, {200, 200}}
	out := make([]int8, len(in))
	if err := ClampToInt8(in, out); err != nil {
		t.Fatalf("ClampToInt8: %v", err)
	}
	if out[0] != 127 {
		t.Fatalf("high clamp: out[0] = %d want 127", out[0])
	}
	if out[1] != 127 {
		t.Fatalf("high clamp boundary: out[1] = %d want 127", out[1])
	}
}

func TestClampToInt8_ClampLow(t *testing.T) {
	in := []StereoSample{{-30000, -30000}, {-200, -200}}
	out := make([]int8, len(in))
	if err := ClampToInt8(in, out); err != nil {
		t.Fatalf("ClampToInt8: %v", err)
	}
	if out[0] != -128 {
		t.Fatalf("low clamp: out[0] = %d want -128", out[0])
	}
	if out[1] != -128 {
		t.Fatalf("low clamp boundary: out[1] = %d want -128", out[1])
	}
}

func TestClampToInt8_LengthMismatch(t *testing.T) {
	in := []StereoSample{{1, 2}, {3, 4}}
	out := make([]int8, 3) // wrong length
	if err := ClampToInt8(in, out); !errors.Is(err, ErrMixOutputTooLarge) {
		t.Fatalf("len-mismatch err = %v want ErrMixOutputTooLarge", err)
	}
}

// ----- drift detectors --------------------------------------------

func TestMixConstantsAreSane(t *testing.T) {
	if MixBufferStereoFrames != 512 {
		t.Fatalf("MixBufferStereoFrames drift: %d", MixBufferStereoFrames)
	}
	if MaxMixOutputFrames != MixBufferStereoFrames {
		t.Fatalf("MaxMixOutputFrames drift")
	}
}
