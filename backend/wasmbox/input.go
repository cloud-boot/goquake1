// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js && wasm

package wasmbox

import (
	"sync"
	"syscall/js"
)

// ProtocolInput is an InputDevice that translates the wasmbox
// compositor's `{type:"input", event:{...}}` messages into the
// abstracted []InputEvent stream the Backend consumes. Event listeners
// are registered ONCE at construction on the supplied worker globalThis
// (the same target that PresentRGBA posts commits through).
//
// Concurrency: GOOS=js GOARCH=wasm is single-threaded (cooperative
// scheduling between Go goroutines + the JS event loop) but a
// sync.Mutex still guards the queue against future multithreaded wasm
// runtimes.
type ProtocolInput struct {
	mu      sync.Mutex
	events  []InputEvent
	tracker MouseTracker

	// Retained js.Func references so listener removal (if ever needed)
	// can pass the same callback back to removeEventListener.
	messageCB js.Func
}

// NewProtocolInput wires the worker's `message` event listener.
// onMessage demultiplexes by msg.type and only acts on type=="input";
// other message types (welcome, closed) are routed through the
// HandshakeHandler installed by the factory.
//
// The handshake's onmessage listener is removed before this is called
// (so we don't get a duplicate dispatch); after wiring, no further
// welcome/closed messages are expected on the same channel.
func NewProtocolInput(self js.Value) *ProtocolInput {
	p := &ProtocolInput{}
	p.install(self)
	return p
}

func (p *ProtocolInput) install(self js.Value) {
	p.messageCB = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		ev := args[0]
		data := ev.Get("data")
		if !data.Truthy() {
			return nil
		}
		if debugInput {
			println("WASMBOX-MSG type=", data.Get("type").String())
		}
		typ := data.Get("type")
		if !typ.Truthy() || typ.Type() != js.TypeString {
			return nil
		}
		switch typ.String() {
		case "input":
			p.handleInput(data.Get("event"))
		case "closed":
			p.push(InputEvent{Kind: EventQuit})
		}
		return nil
	})
	self.Call("addEventListener", "message", p.messageCB)
}

// handleInput decodes one wasmbox event payload into one or more
// abstracted events via DecodeInputEvent.
func (p *ProtocolInput) handleInput(event js.Value) {
	if !event.Truthy() {
		return
	}
	kind := jsString(event.Get("kind"))
	code := jsString(event.Get("code"))
	button := jsInt(event.Get("button"))
	x := jsInt(event.Get("x"))
	y := jsInt(event.Get("y"))
	if debugInput {
		println("WASMBOX-INPUT:", kind, code, button)
	}
	out := DecodeInputEvent(&p.tracker, kind, code, button, x, y)
	if len(out) == 0 {
		return
	}
	p.mu.Lock()
	p.events = append(p.events, out...)
	p.mu.Unlock()
}

// push appends one event under the mutex.
func (p *ProtocolInput) push(ev InputEvent) {
	p.mu.Lock()
	p.events = append(p.events, ev)
	p.mu.Unlock()
}

// PollEvents drains + returns the queued events. Safe to call from the
// main loop.
func (p *ProtocolInput) PollEvents() ([]InputEvent, error) {
	p.mu.Lock()
	out := p.events
	p.events = nil
	p.mu.Unlock()
	return out, nil
}

// jsString returns the string value of a JS field or "" if the field
// is null/undefined/non-string.
func jsString(v js.Value) string {
	if !v.Truthy() {
		return ""
	}
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}

// jsInt returns the int value of a JS field or 0 if the field is not
// a number.
func jsInt(v js.Value) int {
	if !v.Truthy() && v.Type() != js.TypeNumber {
		return 0
	}
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Int()
}

// debugInput, when true, prints every decoded compositor input event to
// the JS console so the browser-verification harness can confirm the
// key/mouse stream actually reaches the wasm backend. Off by default.
var debugInput = false
