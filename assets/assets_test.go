// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package assets

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/vfs"
)

// synthVFS builds a test vfs.SearchPath populated with a fstest.MapFS
// that maps paths to synthetic blob contents.
func synthVFS(t *testing.T, files map[string][]byte) *vfs.SearchPath {
	t.Helper()
	v := vfs.New()
	mfs := fstest.MapFS{}
	for path, content := range files {
		mfs[path] = &fstest.MapFile{Data: content}
	}
	v.Add(mfs)
	return v
}

func makePaletteLump() []byte {
	buf := make([]byte, render.PaletteLumpSize)
	for i := 0; i < 256; i++ {
		buf[i*3+0] = byte(i)
		buf[i*3+1] = byte(i ^ 0xFF)
		buf[i*3+2] = byte(i << 1)
	}
	return buf
}

func makeColorMapLump() []byte {
	buf := make([]byte, render.ColorMapRows*render.ColorMapCols)
	for i := range buf {
		buf[i] = byte(i)
	}
	return buf
}

func makeConcharsLump() []byte {
	buf := make([]byte, ConCharsLumpSize)
	for i := range buf {
		buf[i] = byte(i & 0xFF)
	}
	return buf
}

// makeWavLump returns a minimal valid WAV with 8-bit unsigned PCM mono.
func makeWavLump() []byte {
	body := []byte{128, 128, 128, 128}
	// RIFF + WAVE + fmt + data chunks
	fmtChunk := make([]byte, 24)
	copy(fmtChunk[0:4], []byte{'f', 'm', 't', ' '})
	binary.LittleEndian.PutUint32(fmtChunk[4:8], 16)
	binary.LittleEndian.PutUint16(fmtChunk[8:10], 1)  // PCM
	binary.LittleEndian.PutUint16(fmtChunk[10:12], 1) // mono
	binary.LittleEndian.PutUint32(fmtChunk[12:16], 11025)
	binary.LittleEndian.PutUint32(fmtChunk[16:20], 11025)
	binary.LittleEndian.PutUint16(fmtChunk[20:22], 1)
	binary.LittleEndian.PutUint16(fmtChunk[22:24], 8)

	dataChunk := make([]byte, 8+len(body))
	copy(dataChunk[0:4], []byte{'d', 'a', 't', 'a'})
	binary.LittleEndian.PutUint32(dataChunk[4:8], uint32(len(body)))
	copy(dataChunk[8:], body)

	out := make([]byte, 12+len(fmtChunk)+len(dataChunk))
	copy(out[0:4], []byte{'R', 'I', 'F', 'F'})
	binary.LittleEndian.PutUint32(out[4:8], uint32(4+len(fmtChunk)+len(dataChunk)))
	copy(out[8:12], []byte{'W', 'A', 'V', 'E'})
	copy(out[12:], fmtChunk)
	copy(out[12+len(fmtChunk):], dataChunk)
	return out
}

// ----- NewSet / readAll helpers ------------------------------------

func TestNewSet(t *testing.T) {
	s := NewSet()
	if s.Pics == nil || s.Sounds == nil {
		t.Fatalf("NewSet maps nil: %+v", s)
	}
}

// ----- LoadPaletteFrom ---------------------------------------------

func TestLoadPalette_Happy(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/palette.lmp": makePaletteLump(),
	})
	pal, err := LoadPaletteFrom(v)
	if err != nil {
		t.Fatalf("LoadPaletteFrom: %v", err)
	}
	if pal[42][0] != 42 {
		t.Fatalf("palette[42][0] = %d want 42", pal[42][0])
	}
}

func TestLoadPalette_NilVFS(t *testing.T) {
	_, err := LoadPaletteFrom(nil)
	if !errors.Is(err, ErrAssetsNilVFS) {
		t.Fatalf("err = %v want ErrAssetsNilVFS", err)
	}
}

func TestLoadPalette_Missing(t *testing.T) {
	v := synthVFS(t, nil)
	_, err := LoadPaletteFrom(v)
	if err == nil {
		t.Fatalf("expected error for missing palette lump")
	}
}

func TestLoadPalette_BadSize(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/palette.lmp": make([]byte, 100), // wrong size
	})
	_, err := LoadPaletteFrom(v)
	if err == nil {
		t.Fatalf("expected error for bad-size palette")
	}
}

// ----- LoadColorMapFrom --------------------------------------------

func TestLoadColorMap_Happy(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/colormap.lmp": makeColorMapLump(),
	})
	cm, err := LoadColorMapFrom(v)
	if err != nil {
		t.Fatalf("LoadColorMapFrom: %v", err)
	}
	_ = cm
}

func TestLoadColorMap_NilVFS(t *testing.T) {
	_, err := LoadColorMapFrom(nil)
	if !errors.Is(err, ErrAssetsNilVFS) {
		t.Fatalf("err = %v want ErrAssetsNilVFS", err)
	}
}

func TestLoadColorMap_Missing(t *testing.T) {
	v := synthVFS(t, nil)
	_, err := LoadColorMapFrom(v)
	if err == nil {
		t.Fatalf("expected error for missing colormap lump")
	}
}

// ----- LoadConCharsFrom --------------------------------------------

func TestLoadConChars_Happy(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/conchars.lmp": makeConcharsLump(),
	})
	cc, err := LoadConCharsFrom(v)
	if err != nil {
		t.Fatalf("LoadConCharsFrom: %v", err)
	}
	if cc.Width != 128 || cc.Height != 128 {
		t.Fatalf("conchars dim = %dx%d want 128x128", cc.Width, cc.Height)
	}
	if len(cc.Pixels) != ConCharsLumpSize {
		t.Fatalf("conchars pixels = %d want %d", len(cc.Pixels), ConCharsLumpSize)
	}
}

func TestLoadConChars_NilVFS(t *testing.T) {
	_, err := LoadConCharsFrom(nil)
	if !errors.Is(err, ErrAssetsNilVFS) {
		t.Fatalf("err = %v want ErrAssetsNilVFS", err)
	}
}

func TestLoadConChars_Missing(t *testing.T) {
	v := synthVFS(t, nil)
	_, err := LoadConCharsFrom(v)
	if err == nil {
		t.Fatalf("expected error for missing conchars lump")
	}
}

func TestLoadConChars_BadSize(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/conchars.lmp": make([]byte, 100),
	})
	_, err := LoadConCharsFrom(v)
	if !errors.Is(err, ErrAssetsConcharsSize) {
		t.Fatalf("err = %v want ErrAssetsConcharsSize", err)
	}
}

// ----- LoadSoundFrom -----------------------------------------------

func TestLoadSound_Happy(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"sound/test.wav": makeWavLump(),
	})
	s, err := LoadSoundFrom(v, "sound/test.wav")
	if err != nil {
		t.Fatalf("LoadSoundFrom: %v", err)
	}
	if s.Name != "sound/test.wav" {
		t.Fatalf("sample name = %q", s.Name)
	}
}

func TestLoadSound_NilVFS(t *testing.T) {
	_, err := LoadSoundFrom(nil, "sound/x.wav")
	if !errors.Is(err, ErrAssetsNilVFS) {
		t.Fatalf("err = %v want ErrAssetsNilVFS", err)
	}
}

func TestLoadSound_Missing(t *testing.T) {
	v := synthVFS(t, nil)
	_, err := LoadSoundFrom(v, "sound/nope.wav")
	if err == nil {
		t.Fatalf("expected error for missing sound lump")
	}
}

// ----- LoadStandard ------------------------------------------------

func TestLoadStandard_Happy(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/palette.lmp":  makePaletteLump(),
		"gfx/colormap.lmp": makeColorMapLump(),
		"gfx/conchars.lmp": makeConcharsLump(),
	})
	set, err := LoadStandard(v)
	if err != nil {
		t.Fatalf("LoadStandard: %v", err)
	}
	if set.Palette == nil || set.ColorMap == nil || set.ConChars == nil {
		t.Fatalf("LoadStandard missing fields: %+v", set)
	}
}

func TestLoadStandard_NilVFS(t *testing.T) {
	set, err := LoadStandard(nil)
	if !errors.Is(err, ErrAssetsNilVFS) {
		t.Fatalf("err = %v want ErrAssetsNilVFS", err)
	}
	if set == nil {
		t.Fatalf("LoadStandard returned nil Set on nil vfs")
	}
}

func TestLoadStandard_MissingPalette(t *testing.T) {
	v := synthVFS(t, nil)
	_, err := LoadStandard(v)
	if err == nil {
		t.Fatalf("expected error for missing palette in LoadStandard")
	}
}

func TestLoadStandard_MissingColorMap(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/palette.lmp": makePaletteLump(),
	})
	_, err := LoadStandard(v)
	if err == nil {
		t.Fatalf("expected error for missing colormap in LoadStandard")
	}
}

func TestLoadStandard_MissingConChars(t *testing.T) {
	v := synthVFS(t, map[string][]byte{
		"gfx/palette.lmp":  makePaletteLump(),
		"gfx/colormap.lmp": makeColorMapLump(),
	})
	_, err := LoadStandard(v)
	if err == nil {
		t.Fatalf("expected error for missing conchars in LoadStandard")
	}
}

// Sanity: vfs.SearchPath satisfies the fs.FS-style usage we depend on.
func TestVFSCompat(t *testing.T) {
	v := synthVFS(t, map[string][]byte{"x": {1, 2, 3}})
	f, err := v.Open("x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	var _ fs.File = f
}
