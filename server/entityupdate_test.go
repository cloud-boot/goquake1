// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// --- SendEntityUpdates ---------------------------------------------

func TestSendEntityUpdates_NilServer(t *testing.T) {
	var s *Server
	_, err := s.SendEntityUpdates(NewClient(), nil, 1)
	if !errors.Is(err, ErrSendEntityUpdatesNilServer) {
		t.Errorf("nil server: got %v want ErrSendEntityUpdatesNilServer", err)
	}
}

func TestSendEntityUpdates_NilClient(t *testing.T) {
	s := NewServer()
	stat, err := s.SendEntityUpdates(nil, nil, 1)
	if err != nil {
		t.Errorf("nil client: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("nil client: got Emitted=%d want 0", stat.Emitted)
	}
}

func TestSendEntityUpdates_InactiveClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = false
	c.Spawned = true
	stat, err := s.SendEntityUpdates(c, nil, 1)
	if err != nil {
		t.Errorf("inactive client: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("inactive client: got Emitted=%d want 0", stat.Emitted)
	}
}

func TestSendEntityUpdates_UnspawnedClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Spawned = false
	stat, err := s.SendEntityUpdates(c, nil, 1)
	if err != nil {
		t.Errorf("unspawned client: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("unspawned client: got Emitted=%d want 0", stat.Emitted)
	}
}

func TestSendEntityUpdates_NilMessageBuf(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Spawned = true
	c.Message = nil
	stat, err := s.SendEntityUpdates(c, nil, 1)
	if err != nil {
		t.Errorf("nil message: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("nil message: got Emitted=%d want 0", stat.Emitted)
	}
}

// Empty edict pool past slot 0 -> zero emissions.
func TestSendEntityUpdates_EmptyEdictPool(t *testing.T) {
	s := NewServer()
	s.Edicts = []*progs.Edict{}
	s.NumEdicts = 0
	c := NewClient()
	c.Active = true
	c.Spawned = true
	stat, err := s.SendEntityUpdates(c, nil, 1)
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("Emitted: got %d want 0", stat.Emitted)
	}
	if c.Message.Len() != 0 {
		t.Errorf("Message.Len: got %d want 0", c.Message.Len())
	}
}

// Mixed pool: world(skip-by-design) + player + monster + free + trigger
// -> 3 emitted (player, monster, trigger), 1 skipped (free).
func TestSendEntityUpdates_HappyPath_EmitsPerEntity(t *testing.T) {
	s, p := serverWithMixedEdicts(t)
	c := NewClient()
	c.Active = true
	c.Spawned = true

	stat, err := s.SendEntityUpdates(c, p, 1)
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}

	// Walk shape: slots 1..4 (slot 0 = world, never visited).
	//   slot 1 (player)        -> emit
	//   slot 2 (monster)       -> emit
	//   slot 3 (free)          -> skip
	//   slot 4 (no-model trig) -> emit (no-model guard disabled for bring-up)
	if stat.Emitted != 3 {
		t.Errorf("Emitted: got %d want 3", stat.Emitted)
	}
	if stat.Skipped != 1 {
		t.Errorf("Skipped: got %d want 1", stat.Skipped)
	}
	if len(stat.PerSlotEmitted) != 3 ||
		stat.PerSlotEmitted[0] != 1 ||
		stat.PerSlotEmitted[1] != 2 ||
		stat.PerSlotEmitted[2] != 4 {
		t.Errorf("PerSlotEmitted: got %v want [1 2 4]", stat.PerSlotEmitted)
	}

	// Walk the queued bytes: each message starts with a USignal'd bits
	// byte; the entNum follows (byte for entNum <= 255). Just verify
	// the first message's opcode + entNum to prove the encoder round-
	// trips through the helper.
	r := msg.NewReader(c.Message.Bytes())
	firstBits := r.ReadU8()
	if firstBits&protocol.USignal == 0 {
		t.Errorf("first message: bits byte %#x missing USignal", firstBits)
	}
	// For slot 1 (player) -> the helper sets bits=fullUpdateBits |
	// U_MODEL | U_COLORMAP, and fullUpdateBits | U_MODEL crosses the
	// 0xff00 boundary (U_MODEL = 1<<10), so U_MOREBITS must be set
	// and a high-bits byte must follow.
	if firstBits&protocol.UMoreBits == 0 {
		t.Errorf("first message: low bits %#x missing UMoreBits", firstBits)
	}
	_ = r.ReadU8() // high bits byte
	if ent := r.ReadU8(); ent != 1 {
		t.Errorf("first message entNum: got %d want 1", ent)
	}
}

// Nil edict slot (slot allocated but pointer is nil) classifies as
// Skipped -- matches the "structurally absent = skip" rule.
func TestSendEntityUpdates_NilEdictSlot_SkipsAsSkipped(t *testing.T) {
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	s.Edicts = []*progs.Edict{nil, nil, nil}
	s.NumEdicts = 3
	c := NewClient()
	c.Active = true
	c.Spawned = true

	stat, err := s.SendEntityUpdates(c, nil, 1)
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("Emitted: got %d want 0", stat.Emitted)
	}
	// Slot 0 is skipped by-design; slots 1 + 2 are nil = Skipped.
	if stat.Skipped != 2 {
		t.Errorf("Skipped: got %d want 2 (slots 1+2 nil; slot 0 is the by-design skip)", stat.Skipped)
	}
}

// EncodeUpdate overflow propagates verbatim. Force it by capping
// client.Message to zero bytes so even the first bits byte fails.
func TestSendEntityUpdates_PropagatesEncoderError(t *testing.T) {
	s, p := serverWithMixedEdicts(t)
	c := NewClient()
	c.Active = true
	c.Spawned = true
	c.Message = sizebuf.New(make([]byte, 0)) // zero capacity -> overflow on first byte

	stat, err := s.SendEntityUpdates(c, p, 1)
	if err == nil {
		t.Fatal("expected encoder error, got nil")
	}
	if stat.Emitted != 0 {
		t.Errorf("Emitted: got %d want 0 (failure on first emit)", stat.Emitted)
	}
}

// Live entity with non-zero Frame + Skin -> the helper sets U_FRAME
// and U_SKIN in the bitmask. Uses progsWithFullEntity (frame=5, skin=2)
// at a non-player slot so the optional bits get exercised.
func TestSendEntityUpdates_EmitsFrameAndSkin(t *testing.T) {
	p, e := progsWithFullEntity(t)
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	s.ModelPrecache = []string{"maps/start.bsp"}
	s.Edicts = []*progs.Edict{
		{Fields: make([]byte, 4)}, // slot 0 = world (skipped by design)
		e,                         // slot 1 = the full-entvars entity
	}
	s.NumEdicts = 2

	c := NewClient()
	c.Active = true
	c.Spawned = true

	// maxClients = 0 forces the non-player branch (slot 1 > maxClients),
	// so the helper picks up frame/skin off v.frame/v.skin.
	stat, err := s.SendEntityUpdates(c, p, 0)
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
	if stat.Emitted != 1 {
		t.Fatalf("Emitted: got %d want 1", stat.Emitted)
	}

	// Walk the encoded bits to assert both U_FRAME (low byte) and
	// U_SKIN (high byte -> requires U_MOREBITS) are present.
	r := msg.NewReader(c.Message.Bytes())
	low := r.ReadU8()
	if low&protocol.UFrame == 0 {
		t.Errorf("bits low %#x missing U_FRAME", low)
	}
	if low&protocol.UMoreBits == 0 {
		t.Errorf("bits low %#x missing U_MOREBITS", low)
	}
	high := r.ReadU8()
	if high&((protocol.USkin>>8)&0xff) == 0 {
		t.Errorf("bits high %#x missing U_SKIN", high)
	}
}

// Large entNum (> 255) triggers U_LONGENTITY -- the entity index is
// written as a short instead of a byte. Build a sparse edict pool
// where the only non-nil/non-free slot is at index 300 so we get a
// deterministic single emission to inspect.
func TestSendEntityUpdates_LongEntity(t *testing.T) {
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	s.ModelPrecache = []string{"maps/start.bsp"}
	s.Edicts = make([]*progs.Edict, 301)
	for i := range s.Edicts {
		s.Edicts[i] = &progs.Edict{Free: true}
	}
	s.Edicts[300] = &progs.Edict{Fields: make([]byte, 4)} // live, default-zero entity
	s.NumEdicts = 301

	c := NewClient()
	c.Active = true
	c.Spawned = true

	stat, err := s.SendEntityUpdates(c, nil, 0)
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
	if stat.Emitted != 1 {
		t.Fatalf("Emitted: got %d want 1", stat.Emitted)
	}

	// First byte: bits low (with USignal). Since U_LONGENTITY is in
	// the high byte, UMoreBits must also be set.
	r := msg.NewReader(c.Message.Bytes())
	low := r.ReadU8()
	if low&protocol.USignal == 0 {
		t.Errorf("bits low %#x missing USignal", low)
	}
	if low&protocol.UMoreBits == 0 {
		t.Errorf("bits low %#x missing UMoreBits (U_LONGENTITY lives in the high byte)", low)
	}
	high := r.ReadU8()
	if high&((protocol.ULongEntity>>8)&0xff) == 0 {
		t.Errorf("bits high %#x missing U_LONGENTITY", high)
	}
	if ent := r.ReadShort(); ent != 300 {
		t.Errorf("entNum: got %d want 300", ent)
	}
}
