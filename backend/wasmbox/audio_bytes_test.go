// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasmbox

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestFloat32SliceAsBytes_empty(t *testing.T) {
	if got := float32SliceAsBytes(nil); got != nil {
		t.Fatalf("nil input: got %v want nil", got)
	}
	if got := float32SliceAsBytes([]float32{}); got != nil {
		t.Fatalf("empty input: got %v want nil", got)
	}
}

func TestFloat32SliceAsBytes_roundTrip(t *testing.T) {
	samples := []float32{1.0, -1.0, 0.5, math.Pi}
	b := float32SliceAsBytes(samples)
	if len(b) != len(samples)*4 {
		t.Fatalf("byte len: got %d want %d", len(b), len(samples)*4)
	}
	// wasm + amd64/arm64 are all little-endian; check the round-trip.
	for i, want := range samples {
		got := math.Float32frombits(binary.LittleEndian.Uint32(b[i*4 : i*4+4]))
		if got != want {
			t.Errorf("sample[%d]: got %v want %v", i, got, want)
		}
	}
}

func TestFloat32SliceAsBytes_aliasesInput(t *testing.T) {
	samples := []float32{1.0}
	b := float32SliceAsBytes(samples)
	// Mutate the input and observe the byte slice change (verifies no-copy).
	samples[0] = 2.0
	got := math.Float32frombits(binary.LittleEndian.Uint32(b[0:4]))
	if got != 2.0 {
		t.Fatalf("alias broken: got %v want 2.0", got)
	}
}
