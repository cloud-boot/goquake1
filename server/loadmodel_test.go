// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/model"
)

// buildMinimalBSP mirrors the model-package helper: 124-byte BSP
// header with version 29 + an all-zero lump table. Sufficient for
// bspfile.Open + model.Load to accept.
func buildMinimalBSP() []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(bspfile.Version29))
	buf.Write(make([]byte, 15*8))
	return buf.Bytes()
}

func TestLoadModelByName_CacheHit(t *testing.T) {
	c := model.NewCache()
	data := buildMinimalBSP()
	first, err := LoadBytesIntoCache(c, "maps/start.bsp", data)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Cache hit: nil resolver MUST NOT be touched -- the cache
	// short-circuits.
	second, err := LoadModelByName(c, "maps/start.bsp", nil)
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if second != first {
		t.Errorf("hit returned %p want same as seed %p", second, first)
	}
}

func TestLoadModelByName_CacheMiss_ValidResolver(t *testing.T) {
	c := model.NewCache()
	data := buildMinimalBSP()
	called := 0
	resolver := func(name string) (int64, io.ReaderAt, error) {
		called++
		if name != "maps/e1m1.bsp" {
			t.Errorf("resolver called with %q want maps/e1m1.bsp", name)
		}
		return int64(len(data)), bytes.NewReader(data), nil
	}
	m, err := LoadModelByName(c, "maps/e1m1.bsp", resolver)
	if err != nil {
		t.Fatalf("miss+resolver: %v", err)
	}
	if m == nil || m.Kind != model.KindBrush {
		t.Errorf("got %+v want non-nil KindBrush", m)
	}
	if called != 1 {
		t.Errorf("resolver called %d times want 1", called)
	}
	// Second call: hit, resolver MUST NOT fire again.
	if _, err := LoadModelByName(c, "maps/e1m1.bsp", resolver); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if called != 1 {
		t.Errorf("second call: resolver called %d times want 1 (still)", called)
	}
}

func TestLoadModelByName_CacheMiss_NilResolver(t *testing.T) {
	c := model.NewCache()
	_, err := LoadModelByName(c, "maps/missing.bsp", nil)
	if !errors.Is(err, ErrNoResolver) {
		t.Errorf("got %v want ErrNoResolver", err)
	}
}

func TestLoadModelByName_ResolverError(t *testing.T) {
	c := model.NewCache()
	sentinel := errors.New("disk on fire")
	resolver := func(name string) (int64, io.ReaderAt, error) {
		return 0, nil, sentinel
	}
	_, err := LoadModelByName(c, "maps/start.bsp", resolver)
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v want sentinel", err)
	}
}

func TestLoadModelByName_LoaderError(t *testing.T) {
	c := model.NewCache()
	// 2-byte payload triggers model.ErrShortRead inside Cache.Load.
	resolver := func(name string) (int64, io.ReaderAt, error) {
		return 2, bytes.NewReader([]byte{0xDE, 0xAD}), nil
	}
	_, err := LoadModelByName(c, "broken.bsp", resolver)
	if !errors.Is(err, model.ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

func TestLoadBytesIntoCache_HappyPath(t *testing.T) {
	c := model.NewCache()
	data := buildMinimalBSP()
	m, err := LoadBytesIntoCache(c, "maps/start.bsp", data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m == nil || m.Kind != model.KindBrush {
		t.Errorf("got %+v want non-nil KindBrush", m)
	}
	if got := c.Get("maps/start.bsp"); got != m {
		t.Errorf("post-load Get=%p want %p", got, m)
	}
}

func TestLoadBytesIntoCache_EmptyName(t *testing.T) {
	c := model.NewCache()
	_, err := LoadBytesIntoCache(c, "", buildMinimalBSP())
	if !errors.Is(err, model.ErrEmptyName) {
		t.Errorf("got %v want ErrEmptyName", err)
	}
}

func TestLoadBytesIntoCache_BadBytes(t *testing.T) {
	c := model.NewCache()
	_, err := LoadBytesIntoCache(c, "broken.bsp", []byte{0x00, 0x01})
	if !errors.Is(err, model.ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}
