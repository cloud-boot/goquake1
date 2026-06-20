// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"testing"

	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// activeClient returns a Client with a Message buffer sized to cap
// and the Active / Spawned flags set per the args.
func activeClient(cap int, active, spawned bool) *Client {
	c := &Client{
		Active:  active,
		Spawned: spawned,
		Message: sizebuf.New(make([]byte, cap)),
	}
	return c
}

// --- ClientPrint --------------------------------------------------------------

func TestClientPrint_HappyPath(t *testing.T) {
	c := activeClient(64, true, true)
	if err := ClientPrint(c, "hi"); err != nil {
		t.Fatalf("ClientPrint: %v", err)
	}
	got := c.Message.Bytes()
	want := []byte{byte(protocol.SvcPrint), 'h', 'i', 0}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d (%v)", len(got), len(want), got)
	}
	for i, b := range want {
		if got[i] != b {
			t.Errorf("byte %d: got %#x want %#x", i, got[i], b)
		}
	}
}

func TestClientPrint_NilClient(t *testing.T) {
	if err := ClientPrint(nil, "hi"); err != nil {
		t.Errorf("nil client should silently drop, got %v", err)
	}
}

func TestClientPrint_InactiveClient(t *testing.T) {
	c := activeClient(64, false, false)
	if err := ClientPrint(c, "hi"); err != nil {
		t.Errorf("inactive client should silently drop, got %v", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("inactive client buffer was written: len=%d", c.Message.Len())
	}
}

// Buffer too small to even hold the svc_print byte -> overflow.
func TestClientPrint_OverflowOnCmdByte(t *testing.T) {
	c := activeClient(0, true, true)
	if err := ClientPrint(c, "hi"); err == nil {
		t.Error("expected overflow error on cmd byte, got nil")
	}
}

// Buffer fits the cmd byte but not the string -> overflow on the
// WriteString call (covers the second error branch).
func TestClientPrint_OverflowOnString(t *testing.T) {
	c := activeClient(1, true, true)
	if err := ClientPrint(c, "hi"); err == nil {
		t.Error("expected overflow error on string, got nil")
	}
}

// --- BroadcastPrint -----------------------------------------------------------

// 3 slots, only 2 are active+spawned -> the third is left untouched.
func TestBroadcastPrint_OnlyActiveSpawned(t *testing.T) {
	s := &Static{
		Clients: []*Client{
			activeClient(64, true, true),  // 0: receives
			activeClient(64, true, false), // 1: active but not spawned -> skipped
			activeClient(64, true, true),  // 2: receives
		},
	}
	if err := BroadcastPrint(s, "yo"); err != nil {
		t.Fatalf("BroadcastPrint: %v", err)
	}
	want := []byte{byte(protocol.SvcPrint), 'y', 'o', 0}
	for _, idx := range []int{0, 2} {
		got := s.Clients[idx].Message.Bytes()
		if len(got) != len(want) {
			t.Errorf("client %d: len=%d want %d", idx, len(got), len(want))
			continue
		}
		for j, b := range want {
			if got[j] != b {
				t.Errorf("client %d byte %d: got %#x want %#x", idx, j, got[j], b)
			}
		}
	}
	if s.Clients[1].Message.Len() != 0 {
		t.Errorf("unspawned client got message: len=%d", s.Clients[1].Message.Len())
	}
}

// nil Static is a silent no-op (defensive guard).
func TestBroadcastPrint_NilStatic(t *testing.T) {
	if err := BroadcastPrint(nil, "x"); err != nil {
		t.Errorf("nil static should be silent, got %v", err)
	}
}

// Empty Clients slice -> nil error, no work.
func TestBroadcastPrint_EmptyClients(t *testing.T) {
	s := &Static{Clients: nil}
	if err := BroadcastPrint(s, "x"); err != nil {
		t.Errorf("empty clients should be silent, got %v", err)
	}
}

// Inactive / nil slots are skipped (covers the c==nil and !Active
// branches of the iteration guard).
func TestBroadcastPrint_SkipsNilAndInactive(t *testing.T) {
	receiver := activeClient(64, true, true)
	s := &Static{
		Clients: []*Client{
			nil,                            // nil slot
			activeClient(64, false, false), // inactive
			receiver,
		},
	}
	if err := BroadcastPrint(s, "ok"); err != nil {
		t.Fatalf("BroadcastPrint: %v", err)
	}
	if receiver.Message.Len() == 0 {
		t.Error("receiver got nothing")
	}
}

// Client 0's buffer overflows on the cmd byte; client 1 still gets
// the message (covers the cmd-byte error branch + the
// continue-iteration semantics).
func TestBroadcastPrint_OverflowOnCmdByteContinues(t *testing.T) {
	overflower := activeClient(0, true, true) // can't fit even svc_print
	receiver := activeClient(64, true, true)
	s := &Static{Clients: []*Client{overflower, receiver}}

	err := BroadcastPrint(s, "msg")
	if err == nil {
		t.Error("expected first-overflow error, got nil")
	}
	if receiver.Message.Len() == 0 {
		t.Error("second client should still have received the message")
	}
	// Sanity: receiver bytes are the full wire shape.
	got := receiver.Message.Bytes()
	want := []byte{byte(protocol.SvcPrint), 'm', 's', 'g', 0}
	if len(got) != len(want) {
		t.Fatalf("receiver len: got %d want %d", len(got), len(want))
	}
}

// Client 0's buffer fits the cmd byte but overflows on the string;
// the string-write error branch is also surfaced, and the next
// client still receives the full broadcast.
func TestBroadcastPrint_OverflowOnStringContinues(t *testing.T) {
	overflower := activeClient(1, true, true) // fits cmd, not string
	receiver := activeClient(64, true, true)
	s := &Static{Clients: []*Client{overflower, receiver}}

	err := BroadcastPrint(s, "msg")
	if err == nil {
		t.Error("expected string-overflow error, got nil")
	}
	if receiver.Message.Len() == 0 {
		t.Error("second client should still have received the message")
	}
}

// Two clients both overflow -> only the FIRST error is returned
// (covers the "firstErr already set" branch in the string-write
// error path).
func TestBroadcastPrint_OnlyFirstErrorRetained(t *testing.T) {
	o1 := activeClient(1, true, true) // fits cmd, fails on string
	o2 := activeClient(1, true, true) // same
	s := &Static{Clients: []*Client{o1, o2}}

	err := BroadcastPrint(s, "msg")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// Two clients both fail on the cmd byte -> covers the
// "firstErr already set" branch in the cmd-byte error path.
func TestBroadcastPrint_OnlyFirstCmdErrorRetained(t *testing.T) {
	o1 := activeClient(0, true, true)
	o2 := activeClient(0, true, true)
	s := &Static{Clients: []*Client{o1, o2}}

	err := BroadcastPrint(s, "msg")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// --- DropClient ---------------------------------------------------------------

func TestDropClient_GracefulWritesDisconnect(t *testing.T) {
	c := activeClient(64, true, true)
	c.SendSignon = true

	DropClient(c, false)

	if c.Active {
		t.Error("Active should be false after DropClient")
	}
	if c.Spawned {
		t.Error("Spawned should be false after DropClient")
	}
	if !c.DropAsap {
		t.Error("DropAsap should be true after DropClient")
	}
	if c.SendSignon {
		t.Error("SendSignon should be false after DropClient")
	}
	got := c.Message.Bytes()
	if len(got) != 1 || got[0] != byte(protocol.SvcDisconnect) {
		t.Errorf("graceful drop wire: got %v want [%d]", got, protocol.SvcDisconnect)
	}
}

func TestDropClient_CrashSkipsDisconnect(t *testing.T) {
	c := activeClient(64, true, true)

	DropClient(c, true)

	if c.Active {
		t.Error("Active should be false after crash drop")
	}
	if !c.DropAsap {
		t.Error("DropAsap should be true after crash drop")
	}
	if c.Message.Len() != 0 {
		t.Errorf("crash drop should not touch Message: len=%d", c.Message.Len())
	}
}

func TestDropClient_NilNoOp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("DropClient(nil) panicked: %v", r)
		}
	}()
	DropClient(nil, false)
	DropClient(nil, true)
}

func TestDropClient_AlreadyInactiveNoOp(t *testing.T) {
	c := activeClient(64, false, false)
	c.DropAsap = false
	DropClient(c, false)
	if c.DropAsap {
		t.Error("already-inactive client should not be re-flagged DropAsap")
	}
	if c.Message.Len() != 0 {
		t.Error("already-inactive client should not get svc_disconnect")
	}
}

// Defensive: graceful drop on a buffer too small to hold
// svc_disconnect still flips the flags + swallows the write error.
// (Covers the "_ = msg.WriteByte(...)" intentional error-swallow.)
func TestDropClient_GracefulWithFullBufferStillFlipsFlags(t *testing.T) {
	c := activeClient(0, true, true)
	DropClient(c, false)
	if c.Active {
		t.Error("Active should be false even when svc_disconnect failed to write")
	}
	if !c.DropAsap {
		t.Error("DropAsap should be true even when svc_disconnect failed to write")
	}
}
