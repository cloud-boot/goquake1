// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"bytes"
	"errors"
	"io"

	"github.com/go-quake1/engine/model"
)

// FileResolver fetches the bytes for a model name. tyrquake: COM_LoadFile.
// Implementations: a real PAK + filesystem walker, an in-memory map for
// tests, or an embedded-asset reader. Returns (size, ReaderAt, error);
// ReaderAt may be a bytes.Reader for in-memory cases. size MUST be the
// full byte count of the resolved file (caller passes it to model.Load).
type FileResolver func(name string) (size int64, src io.ReaderAt, err error)

// ErrNoResolver fires when LoadModelByName is called with a nil
// resolver and a cache miss.
var ErrNoResolver = errors.New("server: cache miss + nil FileResolver")

// LoadModelByName returns the cached *Model for name, or fetches +
// loads on miss. Combines model.Cache + FileResolver into a single
// "get a model from disk" call. tyrquake: Mod_ForName.
//
// On cache hit, returns immediately without touching resolver.
// On cache miss, calls resolver(name) for the bytes + delegates to
// model.Cache.Load(name, src, size) to memoize + dispatch.
//
// Returns:
//
//	ErrNoResolver       on cache miss with nil resolver
//	propagated errors   from resolver or model.Cache.Load
func LoadModelByName(cache *model.Cache, name string, resolver FileResolver) (*model.Model, error) {
	if m := cache.Get(name); m != nil {
		return m, nil
	}
	if resolver == nil {
		return nil, ErrNoResolver
	}
	size, src, err := resolver(name)
	if err != nil {
		return nil, err
	}
	return cache.Load(name, src, size)
}

// LoadBytesIntoCache is the in-memory shortcut: name + raw bytes ->
// cached *Model. Used by tests + the embedded-WAD path where bytes
// are already in hand. Equivalent to wrapping a bytes.Reader in a
// FileResolver and calling LoadModelByName.
func LoadBytesIntoCache(cache *model.Cache, name string, data []byte) (*model.Model, error) {
	return cache.Load(name, bytes.NewReader(data), int64(len(data)))
}
