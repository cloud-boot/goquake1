// Package embedmusic embeds the optional Quake CD music tracks
// (track02.ogg .. track11.ogg) into the binary so the runtime can
// stream them through the music subsystem without a host filesystem.
//
// The .ogg files are NOT committed to git -- they are sourced
// separately from the user's own Quake archive (e.g. the id1/music/
// directory inside the Quake.zip distributable on archive.org). Tiny
// placeholder files are committed so //go:embed compiles even on a
// fresh checkout; replacing them with real OGG Vorbis bytes makes the
// music play. With placeholders, the music loader sees a malformed
// stream + silently skips (graceful-degradation path in
// music.Streamer).
//
// Layout: track02.ogg through track11.ogg, mirroring vanilla Quake's
// CDA track numbering (track 1 is the data track on the original CD;
// music starts at track 2).
package embedmusic

import "embed"

//go:embed track02.ogg track03.ogg track04.ogg track05.ogg track06.ogg track07.ogg track08.ogg track09.ogg track10.ogg track11.ogg
var MusicFS embed.FS
