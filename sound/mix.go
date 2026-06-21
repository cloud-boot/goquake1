// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sound

import "errors"

// MixBufferStereoFrames is the working accumulator's frame count.
// One stereo frame = 2 int16s (L + R). tyrquake uses 512 frames in
// paintbuffer; the Go port matches.
const MixBufferStereoFrames = 512

// MaxMixOutputFrames is the per-call ceiling on Paint's output
// length. Anything longer must be batched.
const MaxMixOutputFrames = MixBufferStereoFrames

var (
	// ErrMixOutputTooLarge is returned when numFrames exceeds
	// MaxMixOutputFrames or len(output), or when ClampToInt8's
	// input/output slices disagree in length.
	ErrMixOutputTooLarge = errors.New("sound: paint output length > MaxMixOutputFrames")
	// ErrMixNilPool is returned when Paint is called with a nil pool.
	ErrMixNilPool = errors.New("sound: nil pool in Paint")
	// ErrMixNilOutput is returned when Paint is called with a nil
	// output buffer.
	ErrMixNilOutput = errors.New("sound: nil output buffer in Paint")
	// ErrMixBadFormat is returned when an active channel's sample is
	// not 8-bit signed PCM. The 16-bit mix path is deferred to a
	// later batch (see tyrquake's SND_PaintChannelFrom16).
	ErrMixBadFormat = errors.New("sound: only 8-bit PCM samples supported in this commit")
)

// StereoSample is one (L, R) int16 frame.
type StereoSample struct {
	L, R int16
}

// Paint mixes `numFrames` stereo samples from every active channel
// into `output`. Each output frame is a (L, R) int16 pair. After
// mixing, each channel's Position is advanced by numFrames. Channels
// whose Position reaches EndPos are stopped (Sfx = nil) and skipped
// for the rest of this call.
//
// tyrquake: S_PaintChannels.
//
// Per-channel mix per output frame i:
//
//	left[i]  += sample[ch.Position + i] * ch.LeftVol  / 256
//	right[i] += sample[ch.Position + i] * ch.RightVol / 256
//
// Sample bytes are int8 (signed); the multiply lifts them to int16
// without clipping (max int8 (127) * max vol (255) = 32385 << int16 max).
//
// Caller is responsible for clamping/converting the int16 accumulator
// to whatever the audio backend wants (int8 / int16 / float32). The
// mixer exposes the 16-bit intermediate so backends with > 8-bit
// hardware retain the full dynamic range. Caller is also responsible
// for zeroing `output` between calls; Paint accumulates into existing
// content (matches tyrquake's paintbuffer-then-music-then-transfer
// pipeline).
//
// Returns:
//
//	ErrMixNilPool         pool == nil
//	ErrMixNilOutput       output == nil
//	ErrMixOutputTooLarge  numFrames > MaxMixOutputFrames OR > len(output)
//	ErrMixBadFormat       any active channel's Sfx.BitsPerSam != 8
func Paint(pool *Pool, output []StereoSample, numFrames int) error {
	if pool == nil {
		return ErrMixNilPool
	}
	if output == nil {
		return ErrMixNilOutput
	}
	if numFrames > MaxMixOutputFrames || numFrames > len(output) {
		return ErrMixOutputTooLarge
	}

	// First pass: validate every active channel's sample format
	// before mutating any state. Mismatches at this layer mean the
	// loader handed us a sample type we cannot mix yet (16-bit is
	// deferred); failing fast prevents partial mixes / corrupted
	// position advances.
	for i := range pool.Channels {
		ch := &pool.Channels[i]
		if ch.Free() {
			continue
		}
		if ch.Sfx.BitsPerSam != 8 {
			return ErrMixBadFormat
		}
	}

	for i := range pool.Channels {
		ch := &pool.Channels[i]
		if ch.Free() {
			continue
		}
		// How many frames this channel contributes before it ends.
		// EndPos is the absolute sample index at which playback
		// halts; mix min(remaining, numFrames) samples then let the
		// stop-check below retire the slot if we've consumed it.
		remaining := ch.EndPos - ch.Position
		count := numFrames
		if remaining < count {
			count = remaining
		}

		// Mix `count` samples; sample bytes reinterpreted as int8.
		data := ch.Sfx.Data
		pos := ch.Position
		lv := int32(ch.LeftVol)
		rv := int32(ch.RightVol)
		for j := 0; j < count; j++ {
			s := int32(int8(data[pos+j]))
			output[j].L += int16(s * lv / 256)
			output[j].R += int16(s * rv / 256)
		}

		ch.Position += numFrames
		if ch.Position >= ch.EndPos {
			ch.Stop()
		}
	}
	return nil
}

// ClampToInt8 converts the int16 stereo accumulator to int8 PCM,
// clamping any per-sample value outside [-128, 127] to the boundary.
// Convenience for backends whose hardware output is 8-bit signed PCM
// (Quake's stock DMA path: 8-bit hardware was mono in the late-90s).
//
// Each frame's L and R are averaged into a single int8, so len(out)
// must equal len(in). Returns ErrMixOutputTooLarge on length mismatch.
// tyrquake: the samplebits==8 branch of S_TransferPaintBuffer.
func ClampToInt8(in []StereoSample, out []int8) error {
	if len(in) != len(out) {
		return ErrMixOutputTooLarge
	}
	for i, s := range in {
		// Average L+R for the mono 8-bit path. int32 widen avoids
		// the (L+R) intermediate overflowing int16.
		v := (int32(s.L) + int32(s.R)) / 2
		if v > 127 {
			out[i] = 127
		} else if v < -128 {
			out[i] = -128
		} else {
			out[i] = int8(v)
		}
	}
	return nil
}
