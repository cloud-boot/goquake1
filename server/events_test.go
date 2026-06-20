// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
)

// --- StartParticle ----------------------------------------------------------

// Happy path: NewServer + StartParticle -> Datagram holds the
// 12-byte svc_particle wire shape and decodes back to the inputs.
func TestServer_StartParticle_HappyPath(t *testing.T) {
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	if err := s.StartParticle([3]float32{8, 16, 24}, [3]float32{0.5, -1, 2}, 73, 5); err != nil {
		t.Fatalf("StartParticle: %v", err)
	}
	if s.Datagram.Len() != 12 {
		t.Fatalf("wire size: got %d want 12", s.Datagram.Len())
	}
	r := msg.NewReader(s.Datagram.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcParticle {
		t.Errorf("cmd: got %d want SvcParticle(%d)", cmd, protocol.SvcParticle)
	}
	for axis, want := range [3]float32{8, 16, 24} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("org[%d]: got %v want %v", axis, got, want)
		}
	}
}

// Datagram near full -> propagated ErrDatagramFull.
func TestServer_StartParticle_DatagramFull(t *testing.T) {
	s := NewServer()
	filler := make([]byte, MaxDatagram-particleReserve+1)
	if err := s.Datagram.Write(filler); err != nil {
		t.Fatal(err)
	}
	err := s.StartParticle([3]float32{}, [3]float32{}, 0, 0)
	if !errors.Is(err, ErrDatagramFull) {
		t.Errorf("got %v want ErrDatagramFull", err)
	}
}

// Nil receiver -> ErrNilServer.
func TestServer_StartParticle_NilReceiver(t *testing.T) {
	var s *Server
	err := s.StartParticle([3]float32{}, [3]float32{}, 0, 0)
	if !errors.Is(err, ErrNilServer) {
		t.Errorf("got %v want ErrNilServer", err)
	}
}

// Server with no Datagram -> ErrNilDatagram.
func TestServer_StartParticle_NilDatagram(t *testing.T) {
	s := &Server{}
	err := s.StartParticle([3]float32{}, [3]float32{}, 0, 0)
	if !errors.Is(err, ErrNilDatagram) {
		t.Errorf("got %v want ErrNilDatagram", err)
	}
}

// --- StartSound -------------------------------------------------------------

// withSound returns a NewServer whose Protocol is set and whose
// SoundPrecache contains name at slot 1 (the first valid sound slot;
// slot 0 is reserved).
func withSound(proto int, name string) *Server {
	s := NewServer()
	s.Protocol = proto
	s.SoundPrecache[1] = name
	return s
}

// Happy path: NewServer with precached sound -> Datagram holds the
// svc_sound wire shape, decodes back to (entity-centered) origin.
func TestServer_StartSound_HappyPath(t *testing.T) {
	s := withSound(protocol.VersionNQ, "weapons/sshotgun.wav")
	// Entity at (10, 20, 30) with mins/maxs that average to (1, 2, 3)
	// -> centered origin (11, 22, 33).
	err := s.StartSound(
		2, 0,
		[3]float32{10, 20, 30},
		[3]float32{-1, -2, -3},
		[3]float32{3, 6, 9},
		"weapons/sshotgun.wav",
		255, 1.0,
	)
	if err != nil {
		t.Fatalf("StartSound: %v", err)
	}
	if s.Datagram.Len() == 0 {
		t.Fatal("Datagram empty after StartSound")
	}
	r := msg.NewReader(s.Datagram.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcSound {
		t.Errorf("cmd: got %d want SvcSound(%d)", cmd, protocol.SvcSound)
	}
	// fieldMask: defaults for volume + atten -> 0.
	if mask := r.ReadU8(); mask != 0 {
		t.Errorf("fieldMask: got %d want 0", mask)
	}
	// entIdx<<3 | channel = 2<<3|0 = 16.
	if v := r.ReadShort(); v != 16 {
		t.Errorf("ent<<3|chan: got %d want 16", v)
	}
	// soundNum (NQ -> 1 byte).
	if sn := r.ReadU8(); sn != 1 {
		t.Errorf("soundNum: got %d want 1", sn)
	}
	// origin: entity-centered.
	for axis, want := range [3]float32{11, 22, 33} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("origin[%d]: got %v want %v", axis, got, want)
		}
	}
}

// Sound not in precache -> ErrNotPrecached.
func TestServer_StartSound_NotPrecached(t *testing.T) {
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	err := s.StartSound(
		1, 0,
		[3]float32{}, [3]float32{}, [3]float32{},
		"missing.wav", 255, 1.0,
	)
	if !errors.Is(err, ErrNotPrecached) {
		t.Errorf("got %v want ErrNotPrecached", err)
	}
}

// Nil *Server receiver -> ErrNilServer.
func TestServer_StartSound_NilReceiver(t *testing.T) {
	var s *Server
	err := s.StartSound(
		1, 0,
		[3]float32{}, [3]float32{}, [3]float32{},
		"x.wav", 255, 1.0,
	)
	if !errors.Is(err, ErrNilServer) {
		t.Errorf("got %v want ErrNilServer", err)
	}
}

// Server with no Datagram -> ErrNilDatagram.
func TestServer_StartSound_NilDatagram(t *testing.T) {
	s := &Server{}
	err := s.StartSound(
		1, 0,
		[3]float32{}, [3]float32{}, [3]float32{},
		"x.wav", 255, 1.0,
	)
	if !errors.Is(err, ErrNilDatagram) {
		t.Errorf("got %v want ErrNilDatagram", err)
	}
}

// Protocol routing: FITZ with channel>=8 routes through writeSoundNum
// with SndFitzLargeSound (2-byte sound num) instead of NQ's 1-byte
// path. The wire shape thus differs (1 byte longer on the soundNum
// field). We compare lengths to verify routing without re-validating
// every byte (EncodeSound has its own tests).
func TestServer_StartSound_FITZRoutesThroughWriteSoundNum(t *testing.T) {
	nq := withSound(protocol.VersionNQ, "x.wav")
	fitz := withSound(protocol.VersionFitz, "x.wav")

	nqErr := nq.StartSound(2, 0,
		[3]float32{}, [3]float32{}, [3]float32{},
		"x.wav", 255, 1.0)
	if nqErr != nil {
		t.Fatalf("NQ StartSound: %v", nqErr)
	}
	// channel=8 forces SndFitzLargeSound on FITZ (2-byte sound num
	// + the large-entity short for the ent/channel slot).
	fitzErr := fitz.StartSound(2, 8,
		[3]float32{}, [3]float32{}, [3]float32{},
		"x.wav", 255, 1.0)
	if fitzErr != nil {
		t.Fatalf("FITZ StartSound: %v", fitzErr)
	}
	if fitz.Datagram.Len() <= nq.Datagram.Len() {
		t.Errorf("FITZ large-sound wire should be longer than NQ: fitz=%d nq=%d",
			fitz.Datagram.Len(), nq.Datagram.Len())
	}
}

// --- BroadcastNop -----------------------------------------------------------

// 3 client slots, 2 Active+Spawned -> only those 2 get the svc_nop
// byte. The third (Active but not Spawned) is skipped.
func TestServer_BroadcastNop_OnlyActiveSpawned(t *testing.T) {
	s := NewServer()
	static := &Static{
		Clients: []*Client{
			activeClient(64, true, true),  // 0: receives
			activeClient(64, true, false), // 1: active but not spawned -> skipped
			activeClient(64, true, true),  // 2: receives
		},
	}
	if err := s.BroadcastNop(static); err != nil {
		t.Fatalf("BroadcastNop: %v", err)
	}
	for _, idx := range []int{0, 2} {
		got := static.Clients[idx].Message.Bytes()
		if len(got) != 1 || got[0] != byte(protocol.SvcNop) {
			t.Errorf("client %d: got %v want [%d]", idx, got, protocol.SvcNop)
		}
	}
	if static.Clients[1].Message.Len() != 0 {
		t.Errorf("unspawned client got message: len=%d", static.Clients[1].Message.Len())
	}
}

// Empty Static -> nil error, no panic.
func TestServer_BroadcastNop_NilStatic(t *testing.T) {
	s := NewServer()
	if err := s.BroadcastNop(nil); err != nil {
		t.Errorf("nil static should be silent, got %v", err)
	}
}

// Skips nil + inactive clients (covers the iteration guard branches).
func TestServer_BroadcastNop_SkipsNilAndInactive(t *testing.T) {
	s := NewServer()
	receiver := activeClient(64, true, true)
	static := &Static{
		Clients: []*Client{
			nil,                            // nil slot
			activeClient(64, false, false), // inactive
			receiver,
		},
	}
	if err := s.BroadcastNop(static); err != nil {
		t.Fatalf("BroadcastNop: %v", err)
	}
	if receiver.Message.Len() != 1 {
		t.Errorf("receiver: got len=%d want 1", receiver.Message.Len())
	}
}

// First client overflows on the svc_nop byte; the second still gets
// the message. Covers (a) the WriteByte error branch and (b) the
// firstErr-was-nil arm of the firstErr capture.
func TestServer_BroadcastNop_OverflowContinues(t *testing.T) {
	s := NewServer()
	overflower := activeClient(0, true, true) // can't fit svc_nop
	receiver := activeClient(64, true, true)
	static := &Static{Clients: []*Client{overflower, receiver}}

	err := s.BroadcastNop(static)
	if err == nil {
		t.Error("expected overflow error, got nil")
	}
	if receiver.Message.Len() != 1 {
		t.Error("second client should still have received svc_nop")
	}
}

// Two clients both overflow -> only the FIRST error is returned
// (covers the firstErr-already-set arm).
func TestServer_BroadcastNop_OnlyFirstErrorRetained(t *testing.T) {
	s := NewServer()
	o1 := activeClient(0, true, true)
	o2 := activeClient(0, true, true)
	static := &Static{Clients: []*Client{o1, o2}}

	if err := s.BroadcastNop(static); err == nil {
		t.Error("expected error, got nil")
	}
}

// --- ClearDatagram ----------------------------------------------------------

func TestServer_ClearDatagram_ResetsCursor(t *testing.T) {
	s := NewServer()
	if err := s.Datagram.Write([]byte{1, 2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	if s.Datagram.Len() != 5 {
		t.Fatalf("pre-clear len: got %d want 5", s.Datagram.Len())
	}
	s.ClearDatagram()
	if s.Datagram.Len() != 0 {
		t.Errorf("post-clear len: got %d want 0", s.Datagram.Len())
	}
}

// Nil receiver -> no-op, no panic.
func TestServer_ClearDatagram_NilReceiver(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ClearDatagram on nil receiver panicked: %v", r)
		}
	}()
	var s *Server
	s.ClearDatagram()
}

// Server with no Datagram -> no-op, no panic.
func TestServer_ClearDatagram_NilDatagram(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ClearDatagram on nil Datagram panicked: %v", r)
		}
	}()
	(&Server{}).ClearDatagram()
}

// Drift guard: re-Clear after writes works repeatedly.
func TestServer_ClearDatagram_Idempotent(t *testing.T) {
	s := NewServer()
	for i := 0; i < 3; i++ {
		if err := s.Datagram.Write([]byte{0xff}); err != nil {
			t.Fatal(err)
		}
		s.ClearDatagram()
		if s.Datagram.Len() != 0 {
			t.Errorf("iter %d: len=%d want 0", i, s.Datagram.Len())
		}
	}
}
