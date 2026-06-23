// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasm

import "syscall/js"

// PerformanceNow returns a ClockFn that reads
// window.performance.now() (milliseconds since the navigation start)
// and converts to seconds for backend.Clock.Now consumers.
//
// performance.now() is monotonic, has sub-millisecond resolution
// (modulo browser cross-origin coarsening), and is exactly the
// canonical clock for "frame timing in a browser game loop".
//
// Returns a closure that captures the performance global once, so
// each Now() call is one js.Value method dispatch.
func PerformanceNow() ClockFn {
	perf := js.Global().Get("performance")
	return func() float64 {
		return perf.Call("now").Float() / 1000.0
	}
}
