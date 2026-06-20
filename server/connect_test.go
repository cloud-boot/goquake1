// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"testing"

	"github.com/go-quake1/engine/progs"
)

// stubEdictFactory returns a closure that yields a fresh *progs.Edict
// on each call + records how many edicts were handed out.
func stubEdictFactory() (func() *progs.Edict, *int) {
	calls := 0
	return func() *progs.Edict {
		calls++
		return &progs.Edict{}
	}, &calls
}

func TestConnectClient_BindsFirstFreeSlot(t *testing.T) {
	static := NewStatic(3)
	cli, _ := NewLoopbackConn()
	factory, calls := stubEdictFactory()

	idx, err := ConnectClient(static, cli, 42.0, factory)
	if err != nil {
		t.Fatalf("ConnectClient: %v", err)
	}
	if idx != 0 {
		t.Errorf("idx: got %d want 0", idx)
	}
	if *calls != 1 {
		t.Errorf("makeEdict called: got %d want 1", *calls)
	}

	slot := static.Clients[0]
	if !slot.Active {
		t.Error("slot.Active: want true")
	}
	if slot.Spawned {
		t.Error("slot.Spawned: want false")
	}
	if !slot.SendSignon {
		t.Error("slot.SendSignon: want true")
	}
	if slot.NetConnection != cli {
		t.Errorf("slot.NetConnection: got %v want loopback conn", slot.NetConnection)
	}
	if slot.LastMessage != 42.0 {
		t.Errorf("slot.LastMessage: got %v want 42.0", slot.LastMessage)
	}
	if slot.Edict == nil {
		t.Error("slot.Edict: want non-nil")
	}
	if slot.Name != "" {
		t.Errorf("slot.Name: got %q want empty", slot.Name)
	}
	if slot.Colors != 0 {
		t.Errorf("slot.Colors: got %d want 0", slot.Colors)
	}
	for i, v := range slot.SpawnParms {
		if v != 0 {
			t.Errorf("slot.SpawnParms[%d]: got %v want 0", i, v)
		}
	}
}

func TestConnectClient_SkipsActiveSlots(t *testing.T) {
	static := NewStatic(3)
	static.Clients[0].Active = true
	static.Clients[1].Active = true

	cli, _ := NewLoopbackConn()
	factory, _ := stubEdictFactory()

	idx, err := ConnectClient(static, cli, 0, factory)
	if err != nil {
		t.Fatalf("ConnectClient: %v", err)
	}
	if idx != 2 {
		t.Errorf("idx: got %d want 2", idx)
	}
	if !static.Clients[2].Active {
		t.Error("slot 2 should be active")
	}
}

func TestConnectClient_NoFreeSlot(t *testing.T) {
	static := NewStatic(2)
	for _, c := range static.Clients {
		c.Active = true
	}
	cli, _ := NewLoopbackConn()
	factory, calls := stubEdictFactory()

	idx, err := ConnectClient(static, cli, 0, factory)
	if err != ErrNoFreeClientSlot {
		t.Errorf("err: got %v want ErrNoFreeClientSlot", err)
	}
	if idx != -1 {
		t.Errorf("idx: got %d want -1", idx)
	}
	if *calls != 0 {
		t.Errorf("makeEdict should not have been called, got %d", *calls)
	}
}

// SpawnParms reset path: pre-populate the array so the reset is
// observable as something distinct from "the slot was already zero".
func TestConnectClient_ResetsSpawnParms(t *testing.T) {
	static := NewStatic(1)
	for i := range static.Clients[0].SpawnParms {
		static.Clients[0].SpawnParms[i] = float32(i + 1)
	}
	static.Clients[0].Name = "leftover"
	static.Clients[0].Colors = 0xff

	cli, _ := NewLoopbackConn()
	factory, _ := stubEdictFactory()

	if _, err := ConnectClient(static, cli, 0, factory); err != nil {
		t.Fatalf("ConnectClient: %v", err)
	}

	slot := static.Clients[0]
	for i, v := range slot.SpawnParms {
		if v != 0 {
			t.Errorf("SpawnParms[%d]: got %v want 0 (reset failed)", i, v)
		}
	}
	if slot.Name != "" {
		t.Errorf("Name: got %q want empty (reset failed)", slot.Name)
	}
	if slot.Colors != 0 {
		t.Errorf("Colors: got %d want 0 (reset failed)", slot.Colors)
	}
}
