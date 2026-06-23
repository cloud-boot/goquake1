// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package savegame

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/go-quake1/engine/progs"
)

// EdictView is the minimal contract Snapshot reads to convert a live
// edict's field block into key/value text pairs. The host adapts a
// `[]*progs.Edict` slice + a bound Progs into this shape so this
// package stays free of any server-package dep.
type EdictView interface {
	// Len returns the number of slot rows the snapshot should walk.
	// Slot 0 is the worldspawn entity; slots 1..Len()-1 are clients
	// + monsters + items.
	Len() int

	// Free reports whether slot i is a free pool slot (no live
	// entity). Snapshot emits an empty "{}" placeholder for free
	// slots so the on-disk index stays aligned with the live pool
	// on restore.
	Free(i int) bool

	// Edict returns the raw *progs.Edict at slot i. nil for an
	// out-of-range / unallocated slot (Snapshot treats nil
	// identically to Free).
	Edict(i int) *progs.Edict
}

// ErrNilProgs fires when Snapshot / Restore is called without a bound
// Progs pointer. The field-name -> offset lookup needs the FieldDefs
// table.
var ErrNilProgs = errors.New("savegame: nil Progs")

// Snapshot walks view and dumps every non-free edict's QC fields
// into the returned []EdictSnap, ordered by slot index. Free slots
// emit empty placeholder snaps so the per-slot offsets stay aligned
// across save -> load.
//
// Per-field type rendering:
//
//	EvFloat    -- strconv "%g"
//	EvVector   -- three "%g" floats joined by spaces
//	EvString   -- raw string-table lookup (no quotes; the per-line
//	              format adds them via writeBlock)
//	EvEntity   -- decimal int32 (the slot index)
//	EvField    -- decimal int32 (the field-def index)
//	EvFunction -- decimal int32 (the function index)
//	EvPointer  -- decimal int32 (raw byte offset)
//	EvVoid     -- skipped (no on-wire shape)
//
// Fields whose type carries the DefSaveGlobal bit are emitted with
// the bit stripped (matches the upstream's ED_PrintEdict).
func Snapshot(p *progs.Progs, view EdictView) ([]EdictSnap, error) {
	if p == nil {
		return nil, ErrNilProgs
	}
	out := make([]EdictSnap, view.Len())
	for i := 0; i < view.Len(); i++ {
		if view.Free(i) {
			out[i] = EdictSnap{Free: true}
			continue
		}
		e := view.Edict(i)
		if e == nil {
			out[i] = EdictSnap{Free: true}
			continue
		}
		out[i] = EdictSnap{FieldKV: snapshotEdict(p, e)}
	}
	return out, nil
}

// snapshotEdict dumps one edict's fields in FieldDefs order. Field
// names are resolved via p.String(def.SName); a def whose SName is 0
// (the upstream's "field has no name" sentinel) is skipped because
// the per-name restore lookup can't address it.
func snapshotEdict(p *progs.Progs, e *progs.Edict) []KV {
	out := make([]KV, 0, len(p.FieldDefs))
	for di := range p.FieldDefs {
		def := &p.FieldDefs[di]
		name := p.String(def.SName)
		if name == "" {
			continue
		}
		t := progs.Etype(def.Type &^ uint16(progs.DefSaveGlobal))
		val, ok := renderField(p, e, def, t)
		if !ok {
			continue
		}
		out = append(out, KV{Key: name, Value: val})
	}
	return out
}

// renderField turns one (def, t) pair into the on-disk string value
// for the per-edict line. ok==false means "skip this field" (e.g.
// EvVoid, an unknown Etype, or an out-of-range field-offset on a
// custom test stub).
func renderField(p *progs.Progs, e *progs.Edict, def *progs.Def, t progs.Etype) (string, bool) {
	switch t {
	case progs.EvFloat:
		f, err := e.FieldFloat(int(def.Ofs))
		if err != nil {
			return "", false
		}
		return strconv.FormatFloat(float64(f), 'g', -1, 32), true
	case progs.EvVector:
		v, err := e.FieldVector(int(def.Ofs))
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%s %s %s",
			strconv.FormatFloat(float64(v[0]), 'g', -1, 32),
			strconv.FormatFloat(float64(v[1]), 'g', -1, 32),
			strconv.FormatFloat(float64(v[2]), 'g', -1, 32)), true
	case progs.EvString:
		off, err := e.FieldInt(int(def.Ofs))
		if err != nil {
			return "", false
		}
		return p.String(off), true
	case progs.EvEntity, progs.EvField, progs.EvFunction, progs.EvPointer:
		n, err := e.FieldInt(int(def.Ofs))
		if err != nil {
			return "", false
		}
		return strconv.FormatInt(int64(n), 10), true
	}
	// EvVoid and any future-unknown Etype: skip.
	return "", false
}

// MutableEdictView is the write-side analogue of EdictView. Restore
// uses it to drop a parsed value back into the live edict pool.
type MutableEdictView interface {
	EdictView

	// SetFree marks slot i free / non-free. Restore calls SetFree
	// when the on-disk slot is an empty "{}" placeholder OR when
	// the slot's field block has been freshly populated.
	SetFree(i int, free bool)
}

// Restore applies snaps onto view's edict pool: for each non-free
// snap, walk the (key, value) pairs and write the typed value into
// the matching field. Slot 0 (worldspawn) is restored verbatim like
// every other slot.
//
// The string interner is needed for EvString fields (the on-disk
// value is the resolved string bytes; the write side has to fold it
// back into the runtime string heap). nil = EvString fields are
// silently skipped (the rest of the per-edict restore still runs).
//
// Restore is best-effort per-field: a key that the live Progs no
// longer declares (mod swap mid-session) is skipped without an
// error. A typed write failure (out-of-range offset, malformed
// number) is propagated.
func Restore(p *progs.Progs, view MutableEdictView, snaps []EdictSnap, intern progs.StringInterner) error {
	if p == nil {
		return ErrNilProgs
	}
	for i := 0; i < view.Len() && i < len(snaps); i++ {
		if snaps[i].Free {
			view.SetFree(i, true)
			continue
		}
		e := view.Edict(i)
		if e == nil {
			continue
		}
		view.SetFree(i, false)
		if err := restoreEdict(p, e, snaps[i].FieldKV, intern); err != nil {
			return fmt.Errorf("savegame: restore edict %d: %w", i, err)
		}
	}
	return nil
}

func restoreEdict(p *progs.Progs, e *progs.Edict, kvs []KV, intern progs.StringInterner) error {
	for _, kv := range kvs {
		def := p.FindField(kv.Key)
		if def == nil {
			continue
		}
		t := progs.Etype(def.Type &^ uint16(progs.DefSaveGlobal))
		if err := applyField(p, e, def, t, kv.Value, intern); err != nil {
			return fmt.Errorf("field %q: %w", kv.Key, err)
		}
	}
	return nil
}

func applyField(p *progs.Progs, e *progs.Edict, def *progs.Def, t progs.Etype, val string, intern progs.StringInterner) error {
	switch t {
	case progs.EvFloat:
		f, err := strconv.ParseFloat(val, 32)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBadNumber, err)
		}
		return e.FieldSetFloat(int(def.Ofs), float32(f))
	case progs.EvVector:
		parts := strings.Fields(val)
		if len(parts) != 3 {
			return fmt.Errorf("%w: vec3 want 3 floats got %d", ErrBadNumber, len(parts))
		}
		var v [3]float32
		for i := 0; i < 3; i++ {
			f, err := strconv.ParseFloat(parts[i], 32)
			if err != nil {
				return fmt.Errorf("%w: vec3[%d]: %v", ErrBadNumber, i, err)
			}
			v[i] = float32(f)
		}
		return e.FieldSetVector(int(def.Ofs), v)
	case progs.EvString:
		if intern == nil {
			return nil
		}
		off := intern(val)
		_ = p // p reserved for future "intern into p.Strings directly" path
		return e.FieldSetInt(int(def.Ofs), off)
	case progs.EvEntity, progs.EvField, progs.EvFunction, progs.EvPointer:
		n, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBadNumber, err)
		}
		return e.FieldSetInt(int(def.Ofs), int32(n))
	}
	return nil
}

// SnapshotGlobals dumps every GlobalDef whose Type carries the
// DefSaveGlobal bit, in declaration order. Used by the host's
// SaveSlot path to persist the "spawn parm written" guard +
// nextmap + serverflags QC named globals.
//
// Same per-type rendering rules as Snapshot's per-edict pass.
func SnapshotGlobals(p *progs.Progs) ([]KV, error) {
	if p == nil {
		return nil, ErrNilProgs
	}
	out := make([]KV, 0, len(p.GlobalDefs))
	for di := range p.GlobalDefs {
		def := &p.GlobalDefs[di]
		if def.Type&uint16(progs.DefSaveGlobal) == 0 {
			continue
		}
		name := p.String(def.SName)
		if name == "" {
			continue
		}
		t := progs.Etype(def.Type &^ uint16(progs.DefSaveGlobal))
		val, ok := renderGlobal(p, def, t)
		if !ok {
			continue
		}
		out = append(out, KV{Key: name, Value: val})
	}
	return out, nil
}

func renderGlobal(p *progs.Progs, def *progs.Def, t progs.Etype) (string, bool) {
	off := int(def.Ofs) * 4
	if off < 0 || off+4 > len(p.Globals) {
		return "", false
	}
	switch t {
	case progs.EvFloat:
		f := float32frombits(p.Globals[off : off+4])
		return strconv.FormatFloat(float64(f), 'g', -1, 32), true
	case progs.EvVector:
		if off+12 > len(p.Globals) {
			return "", false
		}
		v0 := float32frombits(p.Globals[off : off+4])
		v1 := float32frombits(p.Globals[off+4 : off+8])
		v2 := float32frombits(p.Globals[off+8 : off+12])
		return fmt.Sprintf("%s %s %s",
			strconv.FormatFloat(float64(v0), 'g', -1, 32),
			strconv.FormatFloat(float64(v1), 'g', -1, 32),
			strconv.FormatFloat(float64(v2), 'g', -1, 32)), true
	case progs.EvString:
		n := int32frombits(p.Globals[off : off+4])
		return p.String(n), true
	case progs.EvEntity, progs.EvField, progs.EvFunction, progs.EvPointer:
		n := int32frombits(p.Globals[off : off+4])
		return strconv.FormatInt(int64(n), 10), true
	}
	return "", false
}

// float32frombits / int32frombits are little-endian decoders for the
// raw 4-byte global pool slots. The progs.Globals byte slice is the
// canonical on-disk layout (always little-endian, regardless of host
// arch -- the upstream parser does the same byte-explicit decode).
func float32frombits(b []byte) float32 {
	u := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return math.Float32frombits(u)
}

func int32frombits(b []byte) int32 {
	return int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
}
