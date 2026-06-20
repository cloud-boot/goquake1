// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package model

import (
	"bytes"
	"errors"
	"testing"
)

// loadOrFatal builds a minimal BSP blob and inserts it via Cache.Load.
// Returned to share with multiple tests without re-deriving the bytes.
func loadOrFatal(t *testing.T, c *Cache, name string) (*Model, []byte) {
	t.Helper()
	data := buildMinimalBSP()
	m, err := c.Load(name, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	if m == nil {
		t.Fatalf("Load(%q): nil model, nil error", name)
	}
	return m, data
}

func TestNewCache_Empty(t *testing.T) {
	c := NewCache()
	if c == nil {
		t.Fatal("NewCache returned nil")
	}
	if c.Len() != 0 {
		t.Errorf("Len=%d want 0", c.Len())
	}
	if got := c.Get("anything.bsp"); got != nil {
		t.Errorf("Get on empty cache returned %v want nil", got)
	}
}

func TestCacheLoad_MissCaches(t *testing.T) {
	c := NewCache()
	m, _ := loadOrFatal(t, c, "maps/start.bsp")
	if c.Len() != 1 {
		t.Errorf("Len=%d want 1", c.Len())
	}
	if got := c.Get("maps/start.bsp"); got != m {
		t.Errorf("Get returned %p want %p", got, m)
	}
}

func TestCacheLoad_HitReturnsSamePointer(t *testing.T) {
	c := NewCache()
	first, _ := loadOrFatal(t, c, "maps/start.bsp")
	// Hit: src can be nil because the cache short-circuits before
	// touching it.
	second, err := c.Load("maps/start.bsp", nil, 0)
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if second != first {
		t.Errorf("hit returned %p want same as first %p", second, first)
	}
	if c.Len() != 1 {
		t.Errorf("Len=%d want 1 (no double-insert)", c.Len())
	}
}

func TestCacheLoad_NilSrcOnMiss(t *testing.T) {
	c := NewCache()
	_, err := c.Load("maps/never.bsp", nil, 0)
	if !errors.Is(err, ErrNotInCache) {
		t.Errorf("got %v want ErrNotInCache", err)
	}
}

func TestCacheLoad_EmptyName(t *testing.T) {
	c := NewCache()
	data := buildMinimalBSP()
	_, err := c.Load("", bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, ErrEmptyName) {
		t.Errorf("got %v want ErrEmptyName", err)
	}
}

func TestCacheLoad_PropagatesUnderlyingError(t *testing.T) {
	c := NewCache()
	// 2-byte blob -> Load returns ErrShortRead.
	_, err := c.Load("broken.bsp", bytes.NewReader([]byte{1, 2}), 2)
	if !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
	if c.Len() != 0 {
		t.Errorf("Len=%d want 0 -- failed loads must not cache", c.Len())
	}
}

func TestCacheGet_MissAndHit(t *testing.T) {
	c := NewCache()
	if got := c.Get("missing.bsp"); got != nil {
		t.Errorf("Get(missing)=%v want nil", got)
	}
	m, _ := loadOrFatal(t, c, "present.bsp")
	if got := c.Get("present.bsp"); got != m {
		t.Errorf("Get(present)=%p want %p", got, m)
	}
}

func TestCacheClear(t *testing.T) {
	c := NewCache()
	loadOrFatal(t, c, "a.bsp")
	loadOrFatal(t, c, "b.bsp")
	if c.Len() != 2 {
		t.Fatalf("pre-Clear Len=%d want 2", c.Len())
	}
	c.Clear()
	if c.Len() != 0 {
		t.Errorf("post-Clear Len=%d want 0", c.Len())
	}
	if got := c.Get("a.bsp"); got != nil {
		t.Errorf("post-Clear Get(a.bsp)=%v want nil", got)
	}
	// Cache is reusable after Clear.
	loadOrFatal(t, c, "c.bsp")
	if c.Len() != 1 {
		t.Errorf("post-Clear-reuse Len=%d want 1", c.Len())
	}
}

func TestCacheLoad_CaseSensitive(t *testing.T) {
	c := NewCache()
	lower, _ := loadOrFatal(t, c, "foo.bsp")
	upper, _ := loadOrFatal(t, c, "FOO.BSP")
	if lower == upper {
		t.Errorf("expected distinct entries for case-different names; both = %p", lower)
	}
	if c.Len() != 2 {
		t.Errorf("Len=%d want 2", c.Len())
	}
}
