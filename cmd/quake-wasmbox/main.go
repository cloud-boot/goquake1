// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

// quake-wasmbox is the wasmbox-external-client entry point for the
// Go-native Quake engine. It is a sibling of cmd/quake-wasm: same
// engine wiring -- but the presentation surface is a wasmbox-protocol
// SharedArrayBuffer + the `{type:"commit"}` postMessage rather than a
// DOM canvas.
//
// The wasm runs inside a Web Worker. The wasmbox compositor (on the
// main thread) owns the desktop canvas + stacking + focus + input
// routing; our backend.Backend talks to it via the step-B wire
// protocol. Input events the compositor forwards to the focused
// window surface as backend.InputSnapshots out of wasmbox.PollInput,
// which the runloop turns into clc_move + view commands.
//
// The actual game loop lives in package game (shared with the native
// PPM harness + the bare-metal binary): game.Build wires the real
// progs VM + host server + BSP world + client signon + renderer over
// the embedded/streamed pak. This file is just the wasmbox-specific
// backend + asset-source plumbing around that call.
package main

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/go-quake1/engine/backend/wasmbox"
	"github.com/go-quake1/engine/embedpak"
	"github.com/go-quake1/engine/game"
	"github.com/go-quake1/engine/ociassets"
)

// OCIReference, when set at link time via -ldflags '-X
// main.OCIReference=...', streams the pak + music from an OCI registry
// instead of relying on the embedded placeholder.
var OCIReference = ""

// fbWidth / fbHeight match the vanilla DOS Quake framebuffer so the
// software rasterizer's per-pixel work stays affordable in a wasm
// runtime. The wasmbox compositor blits the surface 1:1 to the window.
const (
	fbWidth  = 320
	fbHeight = 240
)

func main() {
	if err := run(); err != nil {
		fmt.Println("QUAKE: FAIL", err)
		// Block forever so the JS-side wasm instance stays alive long
		// enough for the console message to flush + so DOM event
		// handlers retain their js.Func references.
		<-make(chan struct{})
		return
	}
	fmt.Println("QUAKE: exited cleanly")
}

// run is main's testability seam. It returns errors instead of halting
// so the worker console carries the failure reason; main then blocks
// on receipt.
func run() error {
	// 1. Handshake with the wasmbox compositor + build the backend.
	be, err := wasmbox.NewClient("quake (wasm)", fbWidth, fbHeight)
	if err != nil {
		return fmt.Errorf("wasmbox.NewClient: %w", err)
	}
	logf("backend up -- wasmbox surface=%dx%d", fbWidth, fbHeight)

	// 2. Asset source: OCI streaming first (when linked), then the
	//    embedded pak (the 179 MB id1 archive committed to embedpak).
	var pakFS fs.FS
	if OCIReference != "" {
		if fsys, oerr := openOCIAssets(OCIReference); oerr != nil {
			logf("ociassets: %v -- falling back to embedpak", oerr)
		} else {
			pakFS = fsys
			logf("ociassets: streaming pak from %s", OCIReference)
		}
	}
	if pakFS == nil {
		if fsys, perr := embedpak.OpenAsFS(); perr != nil {
			logf("embedpak.OpenAsFS: %v -- the synth fallback will render", perr)
		} else {
			pakFS = fsys
			logf("embedpak: real pak0 opened")
		}
	}

	// 3. Build the real game session (progs VM + host + map + client +
	//    renderer + input). The wasmbox backend's PollInput feeds the
	//    runloop the DOM key/mouse events the compositor forwarded, so
	//    WASD + mouse + space/enter drive the player. DemoOrbit is OFF
	//    (interactive); the player moves the moment a key arrives.
	sess, err := game.Build(pakFS, be, game.Options{
		Map: "start",
		// Per-second liveness log: prints the wire-mirrored player
		// origin + view angles so the browser console proves whether
		// keyboard input (WASD/mouse forwarded by the compositor) is
		// driving the camera. Logs once per ~second at the 20 Hz tic.
		OnFrame: func(frame int, origin, angles [3]float32) {
			if frame%20 == 0 {
				fmt.Printf("QUAKE: tic %d origin=%v angles=%v\n", frame, origin, angles)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("game.Build: %w", err)
	}
	logf("session up -- realHost=%v playerSlot=%d; entering RunUntilQuit",
		sess.Host != nil, sess.PlayerSlot)

	// Session.RunUntilQuit yields to the JS event loop each tic (a 1 ms
	// sleep) so the worker's `message` callbacks fire -- this is what
	// lets the compositor-forwarded WASD/mouse input actually reach the
	// player. The plain runloop.RunUntilQuit would starve input under
	// wasm's single-threaded scheduler.
	return sess.RunUntilQuit()
}

// logf is fmt.Printf scoped to a QUAKE: prefix so worker-console output
// stays grep-able.
func logf(format string, args ...any) {
	fmt.Printf("QUAKE: "+format+"\n", args...)
}

// openOCIAssets parses the linker-baked reference, builds the client,
// fetches the manifest, and returns an fs.FS that streams the layers.
func openOCIAssets(reference string) (fs.FS, error) {
	ref, err := ociassets.ParseReference(reference)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", reference, err)
	}
	client := ociassets.NewClient(ref.Origin)
	return ociassets.NewFSFromManifest(context.Background(), client, ref.Repo, ref.Tag)
}
