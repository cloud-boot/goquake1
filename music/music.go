// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package music

import (
	"bytes"
	"errors"
	"io"

	"github.com/go-quake1/engine/sound"
	"github.com/jfreymuth/oggvorbis"
)

// DefaultVolume is the per-tic mix scale Streamer applies to the
// decoded PCM by default. Range is [0.0, 1.0] (1.0 = full master
// amplitude). The 0.7 default leaves headroom for SFX accumulation
// without clipping; the runloop's mixer accumulates int16 values
// from every active sound, and the music stream is typically the
// loudest continuous source so a sub-unity scale keeps the sum
// inside the int16 range when explosions overlap.
const DefaultVolume = 0.7

// PadPath is the canonical "music/track%02d.ogg" path the OpenFunc
// resolver should consult. Exposed so embedders can build matching
// strings without re-encoding the convention; the formatting itself
// (fmt.Sprintf("music/track%02d.ogg", track)) lives in the embedder
// because the music package has no fmt dependency.
const PathPrefix = "music/track"

// PathSuffix completes the canonical music path. The embedder builds
// the full path as PathPrefix + zero-padded 2-digit track number +
// PathSuffix.
const PathSuffix = ".ogg"

// Errors surfaced by [LoadTrack] and the streamer.
var (
	// ErrTrackMissing fires when the OpenFunc resolver reports the
	// named track is not present in the asset source (the typical
	// shareware bring-up state -- the .ogg files are NOT distributed
	// with pak0). Callers should log + degrade to silent operation.
	ErrTrackMissing = errors.New("music: track not in asset source")

	// ErrDecoderFailed wraps a decoder constructor failure (e.g. the
	// resolved blob is not a valid OGG container). Includes the
	// underlying error via errors.Unwrap.
	ErrDecoderFailed = errors.New("music: decoder constructor failed")

	// ErrNilOpen is returned by LoadTrack when the OpenFunc resolver
	// is nil. The streamer cannot fetch its own blob, so a nil
	// resolver is a programmer error rather than a degradable state.
	ErrNilOpen = errors.New("music: nil OpenFunc")

	// ErrNilDecoderFactory is returned by LoadTrack when the
	// DecoderFactory is nil. Same rationale as ErrNilOpen.
	ErrNilDecoderFactory = errors.New("music: nil DecoderFactory")
)

// Decoder is the per-track decode contract the [Streamer] consumes.
// Implementations decode an interleaved-by-channel float32 PCM stream
// in the [-1.0, +1.0] range. The streamer is responsible for
// channel-down-mixing (decoded mono / stereo / multi-channel into the
// mixer's stereo target) and for the int16 conversion.
//
// The production implementation wraps github.com/jfreymuth/oggvorbis
// (see [NewVorbisDecoder]); tests inject a fake decoder via
// [DecoderFactory] to exercise the streamer + loop logic.
type Decoder interface {
	// Read writes interleaved-by-channel float32 PCM into p, returning
	// the number of floats written + an error. Mirrors the
	// io.Reader([]byte) contract: io.EOF marks end-of-stream (with
	// possibly-positive n on the same call), other errors are fatal
	// for this decoder instance.
	Read(p []float32) (int, error)
	// SampleRate returns the source-side sample rate in Hz (e.g.
	// 44100). The streamer carries the value forward so the mixer +
	// backend can configure their hardware match.
	SampleRate() int
	// Channels returns the number of interleaved channels per frame
	// in the float32 output (1 = mono, 2 = stereo). Streams with more
	// than 2 channels are tolerated by averaging surplus channels
	// down to the L/R pair.
	Channels() int
}

// DecoderFactory builds a [Decoder] from a raw asset blob. The
// production factory is [NewVorbisDecoder]; tests inject a fake.
//
// The factory returns the underlying decoder error verbatim so the
// streamer can wrap it with context; nil + nil error is treated as
// ErrDecoderFailed (the factory MUST return either a valid decoder
// or an error).
type DecoderFactory func(blob []byte) (Decoder, error)

// OpenFunc is the pak-agnostic asset resolver. Returns the raw OGG
// blob for the named track number, or (nil, false) when the track
// is not in the asset source.
//
// `track` is the wire-byte the server sent (0..255; matches
// [client.State.MusicTrack]); the resolver decides how to map that
// to a filename (the canonical form is
// fmt.Sprintf("music/track%02d.ogg", track), but embedders are free
// to remap -- e.g. honour `forcetrack` cvars, or fall back to
// alternate paths).
type OpenFunc func(track int) (blob []byte, ok bool)

// Streamer is one active music playback context. Owns the per-track
// [Decoder] + the floats-to-int16 conversion buffer + the loop
// bookkeeping.
//
// Safe to construct via [LoadTrack]; do not instantiate manually
// (the zero value has neither a decoder nor a resolver and every
// call would panic on the nil decoder).
type Streamer struct {
	track       int            // currently-playing track index
	loopTrack   int            // EOF fallback track (0 = stop)
	open        OpenFunc       // asset resolver (kept for re-open at EOF)
	makeDecoder DecoderFactory // decoder factory (kept for re-open at EOF)
	decoder     Decoder        // active decoder; nil after a stop
	volume      float32        // per-stream mix scale, [0.0, 1.0]

	// floatBuf is the scratch buffer Read float32 samples land in
	// before the int16 conversion. Sized to 2 * one stereo frame so
	// a single decoder.Read call covers one mixer frame regardless
	// of channel layout (mono / stereo / 5.1 etc.).
	floatBuf []float32
}

// LoadTrack opens `track` via the OpenFunc resolver, hands the blob
// to the DecoderFactory, and returns a ready-to-stream Streamer.
//
// On a missing track (resolver returns (nil, false)) the function
// returns (nil, ErrTrackMissing); the embedder is expected to log
// the missing-music event and continue with no streamer wired (the
// runloop's audio mix path tolerates a nil [Streamer]).
//
// On a decoder construction failure the underlying error is wrapped
// in ErrDecoderFailed so callers can errors.Is(err, ErrDecoderFailed)
// and recover the cause via errors.Unwrap.
//
// `loopTrack` is the EOF fallback; passing 0 stops the stream at
// EOF, any non-zero value re-opens that track. Caller-supplied volume
// defaults to [DefaultVolume] when zero (any explicit non-zero value
// is honoured; pass DefaultVolume verbatim if that is what you want).
func LoadTrack(open OpenFunc, makeDecoder DecoderFactory, track, loopTrack int, volume float32) (*Streamer, error) {
	if open == nil {
		return nil, ErrNilOpen
	}
	if makeDecoder == nil {
		return nil, ErrNilDecoderFactory
	}
	blob, ok := open(track)
	if !ok {
		return nil, ErrTrackMissing
	}
	dec, err := makeDecoder(blob)
	if err != nil {
		return nil, joinErr(ErrDecoderFailed, err)
	}
	if dec == nil {
		return nil, ErrDecoderFailed
	}
	if volume == 0 {
		volume = DefaultVolume
	}
	return &Streamer{
		track:       track,
		loopTrack:   loopTrack,
		open:        open,
		makeDecoder: makeDecoder,
		decoder:     dec,
		volume:      volume,
	}, nil
}

// Track returns the track index currently feeding the streamer.
func (s *Streamer) Track() int { return s.track }

// LoopTrack returns the EOF fallback track.
func (s *Streamer) LoopTrack() int { return s.loopTrack }

// Volume returns the per-stream mix scale.
func (s *Streamer) Volume() float32 { return s.volume }

// SetVolume clamps to [0.0, 1.0] and updates the per-stream mix
// scale. A subsequent NextSamples call applies the new value.
func (s *Streamer) SetVolume(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.volume = v
}

// SampleRate returns the active decoder's source-side sample rate
// in Hz. Returns 0 after the streamer has been stopped (decoder ==
// nil) so callers can probe for liveness.
func (s *Streamer) SampleRate() int {
	if s.decoder == nil {
		return 0
	}
	return s.decoder.SampleRate()
}

// Channels returns the active decoder's channel count. Returns 0
// after a stop.
func (s *Streamer) Channels() int {
	if s.decoder == nil {
		return 0
	}
	return s.decoder.Channels()
}

// Stopped reports whether the streamer has run out of input AND has
// no loop fallback. Once stopped, NextSamples is a constant (0, nil)
// return.
func (s *Streamer) Stopped() bool { return s.decoder == nil }

// NextSamples writes up to len(buf) stereo int16 frames into buf,
// returning the number of frames written. The mixer accumulator
// expects (L, R) int16 pairs scaled to a master 1<<14-ish range
// (matches sound.Paint's 16-bit mix path); NextSamples produces
// values in approximately [-32768, +32767] after the volume scale.
//
// On EOF the streamer re-opens [LoopTrack] (if non-zero) via the
// stored OpenFunc + DecoderFactory and continues seamlessly into the
// new track. A loop re-open that fails (track missing OR decoder
// constructor errors) STOPS the streamer (decoder = nil) and any
// subsequent NextSamples call returns (0, nil); the embedder polls
// [Stopped] to detect this.
//
// Returns the number of frames actually written. A short write is
// possible at EOF when the loop fallback either succeeds (the
// remaining slots are filled from the new track) OR fails (the
// remaining slots stay zeroed; the caller's accumulator already had
// them at zero from its per-tic reset).
//
// The returned error is always nil: stream-end is observable via
// Stopped(), and decoder Read errors are also treated as end-of-stream
// (so transient bitstream corruption gracefully degrades to silence
// rather than crashing the runloop).
func (s *Streamer) NextSamples(buf []sound.StereoSample) int {
	if s.decoder == nil || len(buf) == 0 {
		return 0
	}
	written := 0
	for written < len(buf) {
		need := len(buf) - written
		channels := s.decoder.Channels()
		if channels < 1 {
			// Defensive: a decoder that reports zero channels is a
			// programmer error (every valid Vorbis stream has 1..255
			// channels); treat as immediate end-of-stream.
			s.stopOrLoop()
			if s.decoder == nil {
				return written
			}
			continue
		}
		// Re-size the scratch buffer so one decoder.Read fills exactly
		// `need` output frames. need * channels covers the worst case
		// (stereo doubles the floats per output frame; surround formats
		// are averaged down so the float-count budget still pegs at
		// need * channels).
		want := need * channels
		if cap(s.floatBuf) < want {
			s.floatBuf = make([]float32, want)
		}
		s.floatBuf = s.floatBuf[:want]

		n, err := s.decoder.Read(s.floatBuf)
		// Convert the floats we actually got into stereo int16 frames.
		// n is in float units; frames produced = n / channels.
		if n > 0 {
			frames := n / channels
			if frames > need {
				frames = need
			}
			s.mixInto(buf[written:written+frames], s.floatBuf[:frames*channels], channels)
			written += frames
		}
		if err != nil {
			// EOF or any decoder error -> attempt to loop. If looping
			// fails (decoder == nil after stopOrLoop), bail out with
			// whatever we already wrote.
			s.stopOrLoop()
			if s.decoder == nil {
				return written
			}
		}
	}
	return written
}

// stopOrLoop closes the active decoder slot and, when LoopTrack is
// non-zero, attempts to re-open the loop fallback via the stored
// resolver + factory. On any failure the streamer ends silently
// (decoder = nil + Stopped() == true).
func (s *Streamer) stopOrLoop() {
	s.decoder = nil
	if s.loopTrack == 0 {
		return
	}
	blob, ok := s.open(s.loopTrack)
	if !ok {
		return
	}
	dec, err := s.makeDecoder(blob)
	if err != nil || dec == nil {
		return
	}
	s.decoder = dec
	s.track = s.loopTrack
}

// mixInto converts `floats` (channels * len(dst) values, interleaved
// by channel) into `dst` stereo int16 frames, scaling by the
// per-stream volume.
//
// Channel layout handling:
//
//   - 1 channel  (mono):   sample -> both L and R
//   - 2 channels (stereo): L, R as-is
//   - >= 3 channels:       average the first half into L, the second
//     half into R (a coarse downmix; surround Vorbis is rare for
//     game music and a full ITU downmix matrix would be overkill).
func (s *Streamer) mixInto(dst []sound.StereoSample, floats []float32, channels int) {
	scale := float32(32767) * s.volume
	switch channels {
	case 1:
		for i := range dst {
			v := clamp16(floats[i] * scale)
			dst[i] = sound.StereoSample{L: v, R: v}
		}
	case 2:
		for i := range dst {
			l := clamp16(floats[2*i] * scale)
			r := clamp16(floats[2*i+1] * scale)
			dst[i] = sound.StereoSample{L: l, R: r}
		}
	default:
		// Multi-channel: split-and-average. Half-rounded-up channels
		// feed L, the rest feed R (matches the convention used by
		// QuakeSpasm's BGM_OGGVorbis_Update).
		half := (channels + 1) / 2
		for i := range dst {
			off := i * channels
			var lSum, rSum float32
			for c := 0; c < half; c++ {
				lSum += floats[off+c]
			}
			for c := half; c < channels; c++ {
				rSum += floats[off+c]
			}
			l := clamp16(lSum / float32(half) * scale)
			r := clamp16(rSum / float32(channels-half) * scale)
			dst[i] = sound.StereoSample{L: l, R: r}
		}
	}
}

// clamp16 saturates a float to int16. Vorbis decoders nominally
// stay inside [-1.0, +1.0], but transient clipping can push slightly
// past; the saturation guarantees no int16 wrap-around.
func clamp16(f float32) int16 {
	if f >= 32767 {
		return 32767
	}
	if f <= -32768 {
		return -32768
	}
	return int16(f)
}

// joinErr glues two errors into a single value that satisfies
// errors.Is(_, parent) AND errors.Unwrap(_) == child. Avoids the
// fmt dependency the rest of the package is free of so the binary
// stays small under tamago.
type wrappedErr struct {
	parent error
	child  error
}

func (w *wrappedErr) Error() string {
	return w.parent.Error() + ": " + w.child.Error()
}
func (w *wrappedErr) Is(target error) bool { return target == w.parent }
func (w *wrappedErr) Unwrap() error        { return w.child }

func joinErr(parent, child error) error { return &wrappedErr{parent: parent, child: child} }

// NewVorbisDecoder is the production [DecoderFactory] for OGG/Vorbis
// (the format every modern Q1 source port uses for its music/track*.ogg
// payload). Wraps github.com/jfreymuth/oggvorbis -- the pure-Go
// (no-CGO, tamago-safe) Vorbis decoder.
//
// Usage:
//
//	streamer, err := music.LoadTrack(
//	    func(track int) ([]byte, bool) { ... pak lookup ... },
//	    music.NewVorbisDecoder,
//	    cdtrack, looptrack, music.DefaultVolume)
func NewVorbisDecoder(blob []byte) (Decoder, error) {
	r, err := oggvorbis.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	return &vorbisDecoder{r: r}, nil
}

// vorbisDecoder adapts the *oggvorbis.Reader API to the package's
// [Decoder] contract. The upstream Read returns interleaved-by-channel
// float32 PCM matching our shape verbatim.
type vorbisDecoder struct {
	r *oggvorbis.Reader
}

func (d *vorbisDecoder) Read(p []float32) (int, error) { return d.r.Read(p) }
func (d *vorbisDecoder) SampleRate() int               { return d.r.SampleRate() }
func (d *vorbisDecoder) Channels() int                 { return d.r.Channels() }

// Ensure the adapter satisfies the io.Reader-like contract we hand
// to NextSamples; compile-time guard so future Decoder signature
// changes flag the adapter too.
var _ Decoder = (*vorbisDecoder)(nil)

// Ensure the canonical "EOF is io.EOF" contract still holds on this
// path even after the package strips the io dependency from its
// surface area: the streamer's NextSamples treats ANY error as an
// end-of-stream signal so any io.EOF / io.ErrUnexpectedEOF / decoder-
// internal error gracefully degrades to a loop or stop.
var _ = io.EOF
