// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build no_embed_assets

package embedmusic

import "embed"

// MusicFS is empty under the no_embed_assets build tag so the binary
// doesn't carry the music tracks. Callers see a 404 from ReadFile +
// route through their fallback path (the engine's music.Streamer
// degrades gracefully on a missing track).
var MusicFS embed.FS
