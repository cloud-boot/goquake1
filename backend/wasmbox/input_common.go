// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasmbox

// MouseTracker maintains the last surface-local mouse coordinates so
// successive mousemove events can be turned into relative deltas. The
// wasmbox compositor forwards absolute surface-local x,y on every
// mousemove (it has already translated by the window's screen
// position); there is no Pointer Lock dance in step B. The first move
// after a tracker reset establishes the origin and emits no delta.
//
// Kept in a non-tagged file so the math is host-testable.
type MouseTracker struct {
	hasLast bool
	lastX   int
	lastY   int
}

// Move feeds one mousemove (x, y in surface-local pixels) and returns
// the relative deltas since the previous Move. dxValid is false on the
// first call after construction or Reset (so the first sample seeds the
// baseline without firing a spurious motion event).
func (t *MouseTracker) Move(x, y int) (dx, dy int, dxValid bool) {
	if !t.hasLast {
		t.hasLast = true
		t.lastX = x
		t.lastY = y
		return 0, 0, false
	}
	dx = x - t.lastX
	dy = y - t.lastY
	t.lastX = x
	t.lastY = y
	return dx, dy, true
}

// Reset drops the cached coordinates so the next Move re-seeds the
// baseline. Used when focus is transferred away from + back to the
// wasmbox window: the compositor will resume with a fresh absolute
// position and we don't want a giant jump delta on resume.
func (t *MouseTracker) Reset() {
	t.hasLast = false
	t.lastX = 0
	t.lastY = 0
}

// DecodeInputEvent translates one wasmbox-protocol input event payload
// (the .event field of a {type:"input", event:{...}} message) into the
// abstracted []InputEvent slice the Backend consumes. The kind +
// fields it inspects are:
//
//   - "keydown"   → EventKey, Value=1, Code=code
//   - "keyup"     → EventKey, Value=0, Code=code
//   - "mousedown" → EventMouseDown, Code=MouseButtonCode(button) +
//     a Move on the tracker (so a click without a prior
//     move still seeds the baseline)
//   - "mouseup"   → EventMouseUp,   Code=MouseButtonCode(button)
//   - "mousemove" → one EventRelX + one EventRelY if the tracker has a
//     baseline; the first move seeds it silently
//   - "wheel"     → ignored for now (engine has no wheel-bound action;
//     reserved for follow-up)
//
// Unknown kinds + un-mappable mouse buttons produce no events. The
// helper returns nil rather than an empty slice when nothing was
// produced so callers can cheaply detect "no-op".
func DecodeInputEvent(tracker *MouseTracker, kind string, code string, button int, x, y int) []InputEvent {
	switch kind {
	case "keydown":
		if code == "" {
			return nil
		}
		return []InputEvent{{Kind: EventKey, Code: code, Value: 1}}
	case "keyup":
		if code == "" {
			return nil
		}
		return []InputEvent{{Kind: EventKey, Code: code, Value: 0}}
	case "mousedown":
		mc := MouseButtonCode(button)
		if mc == "" {
			return nil
		}
		// Seed the tracker on first click so a subsequent move has a
		// baseline. We don't emit relative motion here; the click
		// position itself isn't a delta.
		if tracker != nil {
			tracker.Move(x, y)
		}
		return []InputEvent{{Kind: EventMouseDown, Code: mc, Value: 1}}
	case "mouseup":
		mc := MouseButtonCode(button)
		if mc == "" {
			return nil
		}
		return []InputEvent{{Kind: EventMouseUp, Code: mc, Value: 0}}
	case "mousemove":
		if tracker == nil {
			return nil
		}
		dx, dy, valid := tracker.Move(x, y)
		if !valid {
			return nil
		}
		var out []InputEvent
		if dx != 0 {
			out = append(out, InputEvent{Kind: EventRelX, Value: int32(dx)})
		}
		if dy != 0 {
			out = append(out, InputEvent{Kind: EventRelY, Value: int32(dy)})
		}
		return out
	}
	return nil
}

// FullDamage returns the rectangle that covers the entire surface.
// Pure-Go helper kept here for host-testability + reuse.
func FullDamage(w, h int) (x, y, width, height int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return 0, 0, w, h
}
