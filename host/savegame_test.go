// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/savegame"
)

// resetSaveStores wipes the package-level saveStores map so per-test
// state doesn't leak across the suite. Run from t.Cleanup at the
// top of each test that touches SaveSlot / LoadSlot.
func resetSaveStores() {
	saveStores = make(map[*Host]*saveSlots)
}

// captureSaveLog redirects SaveLogf into a string slice for the
// duration of the test, restoring the prior logger on cleanup.
func captureSaveLog(t *testing.T) *[]string {
	t.Helper()
	prev := SaveLogf
	t.Cleanup(func() { SaveLogf = prev })
	out := []string{}
	SaveLogf = func(format string, args ...any) {
		out = append(out, format)
	}
	return &out
}

// --- SaveSlot ----------------------------------------------------------------

func TestSaveSlot_OutOfRange(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	if err := h.SaveSlot(-1); !errors.Is(err, ErrSaveSlotIndex) {
		t.Errorf("got %v want ErrSaveSlotIndex", err)
	}
	if err := h.SaveSlot(MaxSaveSlots); !errors.Is(err, ErrSaveSlotIndex) {
		t.Errorf("got %v want ErrSaveSlotIndex", err)
	}
}

func TestSaveSlot_NoServer(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	// Server is not Active until SpawnServer runs.
	if err := h.SaveSlot(0); !errors.Is(err, ErrSaveNoServer) {
		t.Errorf("got %v want ErrSaveNoServer", err)
	}
}

func TestSaveSlot_NoProgs(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	// Drop the progs ref + force-arm the server so the no-progs guard
	// is the only failing check.
	h.SetProgs(nil)
	h.Server.Active = true
	if err := h.SaveSlot(0); !errors.Is(err, ErrSaveNoProgs) {
		t.Errorf("got %v want ErrSaveNoProgs", err)
	}
}

func TestSaveSlot_HappyPath(t *testing.T) {
	resetSaveStores()
	log := captureSaveLog(t)
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("e1m1", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	h.SaveSkill = 2
	// Pre-populate a spawnparm so the carry-over branch fires.
	h.Static.Clients[0].SpawnParms[0] = 3.5

	if err := h.SaveSlot(3); err != nil {
		t.Fatalf("SaveSlot: %v", err)
	}
	save := h.PeekSlot(3)
	if save == nil {
		t.Fatalf("PeekSlot(3) = nil")
	}
	if save.MapName != "e1m1" {
		t.Errorf("MapName: got %q want e1m1", save.MapName)
	}
	if save.Skill != 2 {
		t.Errorf("Skill: got %d want 2", save.Skill)
	}
	if save.SpawnParms[0] != 3.5 {
		t.Errorf("SpawnParms[0]: got %v want 3.5", save.SpawnParms[0])
	}
	if len(*log) != 1 {
		t.Errorf("SaveLogf calls: got %d want 1, log=%v", len(*log), *log)
	}
}

func TestSaveSlot_NoClient(t *testing.T) {
	// MaxClients=0 -> Static.Clients is empty -> SpawnParms carry-over
	// short-circuits; covers the len()==0 branch.
	resetSaveStores()
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("e1m1", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	// Drop the client to exercise the nil-client branch.
	h.Static.Clients[0] = nil
	if err := h.SaveSlot(0); err != nil {
		t.Fatalf("SaveSlot: %v", err)
	}
}

// --- LoadSlot ----------------------------------------------------------------

func TestLoadSlot_OutOfRange(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	if err := h.LoadSlot(-1); !errors.Is(err, ErrSaveSlotIndex) {
		t.Errorf("got %v want ErrSaveSlotIndex", err)
	}
	if err := h.LoadSlot(MaxSaveSlots); !errors.Is(err, ErrSaveSlotIndex) {
		t.Errorf("got %v want ErrSaveSlotIndex", err)
	}
}

func TestLoadSlot_EmptySlot(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	if err := h.LoadSlot(0); !errors.Is(err, ErrLoadEmptySlot) {
		t.Errorf("got %v want ErrLoadEmptySlot", err)
	}
}

func TestLoadSlot_NoProgs(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	// Manually park a save in slot 0 (bypassing SaveSlot so the no-
	// progs branch is the only failing check).
	slotsFor(h)[0] = &savegame.Save{MapName: "e1m1"}
	h.SetProgs(nil)
	if err := h.LoadSlot(0); !errors.Is(err, ErrSaveNoProgs) {
		t.Errorf("got %v want ErrSaveNoProgs", err)
	}
}

func TestLoadSlot_RespawnFails(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	// Park a save referencing a map the resolver can't possibly
	// reload (overwrite resolver with a failure-only stub).
	slotsFor(h)[0] = &savegame.Save{MapName: ""}
	if err := h.LoadSlot(0); err == nil {
		t.Errorf("expected respawn to fail with empty MapName")
	}
}

func TestLoadSlot_HappyPath(t *testing.T) {
	resetSaveStores()
	log := captureSaveLog(t)
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("e1m1", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	// Mutate an edict so we can prove restore rewrote it.
	e := h.Server.Edicts[0]
	_ = e.FieldSetVector(2, [3]float32{42, 0, 0}) // "origin" via progsForHost

	if err := h.SaveSlot(0); err != nil {
		t.Fatalf("SaveSlot: %v", err)
	}
	// Clobber the field; LoadSlot should restore it.
	_ = e.FieldSetVector(2, [3]float32{99, 99, 99})

	if err := h.LoadSlot(0); err != nil {
		t.Fatalf("LoadSlot: %v", err)
	}
	got, _ := h.Server.Edicts[0].FieldVector(2)
	if got != [3]float32{42, 0, 0} {
		t.Errorf("post-load origin: got %v want [42 0 0]", got)
	}
	// 1 SaveLogf for the save + 1 for the load.
	if len(*log) != 2 {
		t.Errorf("SaveLogf calls: got %d want 2", len(*log))
	}
}

func TestLoadSlot_RestoresSpawnParms(t *testing.T) {
	resetSaveStores()
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("e1m1", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	h.Static.Clients[0].SpawnParms[5] = 9.25
	if err := h.SaveSlot(1); err != nil {
		t.Fatalf("SaveSlot: %v", err)
	}
	h.Static.Clients[0].SpawnParms[5] = 0
	if err := h.LoadSlot(1); err != nil {
		t.Fatalf("LoadSlot: %v", err)
	}
	if h.Static.Clients[0].SpawnParms[5] != 9.25 {
		t.Errorf("SpawnParms[5]: got %v want 9.25", h.Static.Clients[0].SpawnParms[5])
	}
}

func TestLoadSlot_NilClient(t *testing.T) {
	resetSaveStores()
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("e1m1", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if err := h.SaveSlot(0); err != nil {
		t.Fatalf("SaveSlot: %v", err)
	}
	// Drop the client so the spawnparm-restore branch short-circuits.
	h.Static.Clients[0] = nil
	if err := h.LoadSlot(0); err != nil {
		t.Errorf("LoadSlot: %v", err)
	}
}

// --- PeekSlot ----------------------------------------------------------------

func TestPeekSlot_OutOfRange(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	if got := h.PeekSlot(-1); got != nil {
		t.Errorf("PeekSlot(-1) = %+v, want nil", got)
	}
	if got := h.PeekSlot(MaxSaveSlots); got != nil {
		t.Errorf("PeekSlot(MaxSaveSlots) = %+v, want nil", got)
	}
}

func TestPeekSlot_Empty(t *testing.T) {
	resetSaveStores()
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	if got := h.PeekSlot(0); got != nil {
		t.Errorf("PeekSlot(0) on fresh host = %+v, want nil", got)
	}
}

// --- SetSkill ----------------------------------------------------------------

func TestSetSkill(t *testing.T) {
	h, _ := makeHost(t, buildHostBSP(t, `{ "classname" "worldspawn" }`, 1), 1)
	h.SetSkill(3)
	if h.SaveSkill != 3 {
		t.Errorf("SaveSkill: got %d want 3", h.SaveSkill)
	}
}

// --- hostEdictView -----------------------------------------------------------

func TestHostEdictView_OutOfRange(t *testing.T) {
	v := &hostEdictView{edicts: []*progs.Edict{{}}}
	if !v.Free(-1) {
		t.Errorf("Free(-1) = false, want true (out-of-range)")
	}
	if !v.Free(99) {
		t.Errorf("Free(99) = false, want true (out-of-range)")
	}
	if v.Edict(-1) != nil {
		t.Errorf("Edict(-1) != nil")
	}
	if v.Edict(99) != nil {
		t.Errorf("Edict(99) != nil")
	}
	// SetFree on out-of-range is a no-op (doesn't panic).
	v.SetFree(-1, true)
	v.SetFree(99, true)
	// SetFree on a nil edict is also a no-op.
	v.edicts[0] = nil
	v.SetFree(0, true)
}

func TestHostEdictView_HappyPath(t *testing.T) {
	e := &progs.Edict{}
	v := &hostEdictView{edicts: []*progs.Edict{e}}
	if v.Len() != 1 {
		t.Errorf("Len(): got %d want 1", v.Len())
	}
	if v.Free(0) {
		t.Errorf("Free(0): got true (default Free=false), want false")
	}
	v.SetFree(0, true)
	if !v.Free(0) {
		t.Errorf("Free(0) post-SetFree(true): got false want true")
	}
	if v.Edict(0) != e {
		t.Errorf("Edict(0) != e")
	}
}
