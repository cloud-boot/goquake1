// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package model

import (
	"errors"
	"io"
)

// Cache memoizes model loads by file name. Subsequent lookups of the
// same name return the previously-loaded *Model instead of re-parsing
// the bytes. tyrquake: the static mod_known[MAX_MOD_KNOWN] table +
// Mod_FindName + Mod_ForName triplet in common/model.c.
//
// The cache is content-agnostic: the source io.ReaderAt is the
// caller's responsibility (file system / pak / in-memory bytes
// indifferently). Cache.Load fetches from the cache or delegates
// to [Load] (the magic-byte dispatcher) on miss; Cache.Get is a
// pure lookup without the load side-effect.
type Cache struct {
	entries map[string]*Model
}

// ErrNotInCache is returned by [Cache.Load] when src is nil and the
// requested name is not already cached -- the caller asked for a
// cached lookup but supplied no bytes for a potential miss.
var ErrNotInCache = errors.New("model: name not in cache and no source provided")

// ErrEmptyName is returned by [Cache.Load] when name is "". The C
// upstream's Mod_FindName Sys_Error()s on this; we surface it as a
// normal error instead.
var ErrEmptyName = errors.New("model: empty name")

// NewCache returns an empty cache. Safe to call without an init step.
func NewCache() *Cache {
	return &Cache{entries: make(map[string]*Model)}
}

// Load returns the cached *Model for name, or loads from src on miss.
// On miss, src + size are passed verbatim to [Load] (the package's
// existing magic-byte dispatcher); the resulting *Model is stored
// under name + returned.
//
// Returns ErrNotInCache iff src == nil AND name is not in the cache --
// i.e. the caller wants a cached lookup without supplying bytes for
// a potential miss.
//
// Other errors from the underlying [Load] are propagated verbatim.
func (c *Cache) Load(name string, src io.ReaderAt, size int64) (*Model, error) {
	if name == "" {
		return nil, ErrEmptyName
	}
	if m, ok := c.entries[name]; ok {
		return m, nil
	}
	if src == nil {
		return nil, ErrNotInCache
	}
	m, err := Load(src, size)
	if err != nil {
		return nil, err
	}
	c.entries[name] = m
	return m, nil
}

// Get is a pure cache lookup; returns nil if name is not cached.
// No side effects.
func (c *Cache) Get(name string) *Model {
	return c.entries[name]
}

// Clear wipes the cache. Used between maps -- the C upstream's
// Mod_ClearAll equivalent. The previously-returned *Model
// references remain valid (Go GC); they just stop being reachable
// via the cache.
func (c *Cache) Clear() {
	c.entries = make(map[string]*Model)
}

// Len returns the cached-entry count.
func (c *Cache) Len() int {
	return len(c.entries)
}
