// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/sizebuf"
)

// --- WriteClientData --------------------------------------------------------

func TestWriteClientData_NilClient(t *testing.T) {
	s := NewServer()
	if err := s.WriteClientData(nil, nil); err != nil {
		t.Fatalf("nil client: got %v want nil", err)
	}
}

func TestWriteClientData_InactiveClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = false
	c.Spawned = true
	if err := s.WriteClientData(c, playerProgs()); err != nil {
		t.Fatalf("inactive: got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("inactive: message len %d want 0", c.Message.Len())
	}
}

func TestWriteClientData_UnspawnedClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Spawned = false
	if err := s.WriteClientData(c, playerProgs()); err != nil {
		t.Fatalf("unspawned: got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("unspawned: message len %d want 0", c.Message.Len())
	}
}

func TestWriteClientData_NilEdict(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Spawned = true
	c.Edict = nil
	if err := s.WriteClientData(c, playerProgs()); err != nil {
		t.Fatalf("nil edict: got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("nil edict: message len %d want 0", c.Message.Len())
	}
}

func TestWriteClientData_NilProgs(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Spawned = true
	p := playerProgs()
	c.Edict = allocPlayerEdict(t, p)
	if err := s.WriteClientData(c, nil); err != nil {
		t.Fatalf("nil progs: got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("nil progs: message len %d want 0", c.Message.Len())
	}
}

// Happy path: a populated edict yields a non-empty svc_clientdata
// in client.Message.
func TestWriteClientData_HappyPath(t *testing.T) {
	s := NewServer()
	p := playerProgs()
	c := NewClient()
	c.Active = true
	c.Spawned = true
	c.Edict = allocPlayerEdict(t, p)

	v, _ := progs.NewEntVars(p, c.Edict)
	mustWriteVec3(t, v, "velocity", [3]float32{160, -80, 0})
	mustWriteFloat(t, v, "health", 100)

	if err := s.WriteClientData(c, p); err != nil {
		t.Fatalf("WriteClientData: %v", err)
	}
	if c.Message.Len() == 0 {
		t.Fatalf("Message: expected non-zero len")
	}
}

// Buffer overflow surfaces from the EncodeClientData error path.
func TestWriteClientData_PropagatesEncodeOverflow(t *testing.T) {
	s := NewServer()
	p := playerProgs()
	c := NewClient()
	c.Active = true
	c.Spawned = true
	c.Edict = allocPlayerEdict(t, p)
	c.Message = sizebuf.New(nil) // zero cap forces overflow

	err := s.WriteClientData(c, p)
	if !errors.Is(err, sizebuf.ErrSizeBufOverflow) {
		t.Fatalf("expected ErrSizeBufOverflow, got %v", err)
	}
}

// --- FlushClientMessage -----------------------------------------------------

func TestFlushClientMessage_NilClient(t *testing.T) {
	s := NewServer()
	if err := s.FlushClientMessage(nil); err != nil {
		t.Fatalf("nil client: got %v want nil", err)
	}
}

func TestFlushClientMessage_Inactive(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = false
	if err := s.FlushClientMessage(c); err != nil {
		t.Fatalf("inactive: got %v want nil", err)
	}
}

func TestFlushClientMessage_NilMessage(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Message = nil
	if err := s.FlushClientMessage(c); err != nil {
		t.Fatalf("nil msg: got %v want nil", err)
	}
}

func TestFlushClientMessage_EmptyMessage(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	cli, srv := NewLoopbackConn()
	c.NetConnection = srv
	if err := s.FlushClientMessage(c); err != nil {
		t.Fatalf("empty: got %v want nil", err)
	}
	// Nothing on the wire.
	kind, _, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if kind != MessageNone {
		t.Errorf("kind: got %v want MessageNone", kind)
	}
}

func TestFlushClientMessage_NotNetConn(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.NetConnection = "not a NetConn"
	if err := c.Message.Write([]byte{1, 2, 3}); err != nil {
		t.Fatalf("seed msg: %v", err)
	}
	if err := s.FlushClientMessage(c); err != nil {
		t.Fatalf("non-NetConn: got %v want nil", err)
	}
	// Message NOT cleared (caller has no transport to drain into).
	if c.Message.Len() != 3 {
		t.Errorf("message len: got %d want 3 (unchanged)", c.Message.Len())
	}
}

// Happy path: bytes leave client.Message + arrive on the loopback
// peer's ReadMessage queue; the buffer is cleared after.
func TestFlushClientMessage_HappyPath(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	cli, srv := NewLoopbackConn()
	c.NetConnection = srv
	payload := []byte{0xaa, 0xbb, 0xcc}
	if err := c.Message.Write(payload); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.FlushClientMessage(c); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("message len after flush: got %d want 0", c.Message.Len())
	}
	kind, data, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if kind != MessageReliable {
		t.Errorf("kind: got %v want MessageReliable", kind)
	}
	if string(data) != string(payload) {
		t.Errorf("payload: got %v want %v", data, payload)
	}
}

// SendReliable error surfaces verbatim + the buffer is NOT cleared
// (caller decides whether to retry or DropClient).
func TestFlushClientMessage_PropagatesSendError(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	_, srv := NewLoopbackConn()
	c.NetConnection = srv
	if err := c.Message.Write([]byte{0x11}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := s.FlushClientMessage(c)
	if !errors.Is(err, ErrNetConnClosed) {
		t.Fatalf("expected ErrNetConnClosed, got %v", err)
	}
	if c.Message.Len() != 1 {
		t.Errorf("message len after failed flush: got %d want 1 (unchanged)", c.Message.Len())
	}
}
