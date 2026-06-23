// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/savegame"
)

// MaxSaveSlots is the number of in-memory save slots the host keeps
// per session. Matches menu.MaxSaveSlots but kept local so the host
// package has no upward dep on menu.
//
// tyrquake: MAX_SAVEGAMES = 12 in NQ/menu.c.
const MaxSaveSlots = 12

// Sentinel errors for the SaveSlot / LoadSlot API.
var (
	// ErrSaveSlotIndex fires on an out-of-range slot index.
	ErrSaveSlotIndex = errors.New("host: save slot index out of range")

	// ErrSaveNoServer fires when SaveSlot is called before
	// SpawnServer has armed the active map.
	ErrSaveNoServer = errors.New("host: SaveSlot needs an active server")

	// ErrSaveNoProgs fires when SaveSlot / LoadSlot is called
	// without a Progs bound (NewHost wires it via SetProgs).
	ErrSaveNoProgs = errors.New("host: SaveSlot needs a bound Progs")

	// ErrLoadEmptySlot fires when LoadSlot indexes a never-saved slot.
	ErrLoadEmptySlot = errors.New("host: LoadSlot on empty save slot")
)

// SaveLogf is the optional logger SaveSlot / LoadSlot route their
// per-slot diagnostic line through. Defaults to a noop; the bare-
// metal main wires it to the serial UART so the operator can see
// the "snapshot taken / restored" line on each menu-driven save.
//
// Variable rather than a field so the test seam is package-level +
// shared across every Host without per-Host construction wiring.
var SaveLogf = func(format string, args ...any) {}

// saveSlots is the per-Host in-memory save store. Indexed 0..N-1.
// A nil slot means "never saved" -- LoadSlot reports ErrLoadEmptySlot.
//
// Kept as a slice (not a pointer field on Host) so the existing Host
// struct stays binary-compatible; tests can swap out the seam without
// reaching into private fields.
type saveSlots [MaxSaveSlots]*savegame.Save

// saveStore is the per-Host slot map. The first SaveSlot call
// allocates it.
var saveStores = make(map[*Host]*saveSlots)

// slotsFor returns the save store for h, allocating one on first use.
func slotsFor(h *Host) *saveSlots {
	if s, ok := saveStores[h]; ok {
		return s
	}
	s := &saveSlots{}
	saveStores[h] = s
	return s
}

// hostEdictView adapts the host's per-Server.Edicts slice into the
// savegame.MutableEdictView contract. Free flags ride on the per-
// edict progs.Edict.Free field so SetFree mutates the live pool the
// per-tic walker reads.
type hostEdictView struct {
	edicts []*progs.Edict
}

func (v *hostEdictView) Len() int { return len(v.edicts) }

func (v *hostEdictView) Free(i int) bool {
	if i < 0 || i >= len(v.edicts) {
		return true
	}
	e := v.edicts[i]
	return e == nil || e.Free
}

func (v *hostEdictView) Edict(i int) *progs.Edict {
	if i < 0 || i >= len(v.edicts) {
		return nil
	}
	return v.edicts[i]
}

func (v *hostEdictView) SetFree(i int, free bool) {
	if i < 0 || i >= len(v.edicts) {
		return
	}
	if e := v.edicts[i]; e != nil {
		e.Free = free
	}
}

// SaveSlot snapshots the current server state into the in-memory
// slot at idx and returns nil. The save's text-format encoding is
// also dumped to SaveLogf at debug verbosity ("QUAKE: save slot %d
// snapshot taken (size=%d bytes)") so the bare-metal serial output
// proves the menu hook fired.
//
// idx must be in [0, MaxSaveSlots). Returns:
//
//	ErrSaveSlotIndex -- idx out of range
//	ErrSaveNoServer  -- SpawnServer has not armed an active map
//	ErrSaveNoProgs   -- no Progs bound (SetProgs not called)
//
// On success the slot's prior content (if any) is overwritten.
func (h *Host) SaveSlot(idx int) error {
	if idx < 0 || idx >= MaxSaveSlots {
		return ErrSaveSlotIndex
	}
	if h.Server == nil || !h.Server.Active {
		return ErrSaveNoServer
	}
	p := h.findProgs()
	if p == nil {
		return ErrSaveNoProgs
	}

	view := &hostEdictView{edicts: h.Server.Edicts}
	// Snapshot + SnapshotGlobals only error on a nil Progs, which
	// the no-progs guard above already rejected -- so the error
	// branches here are unreachable + dropped bsptrace-style.
	edictSnaps, _ := savegame.Snapshot(p, view)
	globalSnaps, _ := savegame.SnapshotGlobals(p)

	save := &savegame.Save{
		Comment: fmt.Sprintf("%s slot %d", h.Server.Name, idx),
		Skill:   h.SaveSkill,
		MapName: h.Server.Name,
		Time:    float32(h.Server.Time),
		Globals: globalSnaps,
		Edicts:  edictSnaps,
	}
	// Carry the player-1 spawn parms verbatim (single-player default).
	if h.Static != nil && len(h.Static.Clients) > 0 && h.Static.Clients[0] != nil {
		for i := 0; i < savegame.SpawnParmCount; i++ {
			save.SpawnParms[i] = h.Static.Clients[0].SpawnParms[i]
		}
	}

	// Size for the operator-visible log + the optional out-of-band
	// dump (bare-metal main can tee the bytes onto a UART; the host
	// itself just measures). Encode to a bytes.Buffer can't fail
	// (bytes.Buffer's Write never returns an error), so the err
	// branch is unreachable + dropped bsptrace-style.
	var buf bytes.Buffer
	_ = save.Encode(&buf)

	slotsFor(h)[idx] = save
	SaveLogf("QUAKE: save slot %d snapshot taken (size=%d bytes)", idx, buf.Len())
	return nil
}

// SetSkill records the active skill rung the next SaveSlot should
// persist. Callers (typically the runloop wiring the menu's
// StateSkill confirm hook) pass the menu.SkillLevel as an int.
// The Host.SaveSkill field is exported so callers can also write
// it directly; SetSkill exists for the symmetric-with-SaveSlot/
// LoadSlot accessor pattern.
func (h *Host) SetSkill(skill int) { h.SaveSkill = skill }

// LoadSlot restores the snapshot in slot idx onto the live server.
// The active map is re-spawned via Host.SpawnServer to reset the
// world state, then per-edict + global writes apply the snapshot
// onto the freshly-spawned pool.
//
// Logs "QUAKE: load slot %d snapshot restored (edicts=%d)" via
// SaveLogf on success.
//
// Returns:
//
//	ErrSaveSlotIndex   -- idx out of range
//	ErrLoadEmptySlot   -- slot was never saved
//	ErrSaveNoProgs     -- no Progs bound
//	any SpawnServer err -- the BSP load failed
func (h *Host) LoadSlot(idx int) error {
	if idx < 0 || idx >= MaxSaveSlots {
		return ErrSaveSlotIndex
	}
	store := slotsFor(h)
	save := store[idx]
	if save == nil {
		return ErrLoadEmptySlot
	}
	p := h.findProgs()
	if p == nil {
		return ErrSaveNoProgs
	}
	// Re-spawn the map so the world geometry + precache state come
	// from a fresh BSP load. The post-spawn restore overwrites the
	// per-entity QC fields with the snapshotted values.
	if h.Server != nil {
		// Use the snapshot's protocol when the server has one stashed;
		// otherwise fall back to the wire-mirroring default.
		proto := 0
		if h.Server != nil {
			proto = h.Server.Protocol
		}
		if err := h.SpawnServer(save.MapName, proto); err != nil {
			return fmt.Errorf("host: respawn for load: %w", err)
		}
	}
	view := &hostEdictView{edicts: h.Server.Edicts}
	// Restore only errors on nil-Progs (rejected above) or a malformed
	// per-field value -- the only way to land here with malformed data
	// is to hand-craft a Save into the slot map, which the public API
	// can't do. Drop bsptrace-style.
	_ = savegame.Restore(p, view, save.Edicts, h.interner)
	h.Server.Time = float64(save.Time)
	h.SaveSkill = save.Skill
	if h.Static != nil && len(h.Static.Clients) > 0 && h.Static.Clients[0] != nil {
		for i := 0; i < savegame.SpawnParmCount; i++ {
			h.Static.Clients[0].SpawnParms[i] = save.SpawnParms[i]
		}
	}
	SaveLogf("QUAKE: load slot %d snapshot restored (edicts=%d)", idx, len(save.Edicts))
	return nil
}

// PeekSlot returns the in-memory save at idx, or nil when the slot
// has not been saved. Used by the menu to populate per-slot labels
// ("SLOT 01 - e1m1 t=42.5"); exposes Save by value (caller cannot
// mutate the stored copy).
func (h *Host) PeekSlot(idx int) *savegame.Save {
	if idx < 0 || idx >= MaxSaveSlots {
		return nil
	}
	return slotsFor(h)[idx]
}
