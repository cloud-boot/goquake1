// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package assets

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/sound"
	"github.com/go-quake1/engine/vfs"
)

// ConCharsLumpSize is the on-disk byte count of the conchars sheet:
// 128 * 128 = 16384 raw palette-indexed bytes (no dpic8_t header).
const ConCharsLumpSize = 128 * 128

// Set bundles every startup-loaded asset. The fields are populated
// in stages by LoadStandard; partial sets are returned on error so
// callers can decide what to do (e.g. continue with placeholder
// assets in dev mode).
type Set struct {
	Palette  *render.Palette
	ColorMap *render.ColorMap
	ConChars *render.Pic
	Pics     map[string]*render.Pic
	Sounds   map[string]*sound.Sample
}

// NewSet returns a Set with empty Pics + Sounds maps.
func NewSet() *Set {
	return &Set{
		Pics:   make(map[string]*render.Pic),
		Sounds: make(map[string]*sound.Sample),
	}
}

var (
	ErrAssetsNilVFS    = errors.New("assets: nil vfs.SearchPath")
	ErrAssetsConcharsSize = errors.New("assets: conchars.lmp not exactly 16384 bytes")
)

// LoadPaletteFrom opens "gfx/palette.lmp" via vfs and parses it.
// Returns the parsed *Palette + nil on success; the propagated
// vfs.Open / read / parse error on failure.
func LoadPaletteFrom(v *vfs.SearchPath) (*render.Palette, error) {
	if v == nil {
		return nil, ErrAssetsNilVFS
	}
	bytes, err := readAll(v, "gfx/palette.lmp")
	if err != nil {
		return nil, fmt.Errorf("load palette: %w", err)
	}
	return render.LoadPalette(bytes)
}

// LoadColorMapFrom opens "gfx/colormap.lmp" via vfs and parses it.
func LoadColorMapFrom(v *vfs.SearchPath) (*render.ColorMap, error) {
	if v == nil {
		return nil, ErrAssetsNilVFS
	}
	bytes, err := readAll(v, "gfx/colormap.lmp")
	if err != nil {
		return nil, fmt.Errorf("load colormap: %w", err)
	}
	return render.LoadColorMap(bytes)
}

// LoadConCharsFrom opens "gfx/conchars.lmp" via vfs and wraps it in
// a 128x128 *Pic. The lump is raw palette-indexed bytes (no
// dpic8_t header); LoadConCharsFrom synthesizes the Pic wrapper.
func LoadConCharsFrom(v *vfs.SearchPath) (*render.Pic, error) {
	if v == nil {
		return nil, ErrAssetsNilVFS
	}
	bytes, err := readAll(v, "gfx/conchars.lmp")
	if err != nil {
		return nil, fmt.Errorf("load conchars: %w", err)
	}
	if len(bytes) != ConCharsLumpSize {
		return nil, ErrAssetsConcharsSize
	}
	pixels := make([]byte, ConCharsLumpSize)
	copy(pixels, bytes)
	return &render.Pic{Width: 128, Height: 128, Pixels: pixels}, nil
}

// LoadSoundFrom opens a .wav lump via vfs and parses it into a
// *sound.Sample. The lump path is supplied verbatim (typically
// "sound/<name>"). The returned sample's Name is the path.
func LoadSoundFrom(v *vfs.SearchPath, path string) (*sound.Sample, error) {
	if v == nil {
		return nil, ErrAssetsNilVFS
	}
	bytes, err := readAll(v, path)
	if err != nil {
		return nil, fmt.Errorf("load sound %q: %w", path, err)
	}
	return sound.LoadWav(path, bytes)
}

// LoadStandard runs the standard startup loaders in order:
// palette, colormap, conchars. On the first error, returns the
// partial Set (Pics + Sounds empty + nil fields where loading
// failed) along with the error.
func LoadStandard(v *vfs.SearchPath) (*Set, error) {
	set := NewSet()
	if v == nil {
		return set, ErrAssetsNilVFS
	}
	pal, err := LoadPaletteFrom(v)
	if err != nil {
		return set, err
	}
	set.Palette = pal
	cm, err := LoadColorMapFrom(v)
	if err != nil {
		return set, err
	}
	set.ColorMap = cm
	cc, err := LoadConCharsFrom(v)
	if err != nil {
		return set, err
	}
	set.ConChars = cc
	return set, nil
}

// readAll is a small helper that opens a vfs path and reads the
// full file into a byte slice. Closes the underlying file.
func readAll(v *vfs.SearchPath, path string) ([]byte, error) {
	f, err := v.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// Compile-time check: readAll's return matches fs.File.Read.
var _ fs.File = (fs.File)(nil)
