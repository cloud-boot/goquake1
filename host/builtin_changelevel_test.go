// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
)

// progsForChangelevel builds a tiny Progs that declares the
// `nextmap` named global + a string constant slot we pre-load with
// the offset of "e1m2". The builtin reads OFS_PARM0 (the first param
// slot) so we pre-load that with the string offset directly.
//
// String layout:
//
//	0:        ""    (the always-empty sentinel)
//	1..       "nextmap"
//	          "e1m2"
//	          "" (sentinel)
//
// Global layout (one slot = 4 bytes):
//
//	OFS_PARM0    (slot 4)   -- caller writes the string offset here
//	nextmapOfs   (slot 60)  -- declared as the QC `nextmap` global
func progsForChangelevel(mapName string) (*progs.Progs, int32) {
	strs := []byte{0}
	nextmapName := addStr(&strs, "nextmap")
	mapOff := addStr(&strs, mapName)

	const numGlobals = 96
	globals := make([]byte, numGlobals*4)

	const nextmapOfs = 60
	return &progs.Progs{
		Header:  progs.Header{EntityFields: 4},
		Strings: strs,
		GlobalDefs: []progs.Def{
			{Type: uint16(progs.EvString), Ofs: nextmapOfs, SName: nextmapName},
		},
		Globals: globals,
	}, mapOff
}

// progsForChangelevelNoNextmap omits the `nextmap` global so the
// FindGlobal lookup misses + the named-global write is skipped.
// The flag flip + NextMap record still happen.
func progsForChangelevelNoNextmap(mapName string) (*progs.Progs, int32) {
	strs := []byte{0}
	mapOff := addStr(&strs, mapName)
	const numGlobals = 96
	return &progs.Progs{
		Header:  progs.Header{EntityFields: 4},
		Strings: strs,
		Globals: make([]byte, numGlobals*4),
	}, mapOff
}

// changelevelHost wires a minimal Host with the supplied progs +
// VM. No World / Static needed (the builtin reads only OFS_PARM0 +
// writes a global + flips two host fields).
func changelevelHost(p *progs.Progs) *Host {
	vm := progs.NewVM(p)
	h := &Host{
		VM:     vm,
		Server: server.NewServer(),
	}
	h.SetProgs(p)
	return h
}

// Happy path: changelevel("e1m2") flips the flag, records the slug,
// and writes the string-offset into the QC `nextmap` global.
func TestBuiltinChangeLevel_HappyPath(t *testing.T) {
	p, mapOff := progsForChangelevel("e1m2")
	h := changelevelHost(p)
	if err := h.VM.SetGlobalInt(progs.OfsParm0, mapOff); err != nil {
		t.Fatalf("SetGlobalInt OFS_PARM0: %v", err)
	}

	fn := BuiltinChangeLevel(h)
	if err := fn(h.VM); err != nil {
		t.Fatalf("BuiltinChangeLevel: %v", err)
	}
	if !h.PendingChangelevel {
		t.Error("PendingChangelevel: got false want true")
	}
	if h.NextMap != "e1m2" {
		t.Errorf("NextMap: got %q want %q", h.NextMap, "e1m2")
	}
	// The named `nextmap` global must hold the same string offset we
	// wrote into OFS_PARM0.
	def := p.FindGlobal("nextmap")
	if def == nil {
		t.Fatal("FindGlobal(nextmap): nil")
	}
	got, err := h.VM.GlobalInt(int(def.Ofs))
	if err != nil {
		t.Fatalf("GlobalInt(nextmap): %v", err)
	}
	if got != mapOff {
		t.Errorf("nextmap global: got %d want %d", got, mapOff)
	}
}

// Empty OFS_PARM0 (= the empty-string sentinel) is a tolerated
// early-return: no flag flip, no NextMap write, no global write.
func TestBuiltinChangeLevel_EmptyParmIsNoop(t *testing.T) {
	p, _ := progsForChangelevel("e1m2")
	h := changelevelHost(p)
	// OFS_PARM0 stays 0 (= the empty-string sentinel) by default.
	fn := BuiltinChangeLevel(h)
	if err := fn(h.VM); err != nil {
		t.Fatalf("BuiltinChangeLevel: %v", err)
	}
	if h.PendingChangelevel {
		t.Error("PendingChangelevel: got true want false (empty parm)")
	}
	if h.NextMap != "" {
		t.Errorf("NextMap: got %q want empty", h.NextMap)
	}
}

// OFS_PARM0 holds a non-zero offset but the string at that offset
// is empty (the byte at strs[off] == 0). Also a no-op.
func TestBuiltinChangeLevel_EmptyStringIsNoop(t *testing.T) {
	p, _ := progsForChangelevel("e1m2")
	// Use offset 0 explicitly: addStr after the leading 0 starts at
	// 1, so offset 0 IS the empty-string sentinel. But we want a
	// non-zero offset whose byte is also zero; that requires
	// surgically zeroing the strs byte. Easier: re-build with a
	// real-but-empty add.
	strs := []byte{0, 0} // sentinel, then a second NUL
	p.Strings = strs
	h := changelevelHost(p)
	_ = h.VM.SetGlobalInt(progs.OfsParm0, 1) // points at the second NUL = empty
	fn := BuiltinChangeLevel(h)
	if err := fn(h.VM); err != nil {
		t.Fatalf("BuiltinChangeLevel: %v", err)
	}
	if h.PendingChangelevel {
		t.Error("PendingChangelevel: got true want false (empty string)")
	}
}

// nil host -- tolerated; no panic, no side effects to observe.
func TestBuiltinChangeLevel_NilHostNoPanic(t *testing.T) {
	fn := BuiltinChangeLevel(nil)
	p, _ := progsForChangelevel("e1m2")
	vm := progs.NewVM(p)
	if err := fn(vm); err != nil {
		t.Errorf("BuiltinChangeLevel(nil)(vm): err=%v want nil", err)
	}
}

// nextmap global absent -- still flips the flag + records the slug.
// The named-global write is silently skipped (matches the rest of
// the named-global hand-off pattern in this package).
func TestBuiltinChangeLevel_NextmapGlobalAbsent(t *testing.T) {
	p, mapOff := progsForChangelevelNoNextmap("e1m3")
	h := changelevelHost(p)
	_ = h.VM.SetGlobalInt(progs.OfsParm0, mapOff)
	fn := BuiltinChangeLevel(h)
	if err := fn(h.VM); err != nil {
		t.Fatalf("BuiltinChangeLevel: %v", err)
	}
	if !h.PendingChangelevel {
		t.Error("PendingChangelevel: got false want true")
	}
	if h.NextMap != "e1m3" {
		t.Errorf("NextMap: got %q want %q", h.NextMap, "e1m3")
	}
}

// ConsumeChangelevel returns (true, slug) once, then (false, "")
// thereafter (single-shot semantics).
func TestConsumeChangelevel_SingleShot(t *testing.T) {
	h := &Host{
		PendingChangelevel: true,
		NextMap:            "e2m1",
	}
	pending, slug := h.ConsumeChangelevel()
	if !pending || slug != "e2m1" {
		t.Fatalf("first consume: got (%v, %q) want (true, %q)", pending, slug, "e2m1")
	}
	pending2, slug2 := h.ConsumeChangelevel()
	if pending2 || slug2 != "" {
		t.Errorf("second consume: got (%v, %q) want (false, \"\")", pending2, slug2)
	}
	// Flag + slug must be cleared post-consume.
	if h.PendingChangelevel {
		t.Error("PendingChangelevel: got true want false after consume")
	}
	if h.NextMap != "" {
		t.Errorf("NextMap: got %q want empty after consume", h.NextMap)
	}
}

// ConsumeChangelevel with no pending request returns (false, "")
// without disturbing the (already-zero) host fields.
func TestConsumeChangelevel_NoPending(t *testing.T) {
	h := &Host{}
	pending, slug := h.ConsumeChangelevel()
	if pending || slug != "" {
		t.Errorf("got (%v, %q) want (false, \"\")", pending, slug)
	}
}
