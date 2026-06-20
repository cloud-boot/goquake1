// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"testing"

	"github.com/go-quake1/engine/sizebuf"
)

// Happy path: NumEdicts=5 -> cleaner called 4 times, once per edict
// in [1, 5), with MuzzleFlashMask each time. World at index 0 is
// skipped. tyrquake parity: SV_CleanupEnts starts the loop at e=1.
func TestCleanupEnts_IteratesSkippingWorld(t *testing.T) {
	s := NewServer()
	s.NumEdicts = 5

	var calls []int
	cleaner := func(idx int, mask int32) {
		if mask != MuzzleFlashMask {
			t.Errorf("call %d: mask got %d want %d", idx, mask, MuzzleFlashMask)
		}
		calls = append(calls, idx)
	}
	s.CleanupEnts(cleaner)

	if got, want := len(calls), 4; got != want {
		t.Fatalf("call count: got %d want %d", got, want)
	}
	for i, idx := range calls {
		if want := i + 1; idx != want {
			t.Errorf("call %d: idx got %d want %d", i, idx, want)
		}
	}
}

// Nil cleaner -> early return, no panic, no work done.
func TestCleanupEnts_NilCleanerNoOp(t *testing.T) {
	s := NewServer()
	s.NumEdicts = 5
	s.CleanupEnts(nil) // must not panic
}

// NumEdicts=0 -> loop body never runs.
func TestCleanupEnts_ZeroEdicts(t *testing.T) {
	s := NewServer()
	s.NumEdicts = 0
	called := false
	s.CleanupEnts(func(int, int32) { called = true })
	if called {
		t.Error("cleaner called with NumEdicts=0")
	}
}

// NumEdicts=1 -> only the world entry, which is skipped (loop runs
// for e=1; e<1 == false).
func TestCleanupEnts_OnlyWorldEdict(t *testing.T) {
	s := NewServer()
	s.NumEdicts = 1
	called := false
	s.CleanupEnts(func(int, int32) { called = true })
	if called {
		t.Error("cleaner called with NumEdicts=1 (should skip world)")
	}
}

// SignonSize returns Signon.Len() verbatim.
func TestSignonSize_ReflectsBufferLen(t *testing.T) {
	s := NewServer()
	if got := s.SignonSize(); got != 0 {
		t.Errorf("fresh server: SignonSize got %d want 0", got)
	}
	if err := s.Signon.Write([]byte{1, 2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	if got := s.SignonSize(); got != 5 {
		t.Errorf("after 5-byte write: SignonSize got %d want 5", got)
	}
}

// ClearReliableDatagram zeros Len.
func TestClearReliableDatagram_ResetsLen(t *testing.T) {
	s := NewServer()
	if err := s.ReliableDatagram.Write([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if got := s.ReliableDatagram.Len(); got != 4 {
		t.Fatalf("pre-clear Len: got %d want 4", got)
	}
	s.ClearReliableDatagram()
	if got := s.ReliableDatagram.Len(); got != 0 {
		t.Errorf("post-clear Len: got %d want 0", got)
	}
}

// CopyReliableDatagramTo appends server-side bytes onto the client
// message buffer and leaves the server-side buffer untouched (caller
// clears).
func TestCopyReliableDatagramTo_AppendsBytes(t *testing.T) {
	s := NewServer()
	c := NewClient()
	payload := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	if err := s.ReliableDatagram.Write(payload); err != nil {
		t.Fatal(err)
	}

	if err := s.CopyReliableDatagramTo(c); err != nil {
		t.Fatalf("copy: %v", err)
	}
	got := c.Message.Bytes()
	if len(got) != len(payload) {
		t.Fatalf("client len: got %d want %d", len(got), len(payload))
	}
	for i, b := range payload {
		if got[i] != b {
			t.Errorf("byte %d: got 0x%02x want 0x%02x", i, got[i], b)
		}
	}
	// Server side must NOT have been cleared (caller's job).
	if s.ReliableDatagram.Len() != len(payload) {
		t.Errorf("server reliable_datagram should be untouched: got Len=%d want %d",
			s.ReliableDatagram.Len(), len(payload))
	}
}

// Empty reliable_datagram -> no-op, nil error, client untouched.
func TestCopyReliableDatagramTo_EmptyIsNoOp(t *testing.T) {
	s := NewServer()
	c := NewClient()
	if err := s.CopyReliableDatagramTo(c); err != nil {
		t.Errorf("empty copy: got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("client message should stay empty, got Len=%d", c.Message.Len())
	}
}

// Client message buffer too small -> sizebuf overflow error
// propagates out of CopyReliableDatagramTo.
func TestCopyReliableDatagramTo_PropagatesOverflow(t *testing.T) {
	s := NewServer()
	if err := s.ReliableDatagram.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8}); err != nil {
		t.Fatal(err)
	}
	// Custom client with a 4-byte message buffer (won't fit 8).
	c := &Client{Message: sizebuf.New(make([]byte, 4))}
	if err := s.CopyReliableDatagramTo(c); err == nil {
		t.Fatal("expected overflow error from undersized client buffer")
	}
}
