// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build !no_embed_assets

package embedmusic

import "embed"

//go:embed track02.ogg track03.ogg track04.ogg track05.ogg track06.ogg track07.ogg track08.ogg track09.ogg track10.ogg track11.ogg
var MusicFS embed.FS
