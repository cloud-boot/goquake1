// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package anorms

import (
	"math"
	"testing"
)

func TestCountInvariant(t *testing.T) {
	if Count != 162 {
		t.Errorf("Count drift: got %d want 162 (NUMVERTEXNORMALS)", Count)
	}
	if len(Table) != Count {
		t.Errorf("Table length: got %d want %d", len(Table), Count)
	}
}

// Every entry must be a unit vector (length 1.0 to within float32
// quantisation noise). The upstream's anorms.h was generated from a
// unit-sphere subdivision so this is the load-bearing invariant
// the renderer's "x*lightDir" dot product depends on.
func TestEveryEntryIsUnitLength(t *testing.T) {
	for i, n := range Table {
		lenSq := float64(n[0])*float64(n[0]) + float64(n[1])*float64(n[1]) + float64(n[2])*float64(n[2])
		length := math.Sqrt(lenSq)
		if math.Abs(length-1.0) > 1e-5 {
			t.Errorf("entry %d (%v %v %v): length %v, expected 1.0", i, n[0], n[1], n[2], length)
		}
	}
}

// Spot-check a few known entries against the upstream table -- if
// any of these drift, demos diverge.
func TestKnownEntries(t *testing.T) {
	if Table[0] != [3]float32{-0.525731, 0.000000, 0.850651} {
		t.Errorf("entry 0: %v", Table[0])
	}
	if Table[1] != [3]float32{-0.442863, 0.238856, 0.864188} {
		t.Errorf("entry 1: %v", Table[1])
	}
	// Last entry per the upstream file order.
	if Table[161] != [3]float32{-0.688191, -0.587785, -0.425325} {
		t.Errorf("entry 161: %v", Table[161])
	}
}

// No two entries are identical -- the table is a sphere subdivision
// with unique points.
func TestNoDuplicates(t *testing.T) {
	seen := make(map[[3]float32]int, Count)
	for i, n := range Table {
		if j, ok := seen[n]; ok {
			t.Errorf("duplicate entry: %d and %d are both %v", j, i, n)
		}
		seen[n] = i
	}
}
