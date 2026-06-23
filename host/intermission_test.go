// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sizebuf"
)

// EmitIntermission with a nil host is a tolerated no-op.
func TestEmitIntermission_NilHost(t *testing.T) {
	var h *Host
	if err := h.EmitIntermission(); err != nil {
		t.Errorf("nil host: err=%v want nil", err)
	}
}

// EmitIntermission with a host whose Server is nil is a tolerated
// no-op.
func TestEmitIntermission_NilServer(t *testing.T) {
	h := &Host{}
	if err := h.EmitIntermission(); err != nil {
		t.Errorf("nil server: err=%v want nil", err)
	}
}

// EmitIntermission with a server whose ReliableDatagram is nil is a
// tolerated no-op.
func TestEmitIntermission_NilReliableDatagram(t *testing.T) {
	h := &Host{Server: &server.Server{}}
	if err := h.EmitIntermission(); err != nil {
		t.Errorf("nil reliable datagram: err=%v want nil", err)
	}
}

// Happy path: EmitIntermission writes 4 svc_updatestat tuples plus a
// trailing svc_intermission byte into the server's ReliableDatagram.
func TestEmitIntermission_HappyPath(t *testing.T) {
	h := &Host{Server: server.NewServer()}
	h.LastIntermissionStats = IntermissionStats{
		TotalSecrets:   7,
		FoundSecrets:   3,
		TotalMonsters:  42,
		KilledMonsters: 25,
	}
	if err := h.EmitIntermission(); err != nil {
		t.Fatalf("EmitIntermission: %v", err)
	}
	// 4 svc_updatestat tuples (cmd + statByte + int32) = 4 * 6 = 24
	// + 1 svc_intermission byte = 25 bytes total.
	if got, want := h.Server.ReliableDatagram.Len(), 25; got != want {
		t.Fatalf("wire size: got %d want %d", got, want)
	}
	// Decode + check tags.
	r := msg.NewReader(h.Server.ReliableDatagram.Bytes())
	wantStats := []struct {
		stat int
		val  int32
	}{
		{protocol.StatSecrets, 3},
		{protocol.StatTotalSecrets, 7},
		{protocol.StatMonsters, 25},
		{protocol.StatTotalMonsters, 42},
	}
	for i, w := range wantStats {
		if cmd := r.ReadU8(); cmd != protocol.SvcUpdateStat {
			t.Errorf("[%d] cmd=%d want SvcUpdateStat", i, cmd)
		}
		if stat := r.ReadU8(); stat != w.stat {
			t.Errorf("[%d] stat=%d want %d", i, stat, w.stat)
		}
		if val := r.ReadLong(); val != w.val {
			t.Errorf("[%d] val=%d want %d", i, val, w.val)
		}
	}
	if cmd := r.ReadU8(); cmd != protocol.SvcIntermission {
		t.Errorf("trailing cmd=%d want SvcIntermission", cmd)
	}
}

// EmitIntermission propagates a write failure: a zero-cap
// ReliableDatagram trips ErrSizeBufOverflow on the very first
// EncodeUpdateStat byte.
func TestEmitIntermission_OverflowPropagated(t *testing.T) {
	h := &Host{Server: &server.Server{
		ReliableDatagram: sizebuf.New(make([]byte, 0)),
	}}
	if err := h.EmitIntermission(); !errors.Is(err, sizebuf.ErrSizeBufOverflow) {
		t.Errorf("got %v want ErrSizeBufOverflow", err)
	}
}

// EmitIntermission propagates an overflow at each subsequent encode
// step. Each EncodeUpdateStat tuple is 6 bytes (svc + stat + int32);
// we run the buffer through every boundary so every error-return
// inside EmitIntermission's fan-out fires at least once.
func TestEmitIntermission_OverflowAtEachStep(t *testing.T) {
	// Capacity ladder: 6, 12, 18 -> each step lets the previous
	// EncodeUpdateStat succeed and trips overflow on the next tuple.
	// 24 -> stats fit but the trailing svc_intermission byte overflows.
	for _, cap := range []int{6, 12, 18, 24} {
		h := &Host{Server: &server.Server{
			ReliableDatagram: sizebuf.New(make([]byte, cap)),
		}}
		if err := h.EmitIntermission(); !errors.Is(err, sizebuf.ErrSizeBufOverflow) {
			t.Errorf("cap=%d: got %v want ErrSizeBufOverflow", cap, err)
		}
	}
}

// --- HarvestIntermissionStats ----------------------------------------

// HarvestIntermissionStats nil host: no panic.
func TestHarvestIntermissionStats_NilHost(t *testing.T) {
	var h *Host
	h.HarvestIntermissionStats()
}

// HarvestIntermissionStats: no progs bound = silent no-op (existing
// stats values are preserved).
func TestHarvestIntermissionStats_NoProgs(t *testing.T) {
	h := &Host{}
	h.LastIntermissionStats.TotalSecrets = 42
	h.HarvestIntermissionStats()
	if h.LastIntermissionStats.TotalSecrets != 42 {
		t.Errorf("stats clobbered when no progs bound")
	}
}

// HarvestIntermissionStats: globals present with values. The progs
// declares all four named globals; we pre-load the slots with float
// values and the harvest reflects them.
func TestHarvestIntermissionStats_FromGlobals(t *testing.T) {
	strs := []byte{0}
	totalSecretsName := addStr(&strs, "total_secrets")
	foundSecretsName := addStr(&strs, "found_secrets")
	totalMonstersName := addStr(&strs, "total_monsters")
	killedMonstersName := addStr(&strs, "killed_monsters")

	const numGlobals = 96
	const (
		offTotalSecrets   = 40
		offFoundSecrets   = 44
		offTotalMonsters  = 48
		offKilledMonsters = 52
	)
	p := &progs.Progs{
		Header:  progs.Header{EntityFields: 4},
		Strings: strs,
		GlobalDefs: []progs.Def{
			{Type: uint16(progs.EvFloat), Ofs: offTotalSecrets, SName: totalSecretsName},
			{Type: uint16(progs.EvFloat), Ofs: offFoundSecrets, SName: foundSecretsName},
			{Type: uint16(progs.EvFloat), Ofs: offTotalMonsters, SName: totalMonstersName},
			{Type: uint16(progs.EvFloat), Ofs: offKilledMonsters, SName: killedMonstersName},
		},
		Globals: make([]byte, numGlobals*4),
	}
	vm := progs.NewVM(p)
	if err := vm.SetGlobalFloat(offTotalSecrets, 10); err != nil {
		t.Fatalf("SetGlobalFloat(total_secrets): %v", err)
	}
	if err := vm.SetGlobalFloat(offFoundSecrets, 4); err != nil {
		t.Fatalf("SetGlobalFloat(found_secrets): %v", err)
	}
	if err := vm.SetGlobalFloat(offTotalMonsters, 30); err != nil {
		t.Fatalf("SetGlobalFloat(total_monsters): %v", err)
	}
	if err := vm.SetGlobalFloat(offKilledMonsters, 12); err != nil {
		t.Fatalf("SetGlobalFloat(killed_monsters): %v", err)
	}
	h := &Host{VM: vm}
	h.SetProgs(p)
	h.HarvestIntermissionStats()
	got := h.LastIntermissionStats
	want := IntermissionStats{
		TotalSecrets:   10,
		FoundSecrets:   4,
		TotalMonsters:  30,
		KilledMonsters: 12,
	}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// HarvestIntermissionStats: missing globals leave their slots at
// zero (don't clobber existing nonzero state with mistaken reads).
// We use a progs that declares none of the four named globals.
func TestHarvestIntermissionStats_MissingGlobalsLeaveStateAlone(t *testing.T) {
	p := &progs.Progs{
		Header:  progs.Header{EntityFields: 4},
		Strings: []byte{0},
		Globals: make([]byte, 96*4),
	}
	vm := progs.NewVM(p)
	h := &Host{VM: vm}
	h.SetProgs(p)
	h.LastIntermissionStats = IntermissionStats{
		TotalSecrets: 99, FoundSecrets: 7, TotalMonsters: 55, KilledMonsters: 3,
	}
	h.HarvestIntermissionStats()
	want := IntermissionStats{
		TotalSecrets: 99, FoundSecrets: 7, TotalMonsters: 55, KilledMonsters: 3,
	}
	if h.LastIntermissionStats != want {
		t.Errorf("missing-globals harvest clobbered state: got %+v want %+v", h.LastIntermissionStats, want)
	}
}
