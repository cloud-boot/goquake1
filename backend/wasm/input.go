// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasm

import (
	"sync"
	"syscall/js"
)

// DOMInput is an InputDevice that snapshots DOM keyboard + mouse
// events. Event listeners are registered ONCE at construction on the
// supplied target (typically the canvas — for keyboard events we
// listen on window so focus is not required).
//
// Pointer Lock: a "click" listener on the canvas calls
// requestPointerLock() so subsequent mousemove events carry
// movementX/movementY suitable for first-person look.
//
// Concurrency: GOOS=js GOARCH=wasm is currently single-threaded
// (cooperative scheduling between Go goroutines + the JS event
// loop), but a sync.Mutex still guards the event queue so the
// invariant holds against future multithreaded wasm runtimes.
type DOMInput struct {
	mu     sync.Mutex
	events []InputEvent

	// Retained js.Func references so listener removal (if ever
	// needed) can pass the same callback back to removeEventListener.
	// Without retention the Go runtime would garbage-collect the
	// wrapping func + the next event would dispatch into freed
	// memory.
	keyDownCB    js.Func
	keyUpCB      js.Func
	mouseMoveCB  js.Func
	mouseDownCB  js.Func
	mouseUpCB    js.Func
	clickCB      js.Func
	beforeUnload js.Func
}

// NewDOMInput wires keydown / keyup on window + mousemove /
// mousedown / mouseup / click on canvas + a beforeunload listener on
// window (synthesizes EventQuit). Returns the adapter.
func NewDOMInput(canvas js.Value) *DOMInput {
	d := &DOMInput{}
	d.install(canvas)
	return d
}

func (d *DOMInput) install(canvas js.Value) {
	win := js.Global().Get("window")

	d.keyDownCB = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		ev := args[0]
		// Suppress browser default behavior for keys we own (arrows
		// scroll the page; backquote opens DevTools console in some
		// browsers).
		ev.Call("preventDefault")
		// Drop repeats: the wasm Backend's mapKey already latches the
		// held bit on the press edge; the renderer treats every
		// repeat as a new down-edge otherwise.
		if ev.Get("repeat").Bool() {
			return nil
		}
		code := ev.Get("code").String()
		d.push(InputEvent{Kind: EventKey, Code: code, Value: 1})
		return nil
	})
	d.keyUpCB = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		ev := args[0]
		ev.Call("preventDefault")
		code := ev.Get("code").String()
		d.push(InputEvent{Kind: EventKey, Code: code, Value: 0})
		return nil
	})
	d.mouseMoveCB = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		ev := args[0]
		dx := ev.Get("movementX").Int()
		dy := ev.Get("movementY").Int()
		if dx != 0 {
			d.push(InputEvent{Kind: EventRelX, Value: int32(dx)})
		}
		if dy != 0 {
			d.push(InputEvent{Kind: EventRelY, Value: int32(dy)})
		}
		return nil
	})
	d.mouseDownCB = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		ev := args[0]
		btn := ev.Get("button").Int()
		code := mouseButtonCode(btn)
		if code == "" {
			return nil
		}
		d.push(InputEvent{Kind: EventMouseDown, Code: code, Value: 1})
		return nil
	})
	d.mouseUpCB = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		ev := args[0]
		btn := ev.Get("button").Int()
		code := mouseButtonCode(btn)
		if code == "" {
			return nil
		}
		d.push(InputEvent{Kind: EventMouseUp, Code: code, Value: 0})
		return nil
	})
	d.clickCB = js.FuncOf(func(this js.Value, _ []js.Value) any {
		// Pointer-lock requests must originate from a user gesture
		// or the browser ignores them. This handler runs in click
		// dispatch which qualifies.
		if this.Get("requestPointerLock").Truthy() {
			this.Call("requestPointerLock")
		}
		return nil
	})
	d.beforeUnload = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		d.push(InputEvent{Kind: EventQuit})
		return nil
	})

	win.Call("addEventListener", "keydown", d.keyDownCB)
	win.Call("addEventListener", "keyup", d.keyUpCB)
	canvas.Call("addEventListener", "mousemove", d.mouseMoveCB)
	canvas.Call("addEventListener", "mousedown", d.mouseDownCB)
	canvas.Call("addEventListener", "mouseup", d.mouseUpCB)
	canvas.Call("addEventListener", "click", d.clickCB)
	win.Call("addEventListener", "beforeunload", d.beforeUnload)
}

// push appends one event under the mutex.
func (d *DOMInput) push(ev InputEvent) {
	d.mu.Lock()
	d.events = append(d.events, ev)
	d.mu.Unlock()
}

// PollEvents drains + returns the queued events. Safe to call from
// the main loop.
func (d *DOMInput) PollEvents() ([]InputEvent, error) {
	d.mu.Lock()
	out := d.events
	d.events = nil
	d.mu.Unlock()
	return out, nil
}

// mouseButtonCode is defined in input_common.go (testable on host).
