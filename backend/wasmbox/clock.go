// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasmbox

import "syscall/js"

// PerformanceNow returns a ClockFn that reads performance.now()
// (milliseconds since the navigation start, monotonic) and converts to
// seconds. Worker globals expose performance.now() with the same shape
// as the main-thread API, so this is a direct mirror of
// backend/wasm.PerformanceNow.
func PerformanceNow() ClockFn {
	perf := js.Global().Get("performance")
	return func() float64 {
		return perf.Call("now").Float() / 1000.0
	}
}
