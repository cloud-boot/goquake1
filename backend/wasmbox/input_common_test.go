// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasmbox

import (
	"reflect"
	"testing"
)

// ---- MouseTracker ----

func TestMouseTracker_firstMoveSeedsNoDelta(t *testing.T) {
	var tr MouseTracker
	dx, dy, valid := tr.Move(100, 50)
	if valid {
		t.Fatalf("first Move: valid=%v want false", valid)
	}
	if dx != 0 || dy != 0 {
		t.Fatalf("first Move: dx,dy=%d,%d want 0,0", dx, dy)
	}
}

func TestMouseTracker_subsequentDeltas(t *testing.T) {
	var tr MouseTracker
	tr.Move(100, 50)
	dx, dy, valid := tr.Move(110, 45)
	if !valid {
		t.Fatalf("second Move: valid=false")
	}
	if dx != 10 || dy != -5 {
		t.Fatalf("second Move: got %d,%d want 10,-5", dx, dy)
	}
	dx, dy, valid = tr.Move(100, 50)
	if !valid || dx != -10 || dy != 5 {
		t.Fatalf("third Move: got %d,%d valid=%v want -10,5,true", dx, dy, valid)
	}
}

func TestMouseTracker_resetClearsBaseline(t *testing.T) {
	var tr MouseTracker
	tr.Move(10, 20)
	tr.Move(30, 40)
	tr.Reset()
	dx, dy, valid := tr.Move(100, 200)
	if valid || dx != 0 || dy != 0 {
		t.Fatalf("after Reset: got %d,%d valid=%v want 0,0,false", dx, dy, valid)
	}
	// Next move after Reset+seed should produce a delta.
	dx, dy, valid = tr.Move(105, 195)
	if !valid || dx != 5 || dy != -5 {
		t.Fatalf("after Reset baseline: got %d,%d valid=%v want 5,-5,true", dx, dy, valid)
	}
}

// ---- DecodeInputEvent ----

func TestDecodeInputEvent_keydown(t *testing.T) {
	got := DecodeInputEvent(nil, "keydown", "KeyW", 0, 0, 0)
	want := []InputEvent{{Kind: EventKey, Code: "KeyW", Value: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keydown: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_keydownEmptyCodeDropped(t *testing.T) {
	if got := DecodeInputEvent(nil, "keydown", "", 0, 0, 0); got != nil {
		t.Fatalf("empty code: got %+v want nil", got)
	}
}

func TestDecodeInputEvent_keyup(t *testing.T) {
	got := DecodeInputEvent(nil, "keyup", "KeyA", 0, 0, 0)
	want := []InputEvent{{Kind: EventKey, Code: "KeyA", Value: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keyup: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_keyupEmptyCodeDropped(t *testing.T) {
	if got := DecodeInputEvent(nil, "keyup", "", 0, 0, 0); got != nil {
		t.Fatalf("empty code: got %+v want nil", got)
	}
}

func TestDecodeInputEvent_mousedownLeft(t *testing.T) {
	var tr MouseTracker
	got := DecodeInputEvent(&tr, "mousedown", "", 0, 50, 60)
	want := []InputEvent{{Kind: EventMouseDown, Code: "Mouse1", Value: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mousedown: got %+v want %+v", got, want)
	}
	// The click should have seeded the tracker baseline so a follow-up
	// mousemove produces a delta from (50,60).
	dx, dy, valid := tr.Move(55, 58)
	if !valid || dx != 5 || dy != -2 {
		t.Fatalf("post-click move: got %d,%d valid=%v want 5,-2,true", dx, dy, valid)
	}
}

func TestDecodeInputEvent_mousedownRight(t *testing.T) {
	got := DecodeInputEvent(nil, "mousedown", "", 2, 0, 0)
	want := []InputEvent{{Kind: EventMouseDown, Code: "Mouse2", Value: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("right mousedown: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_mousedownUnmappedDropped(t *testing.T) {
	if got := DecodeInputEvent(nil, "mousedown", "", 1, 0, 0); got != nil {
		t.Fatalf("middle button: got %+v want nil", got)
	}
}

func TestDecodeInputEvent_mousedownNilTracker(t *testing.T) {
	// nil tracker is allowed: the click still produces the press event.
	got := DecodeInputEvent(nil, "mousedown", "", 0, 0, 0)
	if len(got) != 1 || got[0].Kind != EventMouseDown {
		t.Fatalf("nil tracker mousedown: got %+v", got)
	}
}

func TestDecodeInputEvent_mouseup(t *testing.T) {
	got := DecodeInputEvent(nil, "mouseup", "", 0, 0, 0)
	want := []InputEvent{{Kind: EventMouseUp, Code: "Mouse1", Value: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mouseup: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_mouseupUnmappedDropped(t *testing.T) {
	if got := DecodeInputEvent(nil, "mouseup", "", 1, 0, 0); got != nil {
		t.Fatalf("middle mouseup: got %+v want nil", got)
	}
}

func TestDecodeInputEvent_mousemoveNilTracker(t *testing.T) {
	if got := DecodeInputEvent(nil, "mousemove", "", 0, 100, 200); got != nil {
		t.Fatalf("nil tracker mousemove: got %+v want nil", got)
	}
}

func TestDecodeInputEvent_mousemoveFirstSeedsBaseline(t *testing.T) {
	var tr MouseTracker
	if got := DecodeInputEvent(&tr, "mousemove", "", 0, 100, 200); got != nil {
		t.Fatalf("first mousemove: got %+v want nil (silent baseline)", got)
	}
}

func TestDecodeInputEvent_mousemoveProducesDelta(t *testing.T) {
	var tr MouseTracker
	DecodeInputEvent(&tr, "mousemove", "", 0, 100, 200)
	got := DecodeInputEvent(&tr, "mousemove", "", 0, 110, 195)
	want := []InputEvent{
		{Kind: EventRelX, Value: 10},
		{Kind: EventRelY, Value: -5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mousemove delta: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_mousemoveOnlyXDelta(t *testing.T) {
	var tr MouseTracker
	DecodeInputEvent(&tr, "mousemove", "", 0, 100, 200)
	got := DecodeInputEvent(&tr, "mousemove", "", 0, 110, 200)
	want := []InputEvent{{Kind: EventRelX, Value: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("x-only delta: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_mousemoveOnlyYDelta(t *testing.T) {
	var tr MouseTracker
	DecodeInputEvent(&tr, "mousemove", "", 0, 100, 200)
	got := DecodeInputEvent(&tr, "mousemove", "", 0, 100, 195)
	want := []InputEvent{{Kind: EventRelY, Value: -5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("y-only delta: got %+v want %+v", got, want)
	}
}

func TestDecodeInputEvent_mousemoveNoDelta(t *testing.T) {
	var tr MouseTracker
	DecodeInputEvent(&tr, "mousemove", "", 0, 100, 200)
	if got := DecodeInputEvent(&tr, "mousemove", "", 0, 100, 200); got != nil {
		t.Fatalf("no-motion mousemove: got %+v want nil", got)
	}
}

func TestDecodeInputEvent_unknownKindDropped(t *testing.T) {
	if got := DecodeInputEvent(nil, "wheel", "", 0, 0, 0); got != nil {
		t.Fatalf("wheel: got %+v want nil (deferred)", got)
	}
	if got := DecodeInputEvent(nil, "", "", 0, 0, 0); got != nil {
		t.Fatalf("empty kind: got %+v want nil", got)
	}
	if got := DecodeInputEvent(nil, "unknown-kind", "", 0, 0, 0); got != nil {
		t.Fatalf("unknown kind: got %+v want nil", got)
	}
}

// ---- FullDamage ----

func TestFullDamage(t *testing.T) {
	cases := []struct {
		w, h                       int
		wantX, wantY, wantW, wantH int
	}{
		{200, 150, 0, 0, 200, 150},
		{0, 0, 0, 0, 0, 0},
		{-5, 10, 0, 0, 0, 10},
		{10, -5, 0, 0, 10, 0},
	}
	for _, c := range cases {
		x, y, w, h := FullDamage(c.w, c.h)
		if x != c.wantX || y != c.wantY || w != c.wantW || h != c.wantH {
			t.Errorf("FullDamage(%d,%d): got %d,%d,%d,%d want %d,%d,%d,%d",
				c.w, c.h, x, y, w, h, c.wantX, c.wantY, c.wantW, c.wantH)
		}
	}
}
