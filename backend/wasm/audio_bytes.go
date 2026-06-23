// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasm

import "unsafe"

// float32SliceAsBytes reinterprets the float32 slice's backing array
// as a byte slice (4 bytes per element) WITHOUT copying. The result
// shares memory with the input; callers must keep the input alive
// until the bytes are consumed.
//
// Endianness: wasm is little-endian, which matches the JS
// Float32Array on every browser we target — the bytes can be
// memcpy'd into a Uint8Array view and a Float32Array overlay over
// the same ArrayBuffer will produce identical samples. Host
// platforms that test this helper are amd64/arm64 (also LE), so the
// helper's pure-Go test passes everywhere we run CI.
func float32SliceAsBytes(s []float32) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*4)
}
