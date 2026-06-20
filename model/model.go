// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package model

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/mdl"
	"github.com/go-quake1/engine/spr"
)

// Kind tags the typed payload Model carries.
type Kind int

const (
	KindBrush  Kind = iota // BSP map (default when no magic match)
	KindAlias              // .mdl alias model (IDPO magic)
	KindSprite             // .spr sprite (IDSP magic)
)

// Model is the polymorphic result of Load. Exactly one of Brush /
// Alias / Sprite is non-nil per the Kind tag.
type Model struct {
	Kind   Kind
	Brush  *bspfile.File
	Alias  *mdl.Model
	Sprite *spr.Sprite
}

// Sentinel errors.
var (
	ErrShortRead  = errors.New("model: source has fewer than 4 bytes -- cannot detect magic")
	ErrLoaderFail = errors.New("model: per-format loader rejected the file")
)

// Load reads the first 4 bytes of src to identify the format, then
// dispatches to the matching decoder. tyrquake: Mod_LoadModel.
func Load(src io.ReaderAt, size int64) (*Model, error) {
	if src == nil {
		return nil, errors.New("model: nil src")
	}
	if size < 4 {
		return nil, ErrShortRead
	}
	var hdr [4]byte
	if _, err := src.ReadAt(hdr[:], 0); err != nil {
		return nil, fmt.Errorf("model: read header: %w", err)
	}
	magic := binary.LittleEndian.Uint32(hdr[:])
	switch magic {
	case mdl.IDPolyHeader:
		m, err := mdl.Load(src, size)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrLoaderFail, err)
		}
		return &Model{Kind: KindAlias, Alias: m}, nil
	case spr.IDSpriteHeader:
		s, err := spr.Load(src, size)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrLoaderFail, err)
		}
		return &Model{Kind: KindSprite, Sprite: s}, nil
	default:
		// Anything else: BSP. bspfile.Open enforces version=29 +
		// returns ErrBadVersion otherwise, so a stray binary blob
		// won't be silently accepted as a brush model.
		b, err := bspfile.Open(src, size)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrLoaderFail, err)
		}
		return &Model{Kind: KindBrush, Brush: b}, nil
	}
}
