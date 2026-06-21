// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package embedpak

import (
	"bytes"
	_ "embed"
	"errors"
	"io/fs"

	"github.com/go-quake1/engine/pak"
	"github.com/go-quake1/engine/vfs"
)

// embeddedBytes is the build-time pak blob. The default file
// embedpak/empty.pak is a 12-byte stub (valid PACK header, zero
// directory entries) so the package always builds without requiring a
// real shareware pak0.pak to be present on disk. Operators swap the
// file in by overwriting empty.pak with id Software's freely
// redistributable shareware archive.
//
//go:embed empty.pak
var embeddedBytes []byte

// emptyPakSize is the size in bytes of the 12-byte placeholder
// header (PACK magic + dirofs int32 + dirlen int32). Any blob this
// small or smaller cannot carry directory entries and is treated as
// the empty placeholder.
const emptyPakSize = 12

// ErrEmbedPakEmpty is returned by OpenAsFS / AddToVFS when the
// embedded blob is still the 12-byte placeholder. Callers handle it
// by falling back to synthetic assets (the bootstrap path the engine
// uses while no real pak0.pak is installed).
var ErrEmbedPakEmpty = errors.New("embedpak: embedded pak is the empty placeholder; drop real pak0.pak into embedpak/empty.pak to use real assets")

// Bytes returns a copy of the embedded pak file. The default copy is
// the 12-byte placeholder; replace embedpak/empty.pak with id
// Software's shareware pak0.pak to get real game content. A fresh
// slice is returned so callers cannot mutate the in-binary data.
func Bytes() []byte {
	out := make([]byte, len(embeddedBytes))
	copy(out, embeddedBytes)
	return out
}

// IsEmpty reports whether the embedded blob is the placeholder (no
// real assets). Returns true for any blob of <= 12 bytes (the bare
// PAK header) so callers can decide between real-asset and
// synthetic-asset bootstrap paths.
func IsEmpty() bool {
	return len(embeddedBytes) <= emptyPakSize
}

// OpenAsFS opens the embedded pak via [pak.Open] and returns it as an
// [io/fs.FS] suitable for [vfs.SearchPath.Add]. Returns
// ErrEmbedPakEmpty when [IsEmpty] is true so the caller can wire up
// the synthetic-asset fallback.
func OpenAsFS() (fs.FS, error) {
	if IsEmpty() {
		return nil, ErrEmbedPakEmpty
	}
	return pak.Open(bytes.NewReader(embeddedBytes))
}

// AddToVFS opens the embedded pak and prepends it to sp. Returns
// ErrEmbedPakEmpty when the embedded blob is still the placeholder so
// the caller can fall back to synthetic assets.
func AddToVFS(sp *vfs.SearchPath) error {
	f, err := OpenAsFS()
	if err != nil {
		return err
	}
	sp.Add(f)
	return nil
}
