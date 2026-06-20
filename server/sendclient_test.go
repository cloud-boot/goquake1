// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-quake1/engine/sizebuf"
)

// nil client -> silent no-op (matches the upstream's per-slot skip).
func TestPreparePerClientMessage_NilClient(t *testing.T) {
	s := NewServer()
	if err := s.PreparePerClientMessage(nil); err != nil {
		t.Fatalf("nil client: got err %v want nil", err)
	}
}

// Inactive client -> silent no-op. The per-tick frame builder must
// never touch slots that haven't completed the connect handshake.
func TestPreparePerClientMessage_InactiveClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = false
	c.Spawned = true
	if err := s.PreparePerClientMessage(c); err != nil {
		t.Fatalf("inactive client: got err %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Fatalf("inactive client: message len got %d want 0", c.Message.Len())
	}
}

// Active but not Spawned -> silent no-op. Matches the upstream's
// "if (client->spawned) { SV_SendClientDatagram } else { ... }"
// branch: the per-frame datagram copy lives in the spawned branch.
func TestPreparePerClientMessage_UnspawnedClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Spawned = false
	if err := s.PreparePerClientMessage(c); err != nil {
		t.Fatalf("unspawned client: got err %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Fatalf("unspawned client: message len got %d want 0", c.Message.Len())
	}
}

// Happy path: populated reliable + unreliable buffers get appended
// in that order to the client's Message.
func TestPreparePerClientMessage_AppendsReliableThenDatagram(t *testing.T) {
	s := NewServer()
	reliablePayload := []byte{0x01, 0x02, 0x03}
	datagramPayload := []byte{0xaa, 0xbb}
	if err := s.ReliableDatagram.Write(reliablePayload); err != nil {
		t.Fatalf("seed reliable: %v", err)
	}
	if err := s.Datagram.Write(datagramPayload); err != nil {
		t.Fatalf("seed datagram: %v", err)
	}

	c := NewClient()
	c.Active = true
	c.Spawned = true
	if err := s.PreparePerClientMessage(c); err != nil {
		t.Fatalf("prepare: got err %v want nil", err)
	}

	want := append(append([]byte{}, reliablePayload...), datagramPayload...)
	if !bytes.Equal(c.Message.Bytes(), want) {
		t.Fatalf("message bytes: got %v want %v", c.Message.Bytes(), want)
	}
}

// Overflow on the reliable-datagram write -- Message capacity is
// smaller than the reliable payload alone. The propagated sizebuf
// overflow must reach the caller (so SV_SendClientMessages can
// DropClient the slot).
func TestPreparePerClientMessage_OverflowOnReliable(t *testing.T) {
	s := NewServer()
	if err := s.ReliableDatagram.Write([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("seed reliable: %v", err)
	}

	c := &Client{
		Active:  true,
		Spawned: true,
		Message: sizebuf.New(make([]byte, 2)), // smaller than reliable
	}
	err := s.PreparePerClientMessage(c)
	if !errors.Is(err, sizebuf.ErrSizeBufOverflow) {
		t.Fatalf("expected overflow error, got %v", err)
	}
}

// Overflow on the datagram write -- the reliable write fits but the
// follow-on datagram write blows the cap. Distinct from the
// reliable-overflow case to exercise the second Write branch.
func TestPreparePerClientMessage_OverflowOnDatagram(t *testing.T) {
	s := NewServer()
	if err := s.ReliableDatagram.Write([]byte{1, 2}); err != nil {
		t.Fatalf("seed reliable: %v", err)
	}
	if err := s.Datagram.Write([]byte{9, 9, 9, 9}); err != nil {
		t.Fatalf("seed datagram: %v", err)
	}

	c := &Client{
		Active:  true,
		Spawned: true,
		Message: sizebuf.New(make([]byte, 3)), // fits reliable (2) but not +datagram (4)
	}
	err := s.PreparePerClientMessage(c)
	if !errors.Is(err, sizebuf.ErrSizeBufOverflow) {
		t.Fatalf("expected overflow error, got %v", err)
	}
}

// SendClientFrames: 3-slot Static, slot 0 unspawned, slot 1
// active+spawned, slot 2 inactive. PerClientErrs has length 3 with
// only slot 1 having received the appended bytes; all entries are
// nil (no overflow). The test asserts the parallel-index semantic
// (PerClientErrs[i] <-> static.Clients[i]).
func TestSendClientFrames_MixedSlots(t *testing.T) {
	s := NewServer()
	if err := s.ReliableDatagram.Write([]byte{0x42}); err != nil {
		t.Fatalf("seed reliable: %v", err)
	}
	if err := s.Datagram.Write([]byte{0x43}); err != nil {
		t.Fatalf("seed datagram: %v", err)
	}

	static := NewStatic(3)
	// slot 0: active but unspawned -> skipped
	static.Clients[0].Active = true
	static.Clients[0].Spawned = false
	// slot 1: active + spawned -> receives the copy
	static.Clients[1].Active = true
	static.Clients[1].Spawned = true
	// slot 2: inactive -> skipped
	static.Clients[2].Active = false
	static.Clients[2].Spawned = true

	res := s.SendClientFrames(static)
	if got, want := len(res.PerClientErrs), 3; got != want {
		t.Fatalf("PerClientErrs len: got %d want %d", got, want)
	}
	for i, err := range res.PerClientErrs {
		if err != nil {
			t.Errorf("slot %d: got err %v want nil", i, err)
		}
	}
	if static.Clients[0].Message.Len() != 0 {
		t.Errorf("slot 0 (unspawned) message: got %d bytes want 0",
			static.Clients[0].Message.Len())
	}
	want := []byte{0x42, 0x43}
	if !bytes.Equal(static.Clients[1].Message.Bytes(), want) {
		t.Errorf("slot 1 (active+spawned) message: got %v want %v",
			static.Clients[1].Message.Bytes(), want)
	}
	if static.Clients[2].Message.Len() != 0 {
		t.Errorf("slot 2 (inactive) message: got %d bytes want 0",
			static.Clients[2].Message.Len())
	}
}

// SendClientFrames with an empty Static.Clients slice -> empty
// PerClientErrs (no allocations beyond the zero-length slice header).
func TestSendClientFrames_EmptyStatic(t *testing.T) {
	s := NewServer()
	static := &Static{Clients: nil}
	res := s.SendClientFrames(static)
	if got, want := len(res.PerClientErrs), 0; got != want {
		t.Fatalf("PerClientErrs len: got %d want %d", got, want)
	}
}

// SendClientFrames propagates per-client overflow into the matching
// PerClientErrs slot while still attempting every other client. The
// test sets up a 2-slot Static where slot 0 has a tiny Message and
// will overflow; slot 1 is active+spawned with default capacity
// and must still get the copy.
func TestSendClientFrames_PropagatesPerSlotOverflow(t *testing.T) {
	s := NewServer()
	if err := s.ReliableDatagram.Write([]byte{0x10, 0x20, 0x30, 0x40}); err != nil {
		t.Fatalf("seed reliable: %v", err)
	}

	static := &Static{
		Clients: []*Client{
			{
				Active:  true,
				Spawned: true,
				Message: sizebuf.New(make([]byte, 2)), // will overflow
			},
			{
				Active:  true,
				Spawned: true,
				Message: sizebuf.New(make([]byte, MaxMsgLen)),
			},
		},
	}
	res := s.SendClientFrames(static)
	if !errors.Is(res.PerClientErrs[0], sizebuf.ErrSizeBufOverflow) {
		t.Errorf("slot 0: want overflow err, got %v", res.PerClientErrs[0])
	}
	if res.PerClientErrs[1] != nil {
		t.Errorf("slot 1: want nil err, got %v", res.PerClientErrs[1])
	}
	want := []byte{0x10, 0x20, 0x30, 0x40}
	if !bytes.Equal(static.Clients[1].Message.Bytes(), want) {
		t.Errorf("slot 1 message: got %v want %v",
			static.Clients[1].Message.Bytes(), want)
	}
}
