// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasmbox

import (
	"testing"

	"github.com/go-quake1/engine/sound"
)

func TestResampleNearest_emptyInput(t *testing.T) {
	l, r := ResampleNearest(nil, 11025, 44100)
	if len(l) != 0 || len(r) != 0 {
		t.Fatalf("empty src: got len %d/%d want 0/0", len(l), len(r))
	}
}

func TestResampleNearest_zeroSrcRate(t *testing.T) {
	l, r := ResampleNearest([]sound.StereoSample{{L: 1, R: 2}}, 0, 44100)
	if len(l) != 0 || len(r) != 0 {
		t.Fatalf("zero srcRate: got len %d/%d want 0/0", len(l), len(r))
	}
}

func TestResampleNearest_zeroDstRate(t *testing.T) {
	l, r := ResampleNearest([]sound.StereoSample{{L: 1, R: 2}}, 11025, 0)
	if len(l) != 0 || len(r) != 0 {
		t.Fatalf("zero dstRate: got len %d/%d want 0/0", len(l), len(r))
	}
}

func TestResampleNearest_equalRatePassthrough(t *testing.T) {
	src := []sound.StereoSample{
		{L: 32767, R: -32768},
		{L: 0, R: 16384},
	}
	l, r := ResampleNearest(src, 44100, 44100)
	if len(l) != 2 || len(r) != 2 {
		t.Fatalf("equal rate: got len %d/%d want 2/2", len(l), len(r))
	}
	// 32767 / 32768 ≈ 0.99997
	if l[0] < 0.999 || l[0] > 1.0 {
		t.Fatalf("l[0]: got %v", l[0])
	}
	if r[0] != -1.0 {
		t.Fatalf("r[0]: got %v want -1.0", r[0])
	}
}

func TestResampleNearest_upsample(t *testing.T) {
	src := []sound.StereoSample{
		{L: 1, R: 2},
		{L: 3, R: 4},
	}
	// 1 → 4 ratio: every input frame should be repeated 4 times.
	l, r := ResampleNearest(src, 11025, 44100)
	if len(l) != 8 || len(r) != 8 {
		t.Fatalf("upsample: got len %d/%d want 8/8", len(l), len(r))
	}
	wantLIdx := []int{0, 0, 0, 0, 1, 1, 1, 1}
	for i, j := range wantLIdx {
		expected := float32(src[j].L) / 32768.0
		if l[i] != expected {
			t.Errorf("l[%d]: got %v want %v", i, l[i], expected)
		}
	}
}

func TestResampleNearest_downsample(t *testing.T) {
	src := []sound.StereoSample{
		{L: 1, R: 0}, {L: 2, R: 0}, {L: 3, R: 0}, {L: 4, R: 0},
	}
	// 4 → 2 ratio (44100 → 22050): keeps frames 0 + 2.
	l, _ := ResampleNearest(src, 44100, 22050)
	if len(l) != 2 {
		t.Fatalf("downsample: got len %d want 2", len(l))
	}
	if l[0] != 1.0/32768.0 || l[1] != 3.0/32768.0 {
		t.Fatalf("downsample values: got %v,%v", l[0], l[1])
	}
}
