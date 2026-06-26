// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build no_embed_assets

package embedpak

// embeddedBytes is empty under the no_embed_assets build tag so the
// binary doesn't carry the embedpak/empty.pak payload. IsEmpty()
// returns true and OpenAsFS / AddToVFS return ErrEmbedPakEmpty,
// which the wasm cmd/ entry points handle by routing through
// ociassets instead.
var embeddedBytes []byte
