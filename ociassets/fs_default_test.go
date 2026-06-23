// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build !js || !wasm

package ociassets

import "testing"

func TestNoopPersistCache(t *testing.T) {
	// Make sure the default (host) persistCache really is a no-op
	// -- Get always misses, Put never panics. Direct concrete-type
	// invocation so the coverage profile attributes the Put hit.
	pc := defaultPersistCache()
	if _, ok := pc.Get("sha256:00"); ok {
		t.Fatal("noopPersistCache.Get: want miss")
	}
	pc.Put("sha256:00", []byte("x"))
	// Invoke directly on the concrete type so coverage definitely
	// counts the empty-body Put.
	noopPersistCache{}.Put("k", []byte("v"))
	if _, ok := (noopPersistCache{}).Get("k"); ok {
		t.Fatal("noopPersistCache.Get(concrete): want miss")
	}
}
