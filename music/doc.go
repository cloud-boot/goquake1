// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package music streams the per-map background score the server
// broadcasts via svc_cdtrack (the wire byte the 1996 Q1 retail used
// to address audio-CD tracks 2..11; modern source ports redirected
// it to "music/trackXX.ogg" files inside the pak). The package
// decodes the OGG/Vorbis bitstream into mono/stereo int16 frames the
// runloop's audio mixer accumulates alongside the per-tic SFX, and
// handles loop-back when the active track reaches EOF.
//
// Design seams (so the music subsystem is testable without an OGG
// fixture):
//
//   - [Decoder] is the per-track decode contract. The production
//     factory [NewVorbisDecoder] wraps the pure-Go
//     github.com/jfreymuth/oggvorbis reader; tests inject a fake
//     decoder via [DecoderFactory] to exercise the streamer +
//     loop-back logic without round-tripping through a real OGG.
//
//   - [OpenFunc] is the pak-agnostic asset resolver. Production
//     callers wire a closure over their pak0.pak (or any other
//     fs.FS); tests pass an in-memory map. The function returns
//     ([]byte, false) when the named track is missing so the embedder
//     degrades silently (the .ogg files are NOT distributed with the
//     shareware pak; missing-music must be a tolerated bring-up
//     state, not a hard fail).
//
//   - [Streamer.NextSamples] writes int16 stereo frames (matching the
//     mixer's [sound.StereoSample] shape) into a caller-owned buffer;
//     on EOF the streamer re-opens [Streamer.LoopTrack] (typically
//     the same track for a self-looping background score; 0 = stop).
//
// tyrquake: CL_ParseCDTrack -> CDAudio_Play / BGM_PlayCDtrack chain
// in cd_*.c. The Go port replaces the CD-DA backend with a pure-Go
// OGG streamer + mixer hand-off; the wire shape + the per-track
// loop semantics are preserved verbatim.
package music
