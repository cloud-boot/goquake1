// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !no_embed_assets

package embedpak

import _ "embed"

// embeddedBytes is the build-time pak blob. The default file
// embedpak/empty.pak is a 12-byte stub (valid PACK header, zero
// directory entries) so the package always builds without requiring a
// real shareware pak0.pak to be present on disk. Operators swap the
// file in by overwriting empty.pak with id Software's freely
// redistributable shareware archive.
//
// Build wasm with -tags no_embed_assets to strip this go:embed
// directive (and pull the pak from an OCI registry at runtime via
// ociassets instead).
//
//go:embed empty.pak
var embeddedBytes []byte
