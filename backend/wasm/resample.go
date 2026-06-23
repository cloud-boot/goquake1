// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasm

import "github.com/go-quake1/engine/sound"

// ResampleNearest performs nearest-neighbor stereo resampling from
// srcRate to dstRate. The result is two parallel float32 channel
// buffers (L, R) in the [-1, 1] WebAudio convention.
//
// Algorithm: for output frame i (0..outFrames-1), pick the input
// frame at index floor(i * srcRate / dstRate). This is the cheapest
// rate-conversion that preserves the time base; quality is adequate
// for game SFX at the 11025 → 44100 / 48000 ratios we hit in
// practice. A higher-quality linear or polyphase resampler is a
// follow-up.
//
// Inputs:
//
//   - src: stereo input frames, NOT bytes
//   - srcRate: input sample rate in Hz (typically 11025)
//   - dstRate: output sample rate in Hz (typically 44100 or 48000)
//
// Returns:
//
//   - left, right: two float32 slices, each outFrames long, where
//     outFrames = ceil(len(src) * dstRate / srcRate). Returns two
//     empty slices when src is empty, srcRate <= 0, or dstRate <= 0.
//
// The int16 → float32 conversion divides by 32768 (the negative-side
// magnitude) so the -32768..32767 range maps to roughly -1..0.99997;
// this matches the convention every WebAudio sample loader uses.
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
	// Ceil(len(src) * dstRate / srcRate) without overflowing int.
	outFrames := (len(src)*dstRate + srcRate - 1) / srcRate
	left = make([]float32, outFrames)
	right = make([]float32, outFrames)
	// With outFrames = ceil(len(src) * dstRate / srcRate), every
	// output index i in [0, outFrames) satisfies
	//   floor(i * srcRate / dstRate) < len(src)
	// so no clamp is needed on j; see the package doc for the
	// arithmetic. Keep the math tight so the inner loop is one
	// multiply + one divide + two int16→float32 conversions.
	for i := 0; i < outFrames; i++ {
		j := i * srcRate / dstRate
		left[i] = float32(src[j].L) / 32768.0
		right[i] = float32(src[j].R) / 32768.0
	}
	return left, right
}
