// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasmbox

import "github.com/go-quake1/engine/sound"

// ResampleNearest performs nearest-neighbor stereo resampling from
// srcRate to dstRate. The result is two parallel float32 channel
// buffers (L, R) in the [-1, 1] WebAudio convention.
//
// Algorithm matches backend/wasm.ResampleNearest exactly — kept as a
// per-package copy to keep the two backends decoupled (so backend/wasm
// can change one without dragging the other).
func ResampleNearest(src []sound.StereoSample, srcRate, dstRate int) (left, right []float32) {
	if len(src) == 0 || srcRate <= 0 || dstRate <= 0 {
		return []float32{}, []float32{}
	}
	if srcRate == dstRate {
		left = make([]float32, len(src))
		right = make([]float32, len(src))
		for i, s := range src {
			left[i] = float32(s.L) / 32768.0
			right[i] = float32(s.R) / 32768.0
		}
		return left, right
	}
	outFrames := (len(src)*dstRate + srcRate - 1) / srcRate
	left = make([]float32, outFrames)
	right = make([]float32, outFrames)
	for i := 0; i < outFrames; i++ {
		j := i * srcRate / dstRate
		left[i] = float32(src[j].L) / 32768.0
		right[i] = float32(src[j].R) / 32768.0
	}
	return left, right
}
