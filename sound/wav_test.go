// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sound

import (
	"encoding/binary"
	"errors"
	"testing"
)

// makeWav builds a synthetic WAV blob with the supplied parameters.
// For tests only; faithful to the WAV wire shape (RIFF / fmt / data).
func makeWav(channels, sampleRate, bitsPerSam int, body []byte, withCue bool, cueAt int) []byte {
	fmtChunk := make([]byte, 8+16)
	copy(fmtChunk[0:4], []byte{'f', 'm', 't', ' '})
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 16)
	binary.LittleEndian.PutUint16(fmtChunk[8:10], WavFormatPCM)
	binary.LittleEndian.PutUint16(fmtChunk[10:12], uint16(channels))
	binary.LittleEndian.PutUint32(fmtChunk[12:16], uint32(sampleRate))
	binary.LittleEndian.PutUint32(fmtChunk[16:20], uint32(sampleRate*channels*bitsPerSam/8))
	binary.LittleEndian.PutUint16(fmtChunk[20:22], uint16(channels*bitsPerSam/8))
	binary.LittleEndian.PutUint16(fmtChunk[22:24], uint16(bitsPerSam))

	dataChunk := make([]byte, 8+len(body))
	copy(dataChunk[0:4], []byte{'d', 'a', 't', 'a'})
	binary.LittleEndian.PutUint32(dataChunk[4:8], uint32(len(body)))
	copy(dataChunk[8:], body)

	var cueChunk []byte
	if withCue {
		cueChunk = make([]byte, 8+4+24)
		copy(cueChunk[0:4], []byte{'c', 'u', 'e', ' '})
		binary.LittleEndian.PutUint32(cueChunk[4:8], uint32(4+24))
		binary.LittleEndian.PutUint32(cueChunk[8:12], 1) // 1 cue point
		// Cue point: 4 (id) + 4 (position) + 4 (data_chunk_id) +
		// 4 (chunk_start) + 4 (block_start) + 4 (sample_offset)
		binary.LittleEndian.PutUint32(cueChunk[12+20:12+24], uint32(cueAt))
	}

	chunks := append(fmtChunk, dataChunk...)
	chunks = append(chunks, cueChunk...)

	out := make([]byte, 12+len(chunks))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(chunks)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], chunks)
	return out
}

// ----- LoadWav happy paths ------------------------------------------

func TestLoadWav_Happy8bit(t *testing.T) {
	body := []byte{128, 200, 128, 56} // unsigned 8-bit: silence + + + -
	blob := makeWav(1, 11025, 8, body, false, 0)
	s, err := LoadWav("test.wav", blob)
	if err != nil {
		t.Fatalf("LoadWav: %v", err)
	}
	if s.Name != "test.wav" {
		t.Fatalf("Name = %q", s.Name)
	}
	if s.SampleRate != 11025 || s.BitsPerSam != 8 || s.NumSamples != 4 {
		t.Fatalf("metadata: %+v", s)
	}
	// 8-bit conversion: unsigned -> signed (subtract 128).
	want := []byte{0, 72, 0, 184} // 128-128, 200-128, 128-128, 56-128 mod 256
	for i, b := range want {
		if s.Data[i] != b {
			t.Fatalf("Data[%d] = %d want %d", i, int8(s.Data[i]), int8(b))
		}
	}
	if s.LoopStart != -1 {
		t.Fatalf("LoopStart = %d want -1 (no cue)", s.LoopStart)
	}
}

func TestLoadWav_Happy16bit(t *testing.T) {
	body := []byte{0x00, 0x10, 0x00, 0xF0} // two LE int16 samples
	blob := makeWav(1, 22050, 16, body, false, 0)
	s, err := LoadWav("x.wav", blob)
	if err != nil {
		t.Fatalf("LoadWav: %v", err)
	}
	if s.BitsPerSam != 16 || s.NumSamples != 2 {
		t.Fatalf("16-bit metadata: %+v", s)
	}
	// Data is left as-is (signed LE 16-bit already).
	if s.Data[0] != 0x00 || s.Data[1] != 0x10 {
		t.Fatalf("16-bit data not preserved")
	}
}

func TestLoadWav_WithCueLoopPoint(t *testing.T) {
	body := []byte{0, 0, 0, 0}
	blob := makeWav(1, 11025, 8, body, true, 1234)
	s, err := LoadWav("loop.wav", blob)
	if err != nil {
		t.Fatalf("LoadWav: %v", err)
	}
	if s.LoopStart != 1234 {
		t.Fatalf("LoopStart = %d want 1234", s.LoopStart)
	}
}

// ----- LoadWav error paths ------------------------------------------

func TestLoadWav_TooShort(t *testing.T) {
	_, err := LoadWav("", []byte{1, 2, 3})
	if !errors.Is(err, ErrWavShort) {
		t.Fatalf("err = %v want ErrWavShort", err)
	}
}

func TestLoadWav_NotRIFF(t *testing.T) {
	blob := make([]byte, 12)
	copy(blob, []byte("ABCDsizeWAVE"))
	_, err := LoadWav("", blob)
	if !errors.Is(err, ErrWavNotRIFF) {
		t.Fatalf("err = %v want ErrWavNotRIFF", err)
	}
}

func TestLoadWav_NotWAVE(t *testing.T) {
	blob := make([]byte, 12)
	copy(blob, []byte("RIFFsizeABCD"))
	_, err := LoadWav("", blob)
	if !errors.Is(err, ErrWavNotWAVE) {
		t.Fatalf("err = %v want ErrWavNotWAVE", err)
	}
}

func TestLoadWav_MissingFmt(t *testing.T) {
	// Build a RIFF with only a data chunk, no fmt.
	dataChunk := make([]byte, 8+4)
	copy(dataChunk[0:4], []byte{'d', 'a', 't', 'a'})
	binary.LittleEndian.PutUint32(dataChunk[4:8], 4)
	out := make([]byte, 12+len(dataChunk))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(dataChunk)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], dataChunk)
	_, err := LoadWav("", out)
	if !errors.Is(err, ErrWavMissingFmt) {
		t.Fatalf("err = %v want ErrWavMissingFmt", err)
	}
}

func TestLoadWav_FmtTooShort(t *testing.T) {
	// fmt chunk with length 8 (under the minimum 16)
	fmtChunk := make([]byte, 8+8)
	copy(fmtChunk[0:4], []byte{'f', 'm', 't', ' '})
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 8)
	// fill 8 bytes of garbage
	out := make([]byte, 12+len(fmtChunk))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(fmtChunk)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], fmtChunk)
	_, err := LoadWav("", out)
	if !errors.Is(err, ErrWavMissingFmt) {
		t.Fatalf("fmt-short err = %v want ErrWavMissingFmt", err)
	}
}

func TestLoadWav_MissingData(t *testing.T) {
	// Just fmt, no data
	fmtChunk := make([]byte, 8+16)
	copy(fmtChunk[0:4], []byte{'f', 'm', 't', ' '})
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 16)
	binary.LittleEndian.PutUint16(fmtChunk[8:10], WavFormatPCM)
	binary.LittleEndian.PutUint16(fmtChunk[10:12], 1)
	binary.LittleEndian.PutUint32(fmtChunk[12:16], 11025)
	binary.LittleEndian.PutUint32(fmtChunk[16:20], 11025)
	binary.LittleEndian.PutUint16(fmtChunk[20:22], 1)
	binary.LittleEndian.PutUint16(fmtChunk[22:24], 8)
	out := make([]byte, 12+len(fmtChunk))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(fmtChunk)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], fmtChunk)
	_, err := LoadWav("", out)
	if !errors.Is(err, ErrWavMissingData) {
		t.Fatalf("err = %v want ErrWavMissingData", err)
	}
}

func TestLoadWav_NonPCM(t *testing.T) {
	blob := makeWav(1, 11025, 8, []byte{0}, false, 0)
	// Patch format code to non-PCM (e.g. 2 = ADPCM)
	binary.LittleEndian.PutUint16(blob[20:22], 2) // fmt chunk audio_format
	_, err := LoadWav("", blob)
	if !errors.Is(err, ErrWavBadFormat) {
		t.Fatalf("err = %v want ErrWavBadFormat", err)
	}
}

func TestLoadWav_Stereo(t *testing.T) {
	blob := makeWav(2, 11025, 8, []byte{0, 0}, false, 0)
	_, err := LoadWav("", blob)
	if !errors.Is(err, ErrWavBadChannels) {
		t.Fatalf("err = %v want ErrWavBadChannels", err)
	}
}

func TestLoadWav_BadBitsPerSample(t *testing.T) {
	blob := makeWav(1, 11025, 24, []byte{0, 0, 0}, false, 0) // 24-bit not supported
	_, err := LoadWav("", blob)
	if !errors.Is(err, ErrWavBadBits) {
		t.Fatalf("err = %v want ErrWavBadBits", err)
	}
}

func TestLoadWav_TruncatedChunkTolerated(t *testing.T) {
	// fmt chunk declares size 16 but blob ends earlier inside data chunk.
	// Should still load (data chunk truncated to actual bytes).
	blob := makeWav(1, 11025, 8, []byte{0, 0, 0, 0}, false, 0)
	// Truncate the blob mid-data-chunk-body
	blob = blob[:len(blob)-2]
	s, err := LoadWav("", blob)
	if err != nil {
		t.Fatalf("truncated LoadWav: %v", err)
	}
	if s.NumSamples > 4 {
		t.Fatalf("truncated NumSamples = %d, more than wire", s.NumSamples)
	}
}

func TestLoadWav_OddSizeChunkPadding(t *testing.T) {
	// A chunk with odd body size requires a 1-byte pad to keep
	// the next chunk on an even boundary. Build a manual blob
	// with an unknown 5-byte chunk before fmt to exercise the
	// pad-handling.
	junk := make([]byte, 8+5+1) // 8 header + 5 body + 1 pad
	copy(junk[0:4], []byte{'J', 'U', 'N', 'K'})
	binary.LittleEndian.PutUint32(junk[4:8], 5)
	// pad byte zeroed
	fmtChunk := make([]byte, 8+16)
	copy(fmtChunk[0:4], []byte{'f', 'm', 't', ' '})
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 16)
	binary.LittleEndian.PutUint16(fmtChunk[8:10], WavFormatPCM)
	binary.LittleEndian.PutUint16(fmtChunk[10:12], 1)
	binary.LittleEndian.PutUint32(fmtChunk[12:16], 11025)
	binary.LittleEndian.PutUint32(fmtChunk[16:20], 11025)
	binary.LittleEndian.PutUint16(fmtChunk[20:22], 1)
	binary.LittleEndian.PutUint16(fmtChunk[22:24], 8)
	dataChunk := make([]byte, 8+1)
	copy(dataChunk[0:4], []byte{'d', 'a', 't', 'a'})
	binary.LittleEndian.PutUint32(dataChunk[4:8], 1)

	chunks := append(junk, fmtChunk...)
	chunks = append(chunks, dataChunk...)
	out := make([]byte, 12+len(chunks))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(chunks)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], chunks)

	_, err := LoadWav("", out)
	if err != nil {
		t.Fatalf("odd-pad LoadWav: %v", err)
	}
}

func TestLoadWav_SmallCueIgnored(t *testing.T) {
	// Cue chunk that's too small to hold one cue point -> LoopStart
	// stays at -1.
	fmtChunk := make([]byte, 8+16)
	copy(fmtChunk[0:4], []byte{'f', 'm', 't', ' '})
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 16)
	binary.LittleEndian.PutUint16(fmtChunk[8:10], WavFormatPCM)
	binary.LittleEndian.PutUint16(fmtChunk[10:12], 1)
	binary.LittleEndian.PutUint32(fmtChunk[12:16], 11025)
	binary.LittleEndian.PutUint32(fmtChunk[16:20], 11025)
	binary.LittleEndian.PutUint16(fmtChunk[20:22], 1)
	binary.LittleEndian.PutUint16(fmtChunk[22:24], 8)

	smallCue := make([]byte, 8+4)
	copy(smallCue[0:4], []byte{'c', 'u', 'e', ' '})
	binary.LittleEndian.PutUint32(smallCue[4:8], 4)
	binary.LittleEndian.PutUint32(smallCue[8:12], 0) // 0 cue points

	dataChunk := make([]byte, 8+1)
	copy(dataChunk[0:4], []byte{'d', 'a', 't', 'a'})
	binary.LittleEndian.PutUint32(dataChunk[4:8], 1)

	chunks := append(fmtChunk, smallCue...)
	chunks = append(chunks, dataChunk...)
	out := make([]byte, 12+len(chunks))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(chunks)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], chunks)

	s, err := LoadWav("", out)
	if err != nil {
		t.Fatalf("small-cue LoadWav: %v", err)
	}
	if s.LoopStart != -1 {
		t.Fatalf("small cue: LoopStart = %d want -1", s.LoopStart)
	}
}
