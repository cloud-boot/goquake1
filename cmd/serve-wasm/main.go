// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// serve-wasm is a tiny localhost-only static file server bound to
// localhost:8080 that serves the cmd/quake-wasm/web/ directory so
// the user can open the browser-side Quake build via
// http://localhost:8080/. Set $QUAKE_WASM_ADDR to override the bind
// address (e.g. "127.0.0.1:0" for an ephemeral port).
//
// The server applies the correct application/wasm Content-Type to
// .wasm files -- some browsers refuse to instantiate a wasm payload
// without it -- and otherwise delegates to net/http.FileServer.
//
// One-shot mode: set $QUAKE_WASM_ONESHOT=1 to make the server exit
// cleanly after the first request completes. Used by the integration
// test below + lets headless smoke tests assert "serves and exits".
package main

import (
	"fmt"
	"os"
)

func main() {
	addr := os.Getenv("QUAKE_WASM_ADDR")
	if addr == "" {
		addr = "localhost:8080"
	}
	root := os.Getenv("QUAKE_WASM_ROOT")
	if root == "" {
		root = "cmd/quake-wasm/web"
	}
	oneShot := os.Getenv("QUAKE_WASM_ONESHOT") == "1"

	if err := serve(addr, root, oneShot, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "serve-wasm:", err)
		osExit(1)
		return
	}
}

// osExit is a seam so tests can swap in a no-op exit.
var osExit = os.Exit
