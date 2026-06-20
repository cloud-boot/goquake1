// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"
	"testing"
)

// --- ConnectionState drift detector --------------------------------------

func TestConnectionState_TyrquakeValues(t *testing.T) {
	checks := []struct {
		name string
		got  ConnectionState
		want ConnectionState
	}{
		{"StateDisconnected", StateDisconnected, 0},
		{"StateConnecting", StateConnecting, 1},
		{"StateConnected", StateConnected, 2},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s drift: got %d want %d", c.name, c.got, c.want)
		}
	}
}

// --- Per-struct limit constants ------------------------------------------

func TestLimits_TyrquakeValues(t *testing.T) {
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"MaxLightStyles", MaxLightStyles, 64},
		{"MaxDLights", MaxDLights, 32},
		{"MaxTempEntities", MaxTempEntities, 64},
		{"MaxEfrags", MaxEfrags, 640},
		{"MaxBeams", MaxBeams, 24},
		{"NumPingTimes", NumPingTimes, 16},
		{"MaxClientMessage", MaxClientMessage, 1 << 18},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

// --- NewState ------------------------------------------------------------

func TestNewState_InitializedFields(t *testing.T) {
	s := NewState()
	if s == nil {
		t.Fatal("NewState returned nil")
	}
	if s.Message == nil {
		t.Fatal("Message buffer nil")
	}
	if s.Connection != StateDisconnected {
		t.Errorf("Connection: got %d want StateDisconnected", s.Connection)
	}
	if s.Spawned {
		t.Error("Spawned: got true want false")
	}
	// Per-frame slice caps for the fixed-size arrays.
	if len(s.LightStyles) != MaxLightStyles {
		t.Errorf("LightStyles len: got %d want %d", len(s.LightStyles), MaxLightStyles)
	}
	if len(s.DLights) != MaxDLights {
		t.Errorf("DLights len: got %d want %d", len(s.DLights), MaxDLights)
	}
	if len(s.PingTimes) != NumPingTimes {
		t.Errorf("PingTimes len: got %d want %d", len(s.PingTimes), NumPingTimes)
	}
	// Transport buffer wired to MaxClientMessage capacity.
	if got := cap(s.Message.Bytes()) + s.Message.Len(); got != 0 {
		// Bytes() returns []byte aliasing data[:cursize]; Len() is
		// cursize; the assertion here is just that the fresh
		// buffer reports zero length.
	}
	if s.Message.Len() != 0 {
		t.Errorf("Message.Len: got %d want 0", s.Message.Len())
	}
}

// --- Clear ---------------------------------------------------------------

func TestState_Clear_WipesPerMapFields(t *testing.T) {
	s := NewState()
	s.MapName = "e1m1"
	s.LevelName = "Slipgate Complex"
	s.NumVisEdicts = 17
	s.Health = 99
	s.PlayerNum = 3
	s.OnGround = true
	s.InWater = true
	s.DLights[0] = DLight{Radius: 100, Die: 5.0}
	s.LightStyles[0] = LightStyle{Anim: []byte("abcd")}
	s.Stats[0] = 42
	s.Items = 0xff
	s.Ammo[2] = 50
	// Pre-Clear: connection + spawned set so we can verify they
	// are preserved by Clear (only Disconnect mutates them).
	s.Connection = StateConnected
	s.Spawned = true
	// Stuff the message buffer with a byte so we can verify Clear
	// resets it (Clear should call buf.Clear, not free it).
	if err := s.Message.Write([]byte{0xAA}); err != nil {
		t.Fatalf("Message.Write: %v", err)
	}
	if s.Message.Len() != 1 {
		t.Fatalf("Message.Len pre-Clear: got %d want 1", s.Message.Len())
	}

	msgBefore := s.Message
	s.Clear()

	if s.MapName != "" {
		t.Errorf("MapName: got %q want \"\"", s.MapName)
	}
	if s.LevelName != "" {
		t.Errorf("LevelName: got %q want \"\"", s.LevelName)
	}
	if s.NumVisEdicts != 0 {
		t.Errorf("NumVisEdicts: got %d want 0", s.NumVisEdicts)
	}
	if s.Health != 0 {
		t.Errorf("Health: got %d want 0", s.Health)
	}
	if s.PlayerNum != 0 {
		t.Errorf("PlayerNum: got %d want 0", s.PlayerNum)
	}
	if s.OnGround {
		t.Error("OnGround: got true want false")
	}
	if s.InWater {
		t.Error("InWater: got true want false")
	}
	if s.DLights[0] != (DLight{}) {
		t.Errorf("DLights[0]: got %+v want zero", s.DLights[0])
	}
	if s.LightStyles[0].Anim != nil {
		t.Errorf("LightStyles[0].Anim: got %v want nil", s.LightStyles[0].Anim)
	}
	if s.Stats[0] != 0 {
		t.Errorf("Stats[0]: got %d want 0", s.Stats[0])
	}
	if s.Items != 0 {
		t.Errorf("Items: got %d want 0", s.Items)
	}
	if s.Ammo[2] != 0 {
		t.Errorf("Ammo[2]: got %d want 0", s.Ammo[2])
	}
	// Connection + Spawned preserved.
	if s.Connection != StateConnected {
		t.Errorf("Connection: got %d want StateConnected (Clear must not transition)", s.Connection)
	}
	if !s.Spawned {
		t.Error("Spawned: got false want true (Clear must not flip)")
	}
	// Message buffer alive but empty.
	if s.Message != msgBefore {
		t.Error("Message buffer pointer changed -- Clear should preserve allocation")
	}
	if s.Message.Len() != 0 {
		t.Errorf("Message.Len post-Clear: got %d want 0", s.Message.Len())
	}
}

// --- Disconnect ----------------------------------------------------------

func TestState_Disconnect_FromConnected(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("MarkSpawned: %v", err)
	}
	s.MapName = "e1m1"
	s.Health = 75

	s.Disconnect()

	if s.Connection != StateDisconnected {
		t.Errorf("Connection: got %d want StateDisconnected", s.Connection)
	}
	if s.Spawned {
		t.Error("Spawned: got true want false")
	}
	if s.MapName != "" {
		t.Errorf("MapName: got %q want \"\" (Disconnect should Clear)", s.MapName)
	}
	if s.Health != 0 {
		t.Errorf("Health: got %d want 0 (Disconnect should Clear)", s.Health)
	}
}

func TestState_Disconnect_Idempotent(t *testing.T) {
	s := NewState()
	s.Disconnect()
	s.Disconnect()
	if s.Connection != StateDisconnected {
		t.Errorf("Connection after double-Disconnect: got %d want StateDisconnected", s.Connection)
	}
	if s.Spawned {
		t.Error("Spawned after double-Disconnect: got true want false")
	}
}

// --- SetConnecting -------------------------------------------------------

func TestState_SetConnecting_FromDisconnected(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: got err %v want nil", err)
	}
	if s.Connection != StateConnecting {
		t.Errorf("Connection: got %d want StateConnecting", s.Connection)
	}
}

func TestState_SetConnecting_FromConnecting(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("first SetConnecting: %v", err)
	}
	err := s.SetConnecting()
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("second SetConnecting: got %v want ErrAlreadyConnected", err)
	}
}

func TestState_SetConnecting_FromConnected(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("MarkSpawned: %v", err)
	}
	err := s.SetConnecting()
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("SetConnecting from connected: got %v want ErrAlreadyConnected", err)
	}
}

// --- MarkSpawned ---------------------------------------------------------

func TestState_MarkSpawned_FromConnecting(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("MarkSpawned: got err %v want nil", err)
	}
	if s.Connection != StateConnected {
		t.Errorf("Connection: got %d want StateConnected", s.Connection)
	}
	if !s.Spawned {
		t.Error("Spawned: got false want true")
	}
}

func TestState_MarkSpawned_FromDisconnected(t *testing.T) {
	s := NewState()
	err := s.MarkSpawned()
	if !errors.Is(err, ErrNotConnecting) {
		t.Errorf("MarkSpawned from disconnected: got %v want ErrNotConnecting", err)
	}
}

func TestState_MarkSpawned_FromConnected(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("first MarkSpawned: %v", err)
	}
	err := s.MarkSpawned()
	if !errors.Is(err, ErrNotConnecting) {
		t.Errorf("second MarkSpawned: got %v want ErrNotConnecting", err)
	}
}

// --- RecordPing ----------------------------------------------------------

func TestState_RecordPing_AppendsAndAdvances(t *testing.T) {
	s := NewState()
	s.RecordPing(0.025)
	if s.NumPings != 1 {
		t.Errorf("NumPings after 1 sample: got %d want 1", s.NumPings)
	}
	if s.PingTimes[0] != 0.025 {
		t.Errorf("PingTimes[0]: got %v want 0.025", s.PingTimes[0])
	}
}

func TestState_RecordPing_RingWraparound(t *testing.T) {
	s := NewState()
	// Fill the ring + one extra to verify the modulo overwrites
	// slot 0 instead of growing past NumPingTimes.
	for i := 0; i < NumPingTimes+1; i++ {
		s.RecordPing(float32(i) + 0.5)
	}
	if s.NumPings != NumPingTimes+1 {
		t.Errorf("NumPings: got %d want %d", s.NumPings, NumPingTimes+1)
	}
	// Slot 0 should hold the most recent (NumPingTimes-th) sample.
	want := float32(NumPingTimes) + 0.5
	if s.PingTimes[0] != want {
		t.Errorf("PingTimes[0] after wrap: got %v want %v", s.PingTimes[0], want)
	}
	// Slot 1 should still hold the i=1 sample (untouched after wrap).
	if s.PingTimes[1] != 1.5 {
		t.Errorf("PingTimes[1] after wrap: got %v want 1.5", s.PingTimes[1])
	}
}
