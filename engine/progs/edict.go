// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"encoding/binary"
	"errors"
	"math"
)

// Edict is one game entity. The on-disk layout is a fixed header
// (free flag + freetime) followed by the per-entity field block
// whose size is Progs.Header.EntityFields * 4 bytes.
//
// The renderer + collision code accesses fields by name via the
// FieldDefs table; the Field* accessors below do that lookup.
// tyrquake: edict_t.
type Edict struct {
	// Free marks the slot as available for the next Alloc; mirrors
	// edict_s.free. When true, the Fields slice is preserved (so
	// re-allocations re-use the same backing memory) but its
	// contents are not meaningful.
	Free bool

	// FreeTime is the server tic when Free was last set true.
	// tyrquake uses this to prevent re-allocating a freshly-freed
	// slot inside the same frame (the upstream comment: "first two
	// seconds keep the slot free").
	FreeTime float32

	// Fields is the raw entity-field byte block. Each field is a
	// 4-byte slot accessed via the FieldDefs[*].Ofs offsets. Use
	// FieldFloat / FieldInt / FieldVector / FieldString / FieldSet*
	// for typed access.
	Fields []byte
}

// EdictArena is the fixed-capacity edict pool the server allocates
// at boot. tyrquake holds the analogous sv.edicts array of
// MAX_EDICTS slots. The Go port wraps it in a value so tests can
// spin up isolated arenas without touching package state.
type EdictArena struct {
	progs    *Progs
	edicts   []Edict
	maxFreed int // sv.edicts upper bound on free slots before re-use
}

// Sentinel errors.
var (
	ErrArenaFull    = errors.New("progs: edict arena exhausted (no free slot)")
	ErrEdictIndex   = errors.New("progs: edict index out of range")
	ErrFieldOffset  = errors.New("progs: field offset out of range for entity field block")
)

// NewEdictArena allocates an arena of cap edicts sized to the
// progs.Header.EntityFields layout. The "world" entity (slot 0) is
// pre-cleared but NOT marked free so Alloc never returns it.
// tyrquake: ED_Init effectively builds sv.edicts of size MAX_EDICTS;
// the cap is operator-chosen.
func NewEdictArena(progs *Progs, cap int) *EdictArena {
	if progs == nil {
		panic("progs: NewEdictArena: nil progs")
	}
	if cap < 1 {
		cap = 1
	}
	fieldBytes := int(progs.Header.EntityFields) * globalSlotSize
	a := &EdictArena{
		progs:    progs,
		edicts:   make([]Edict, cap),
		maxFreed: cap,
	}
	for i := range a.edicts {
		a.edicts[i].Fields = make([]byte, fieldBytes)
		// World (slot 0) and every other slot start non-free; the
		// first allocator pass marks slots 1..cap free as needed.
	}
	return a
}

// Cap returns the arena's total slot count.
func (a *EdictArena) Cap() int { return len(a.edicts) }

// Get returns the edict at index n. World is n=0; n in [1, Cap)
// addresses a game entity. tyrquake: EDICT_NUM.
func (a *EdictArena) Get(n int) (*Edict, error) {
	if n < 0 || n >= len(a.edicts) {
		return nil, ErrEdictIndex
	}
	return &a.edicts[n], nil
}

// NumFor returns the slot index of e, or -1 when e is not part of
// this arena. tyrquake: NUM_FOR_EDICT.
func (a *EdictArena) NumFor(e *Edict) int {
	for i := range a.edicts {
		if &a.edicts[i] == e {
			return i
		}
	}
	return -1
}

// Alloc returns the next available free edict, marking it
// non-free. Returns ErrArenaFull when every non-world slot is
// in-use OR freshly freed within the upstream's 2-second window
// (the caller passes the current server tic + the window via
// AllocSince). The simple form below uses no freshness guard --
// any Free()d slot is immediately re-allocatable; AllocSince is
// for callers that want tyrquake's "freshly freed" exclusion.
//
// tyrquake: ED_Alloc.
func (a *EdictArena) Alloc() (*Edict, int, error) {
	for i := 1; i < len(a.edicts); i++ {
		if a.edicts[i].Free {
			a.edicts[i].Free = false
			a.edicts[i].FreeTime = 0
			a.clearFields(&a.edicts[i])
			return &a.edicts[i], i, nil
		}
	}
	return nil, -1, ErrArenaFull
}

// AllocSince is Alloc with tyrquake's freshness window: slots whose
// FreeTime is within freshSecs of now are skipped. Returns the
// first older free slot, or the first ever-allocated slot when the
// caller has filled the arena. Pass now=0 + freshSecs=0 to disable.
// tyrquake: ED_Alloc's `e->freetime < 2 || sv.time - e->freetime > 0.5`
// gate.
func (a *EdictArena) AllocSince(now, freshSecs float32) (*Edict, int, error) {
	for i := 1; i < len(a.edicts); i++ {
		e := &a.edicts[i]
		if !e.Free {
			continue
		}
		if now-e.FreeTime <= freshSecs {
			continue
		}
		e.Free = false
		e.FreeTime = 0
		a.clearFields(e)
		return e, i, nil
	}
	// Fall back to any free slot, freshness be damned.
	for i := 1; i < len(a.edicts); i++ {
		if a.edicts[i].Free {
			a.edicts[i].Free = false
			a.edicts[i].FreeTime = 0
			a.clearFields(&a.edicts[i])
			return &a.edicts[i], i, nil
		}
	}
	return nil, -1, ErrArenaFull
}

// Free marks e as free, stamps its FreeTime, and zeroes its
// field block so a future re-Alloc starts clean. tyrquake: ED_Free.
func (a *EdictArena) Free(e *Edict, now float32) {
	if e == nil {
		return
	}
	e.Free = true
	e.FreeTime = now
	// Field block is zeroed here (not at Alloc time) so the cleared
	// state is observable by intervening enumeration -- mirrors
	// tyrquake's "VectorCopy (vec3_origin, e->v.origin)" etc.
	a.clearFields(e)
}

// Count returns the number of non-free, non-world edicts.
// tyrquake: ED_Count.
func (a *EdictArena) Count() int {
	n := 0
	for i := 1; i < len(a.edicts); i++ {
		if !a.edicts[i].Free {
			n++
		}
	}
	return n
}

// Reset frees every slot and clears every field block. World stays
// non-free (Q1 invariant: slot 0 is always allocated).
func (a *EdictArena) Reset() {
	a.clearFields(&a.edicts[0])
	a.edicts[0].Free = false
	a.edicts[0].FreeTime = 0
	for i := 1; i < len(a.edicts); i++ {
		a.edicts[i].Free = true
		a.edicts[i].FreeTime = 0
		a.clearFields(&a.edicts[i])
	}
}

func (a *EdictArena) clearFields(e *Edict) {
	for i := range e.Fields {
		e.Fields[i] = 0
	}
}

// FieldBytes returns the per-edict field-block size in bytes (=
// Progs.Header.EntityFields * 4). The VM uses it to convert between
// edict pointers (stored in QuakeC global slots as int32 byte
// offsets relative to the start of the edict array) and (edict
// index, field offset) pairs.
func (a *EdictArena) FieldBytes() int { return int(a.progs.Header.EntityFields) * globalSlotSize }

// MakePointer encodes an (edict index, field slot offset) pair as
// the int32 byte offset the QuakeC OP_ADDRESS opcode writes into
// the global pool. fieldSlotOfs is in 4-byte slots, not bytes.
func (a *EdictArena) MakePointer(edictIdx int, fieldSlotOfs int) int32 {
	return int32(edictIdx*a.FieldBytes() + fieldSlotOfs*globalSlotSize)
}

// ResolvePointer is the inverse of MakePointer. Returns the
// addressed Edict plus the byte offset within its Fields slice the
// pointer denotes. Errors when the pointer would land outside the
// arena.
func (a *EdictArena) ResolvePointer(byteOfs int32) (*Edict, int, error) {
	if byteOfs < 0 {
		return nil, 0, ErrEdictIndex
	}
	fb := a.FieldBytes()
	if fb <= 0 {
		return nil, 0, ErrEdictIndex
	}
	idx := int(byteOfs) / fb
	off := int(byteOfs) % fb
	if idx < 0 || idx >= len(a.edicts) {
		return nil, 0, ErrEdictIndex
	}
	return &a.edicts[idx], off, nil
}

// PointerForEdict returns the QuakeC pointer (byte offset) that
// addresses the START of e's field block (= MakePointer(NumFor(e),
// 0)). Returns -1 when e is not in this arena.
func (a *EdictArena) PointerForEdict(e *Edict) int32 {
	idx := a.NumFor(e)
	if idx < 0 {
		return -1
	}
	return a.MakePointer(idx, 0)
}

// --- typed field accessors ---------------------------------------------------

// FieldFloat reads the field at the byte offset (ofs * 4 bytes from
// the start of e.Fields). tyrquake: E_FLOAT.
func (e *Edict) FieldFloat(ofs int) (float32, error) {
	off := ofs * globalSlotSize
	if off < 0 || off+4 > len(e.Fields) {
		return 0, ErrFieldOffset
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(e.Fields[off : off+4])), nil
}

// FieldSetFloat writes a float into the field at ofs. tyrquake:
// assignment via E_FLOAT(e, ofs) = v.
func (e *Edict) FieldSetFloat(ofs int, v float32) error {
	off := ofs * globalSlotSize
	if off < 0 || off+4 > len(e.Fields) {
		return ErrFieldOffset
	}
	binary.LittleEndian.PutUint32(e.Fields[off:off+4], math.Float32bits(v))
	return nil
}

// FieldInt reads the field at ofs as a signed int32. tyrquake:
// E_INT.
func (e *Edict) FieldInt(ofs int) (int32, error) {
	off := ofs * globalSlotSize
	if off < 0 || off+4 > len(e.Fields) {
		return 0, ErrFieldOffset
	}
	return int32(binary.LittleEndian.Uint32(e.Fields[off : off+4])), nil
}

// FieldSetInt writes an int32 into the field at ofs.
func (e *Edict) FieldSetInt(ofs int, v int32) error {
	off := ofs * globalSlotSize
	if off < 0 || off+4 > len(e.Fields) {
		return ErrFieldOffset
	}
	binary.LittleEndian.PutUint32(e.Fields[off:off+4], uint32(v))
	return nil
}

// FieldVector reads a 3-component vector starting at ofs. tyrquake:
// E_VECTOR.
func (e *Edict) FieldVector(ofs int) ([3]float32, error) {
	off := ofs * globalSlotSize
	if off < 0 || off+12 > len(e.Fields) {
		return [3]float32{}, ErrFieldOffset
	}
	return [3]float32{
		math.Float32frombits(binary.LittleEndian.Uint32(e.Fields[off : off+4])),
		math.Float32frombits(binary.LittleEndian.Uint32(e.Fields[off+4 : off+8])),
		math.Float32frombits(binary.LittleEndian.Uint32(e.Fields[off+8 : off+12])),
	}, nil
}

// FieldSetVector writes a 3-component vector starting at ofs.
func (e *Edict) FieldSetVector(ofs int, v [3]float32) error {
	off := ofs * globalSlotSize
	if off < 0 || off+12 > len(e.Fields) {
		return ErrFieldOffset
	}
	binary.LittleEndian.PutUint32(e.Fields[off:off+4], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(e.Fields[off+4:off+8], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(e.Fields[off+8:off+12], math.Float32bits(v[2]))
	return nil
}
