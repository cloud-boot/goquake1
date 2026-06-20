// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import "testing"

// --- ServerState drift detector ------------------------------------------

func TestServerState_TyrquakeValues(t *testing.T) {
	if StateLoading != 0 {
		t.Errorf("StateLoading drift: got %d want 0", StateLoading)
	}
	if StateActive != 1 {
		t.Errorf("StateActive drift: got %d want 1", StateActive)
	}
}

// --- Per-struct limit constants ------------------------------------------

func TestLimits_TyrquakeValues(t *testing.T) {
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"MaxMsgLen", MaxMsgLen, 1 << 18},
		{"NumPingTimes", NumPingTimes, 16},
		{"NumSpawnParms", NumSpawnParms, 16},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

// --- NewServer -----------------------------------------------------------

func TestNewServer_PreallocatedBuffersAndPrecaches(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	// Precache slices sized to the upstream caps.
	if len(s.ModelPrecache) != MaxModels {
		t.Errorf("ModelPrecache: got %d want MaxModels (%d)", len(s.ModelPrecache), MaxModels)
	}
	if len(s.Models) != MaxModels {
		t.Errorf("Models: got %d want MaxModels (%d)", len(s.Models), MaxModels)
	}
	if len(s.SoundPrecache) != MaxSounds {
		t.Errorf("SoundPrecache: got %d want MaxSounds (%d)", len(s.SoundPrecache), MaxSounds)
	}
	if len(s.LightStyles) != MaxLightStyles {
		t.Errorf("LightStyles: got %d want MaxLightStyles (%d)", len(s.LightStyles), MaxLightStyles)
	}
	// Buffers sized correctly.
	if s.Datagram == nil {
		t.Fatal("Datagram nil")
	}
	if s.ReliableDatagram == nil {
		t.Fatal("ReliableDatagram nil")
	}
	if s.Signon == nil {
		t.Fatal("Signon nil")
	}
	// Defaults: not active, no edicts, time=0.
	if s.Active || s.NumEdicts != 0 || s.Time != 0 {
		t.Errorf("default state drift: Active=%v NumEdicts=%d Time=%v", s.Active, s.NumEdicts, s.Time)
	}
	if s.State != StateLoading {
		t.Errorf("default State: got %d want StateLoading", s.State)
	}
}

func TestNewServer_DatagramHasMaxDatagramCap(t *testing.T) {
	s := NewServer()
	// Filling the datagram to MaxDatagram-1 must succeed; the next
	// byte should overflow.
	if err := s.Datagram.Write(make([]byte, MaxDatagram)); err != nil {
		t.Fatalf("Datagram should hold MaxDatagram bytes: %v", err)
	}
	if err := s.Datagram.Write([]byte{0}); err == nil {
		t.Error("Datagram should overflow past MaxDatagram")
	}
}

// --- NewClient -----------------------------------------------------------

func TestNewClient_PreallocatedMessage(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.Message == nil {
		t.Fatal("Message buffer nil")
	}
	if c.Active || c.Spawned || c.DropAsap || c.SendSignon {
		t.Errorf("flags should default false: %+v", c)
	}
	if c.Edict != nil || c.NetConnection != nil {
		t.Errorf("pointer fields should default nil: %+v", c)
	}
}

func TestNewClient_MessageHasMaxMsgLenCap(t *testing.T) {
	c := NewClient()
	if err := c.Message.Write(make([]byte, MaxMsgLen)); err != nil {
		t.Fatalf("Message should hold MaxMsgLen bytes: %v", err)
	}
	if err := c.Message.Write([]byte{0}); err == nil {
		t.Error("Message should overflow past MaxMsgLen")
	}
}

// --- NewStatic -----------------------------------------------------------

func TestNewStatic_ClientPool(t *testing.T) {
	st := NewStatic(8)
	if st == nil {
		t.Fatal("NewStatic returned nil")
	}
	if st.MaxClients != 8 || st.MaxClientsLimit != 8 {
		t.Errorf("MaxClients / Limit: got %d / %d want 8 / 8", st.MaxClients, st.MaxClientsLimit)
	}
	if len(st.Clients) != 8 {
		t.Fatalf("Clients pool: got %d want 8", len(st.Clients))
	}
	for i, c := range st.Clients {
		if c == nil {
			t.Errorf("client %d is nil", i)
		} else if c.Message == nil {
			t.Errorf("client %d Message nil", i)
		}
	}
	if st.ServerFlags != 0 || st.ChangeLevelIssued {
		t.Errorf("default Static drift: ServerFlags=%d ChangeLevelIssued=%v", st.ServerFlags, st.ChangeLevelIssued)
	}
}

// Zero-maxclients edge: produces an empty pool, not a nil one.
func TestNewStatic_ZeroMaxClients(t *testing.T) {
	st := NewStatic(0)
	if st == nil {
		t.Fatal("NewStatic(0) returned nil")
	}
	if st.Clients == nil {
		t.Error("Clients should be non-nil empty slice, not nil")
	}
	if len(st.Clients) != 0 {
		t.Errorf("len(Clients): got %d want 0", len(st.Clients))
	}
}

// --- UserCmd drift detector ----------------------------------------------

// UserCmd is a value type; verify the field shape (3 floats viewangles
// + 3 floats move) by exercising it. The C upstream stores this
// inline in client_t.cmd; the layout is what the netcode parser
// writes into.
func TestUserCmd_FieldShape(t *testing.T) {
	cmd := UserCmd{
		ViewAngles:  [3]float32{45, 0, 0},
		ForwardMove: 100,
		SideMove:    -50,
		UpMove:      25,
		Buttons:     0x03, // attack + jump
		Impulse:     9,
	}
	if cmd.ViewAngles[0] != 45 {
		t.Errorf("ViewAngles[0]: got %v want 45", cmd.ViewAngles[0])
	}
	if cmd.ForwardMove != 100 || cmd.SideMove != -50 || cmd.UpMove != 25 {
		t.Errorf("move drift: %+v", cmd)
	}
	if cmd.Buttons != 0x03 || cmd.Impulse != 9 {
		t.Errorf("trigger drift: %+v", cmd)
	}
}
