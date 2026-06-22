// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// signonInfoFixture returns a valid ServerInfo with the minimum
// payload EncodeServerInfo accepts (non-empty LevelName, otherwise
// zero precaches).
func signonInfoFixture() ServerInfo {
	return ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    1,
		GameType:      GameTypeCoop,
		LevelName:     "the slipgate complex",
		ModelPrecache: []string{"", "maps/start.bsp"},
		SoundPrecache: []string{""},
	}
}

// --- SendSignonHandshake -------------------------------------------

func TestSendSignonHandshake_NilClient(t *testing.T) {
	if err := SendSignonHandshake(nil, signonInfoFixture()); err != nil {
		t.Errorf("got %v want nil", err)
	}
}

func TestSendSignonHandshake_InactiveClient(t *testing.T) {
	c := NewClient()
	c.Active = false
	if err := SendSignonHandshake(c, signonInfoFixture()); err != nil {
		t.Errorf("got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("inactive client got %d bytes queued, want 0", c.Message.Len())
	}
}

func TestSendSignonHandshake_NilMessage(t *testing.T) {
	c := NewClient()
	c.Active = true
	c.Message = nil
	if err := SendSignonHandshake(c, signonInfoFixture()); err != nil {
		t.Errorf("got %v want nil", err)
	}
}

// HappyPath: bytes land in client.Message and match the documented
// shape (svc_serverinfo trailer signonnum(1) + signonnum(2/3/4)).
func TestSendSignonHandshake_HappyPath(t *testing.T) {
	c := NewClient()
	c.Active = true
	c.SendSignon = true
	if err := SendSignonHandshake(c, signonInfoFixture()); err != nil {
		t.Fatalf("SendSignonHandshake: %v", err)
	}
	if c.SendSignon {
		t.Error("SendSignon: got true want false (handshake queued)")
	}
	if c.Message.Len() == 0 {
		t.Fatal("Message empty after handshake")
	}

	// Walk the queued bytes: serverinfo header + ... + signonnum(1)
	// from EncodeServerInfo, then signonnum(2/3/4) appended here.
	r := msg.NewReader(c.Message.Bytes())
	if op := r.ReadU8(); op != protocol.SvcServerInfo {
		t.Errorf("opcode[0]: got %d want SvcServerInfo (%d)", op, protocol.SvcServerInfo)
	}
	// Skip protocol/maxclients/gametype/levelname + precache walks
	// by re-parsing them; we want to land on the trailing signon
	// pairs. EncodeServerInfo's shape is stable + already covered by
	// serverinfo_test.go, so we just consume past it.
	_ = r.ReadLong()   // protocol
	_ = r.ReadU8()     // maxclients
	_ = r.ReadU8()     // gametype
	_ = r.ReadString() // levelname
	for {              // model precache (sentinel-terminated)
		s := r.ReadString()
		if s == "" {
			break
		}
	}
	for { // sound precache (sentinel-terminated)
		s := r.ReadString()
		if s == "" {
			break
		}
	}

	// Stage byte-pairs: (signonnum, 1), (signonnum, 2), (signonnum, 3), (signonnum, 4).
	for _, want := range []int{1, 2, 3, 4} {
		op := r.ReadU8()
		if op != protocol.SvcSignonNum {
			t.Errorf("stage %d header: got %d want SvcSignonNum (%d)", want, op, protocol.SvcSignonNum)
		}
		stage := r.ReadU8()
		if int(stage) != want {
			t.Errorf("stage %d byte: got %d want %d", want, stage, want)
		}
	}
}

// EncodeServerInfo error (empty LevelName) propagates verbatim.
func TestSendSignonHandshake_PropagatesServerInfoError(t *testing.T) {
	c := NewClient()
	c.Active = true
	info := signonInfoFixture()
	info.LevelName = ""
	err := SendSignonHandshake(c, info)
	if !errors.Is(err, ErrEmptyLevelName) {
		t.Errorf("got %v want ErrEmptyLevelName", err)
	}
}

// --- SendServerInfo ------------------------------------------------

func TestSendServerInfo_NilClient(t *testing.T) {
	if err := SendServerInfo(nil, signonInfoFixture()); err != nil {
		t.Errorf("got %v want nil", err)
	}
}

func TestSendServerInfo_InactiveClient(t *testing.T) {
	c := NewClient()
	c.Active = false
	if err := SendServerInfo(c, signonInfoFixture()); err != nil {
		t.Errorf("got %v want nil", err)
	}
	if c.Message.Len() != 0 {
		t.Errorf("inactive client got %d bytes queued, want 0", c.Message.Len())
	}
}

func TestSendServerInfo_NilMessage(t *testing.T) {
	c := NewClient()
	c.Active = true
	c.Message = nil
	if err := SendServerInfo(c, signonInfoFixture()); err != nil {
		t.Errorf("got %v want nil", err)
	}
}

// SendServerInfo queues serverinfo + signonnum(2) + signonnum(3) --
// stage 4 is INTENTIONALLY omitted (the wire-driven flow expects the
// client's "spawn" clc_stringcmd to trigger it via ParseClcStringCmd).
func TestSendServerInfo_HappyPath(t *testing.T) {
	c := NewClient()
	c.Active = true
	c.SendSignon = true
	if err := SendServerInfo(c, signonInfoFixture()); err != nil {
		t.Fatalf("SendServerInfo: %v", err)
	}
	if c.SendSignon {
		t.Error("SendSignon: got true want false (prefix queued)")
	}
	r := msg.NewReader(c.Message.Bytes())
	if op := r.ReadU8(); op != protocol.SvcServerInfo {
		t.Errorf("opcode[0]: got %d want SvcServerInfo (%d)", op, protocol.SvcServerInfo)
	}
	_ = r.ReadLong()
	_ = r.ReadU8()
	_ = r.ReadU8()
	_ = r.ReadString()
	for {
		s := r.ReadString()
		if s == "" {
			break
		}
	}
	for {
		s := r.ReadString()
		if s == "" {
			break
		}
	}
	// Stage byte-pairs: (signonnum, 1) auto-emitted by EncodeServerInfo,
	// then (signonnum, 2) + (signonnum, 3) appended here. Stage 4 must
	// NOT appear.
	for _, want := range []int{1, 2, 3} {
		op := r.ReadU8()
		if op != protocol.SvcSignonNum {
			t.Errorf("stage %d header: got %d want SvcSignonNum (%d)", want, op, protocol.SvcSignonNum)
		}
		stage := r.ReadU8()
		if int(stage) != want {
			t.Errorf("stage %d byte: got %d want %d", want, stage, want)
		}
	}
	// Buffer must end here -- no stage-4 bytes.
	if !r.Bad() {
		// Try one more read; if it succeeds we have extra bytes.
		extra := r.ReadU8()
		if !r.Bad() {
			t.Errorf("extra trailing byte = %d (stage 4 was queued by mistake)", extra)
		}
	}
}

func TestSendServerInfo_PropagatesServerInfoError(t *testing.T) {
	c := NewClient()
	c.Active = true
	info := signonInfoFixture()
	info.LevelName = ""
	if err := SendServerInfo(c, info); !errors.Is(err, ErrEmptyLevelName) {
		t.Errorf("got %v want ErrEmptyLevelName", err)
	}
}

func TestSendServerInfo_PropagatesSignonOverflow(t *testing.T) {
	c := NewClient()
	c.Active = true
	probe := sizebuf.New(make([]byte, 1024))
	if err := EncodeServerInfo(probe, signonInfoFixture()); err != nil {
		t.Fatalf("probe encode: %v", err)
	}
	// Cap c.Message to exactly the serverinfo length so the first
	// appended stage-2 byte overflows.
	c.Message = sizebuf.New(make([]byte, probe.Len()))
	if err := SendServerInfo(c, signonInfoFixture()); err == nil {
		t.Fatal("got nil err on overflow")
	}
}

// Overflow on the signonnum tail propagates: pre-fill the buffer so
// EncodeServerInfo just fits and the first appended signonnum byte
// trips the overflow guard.
func TestSendSignonHandshake_PropagatesSignonOverflow(t *testing.T) {
	c := NewClient()
	c.Active = true
	// Encode once into a sizing buffer to learn the serverinfo length,
	// then cap the real client.Message buffer to that exact length so
	// the trailing signonnum(2) appends overflow.
	probe := sizebuf.New(make([]byte, 1024))
	if err := EncodeServerInfo(probe, signonInfoFixture()); err != nil {
		t.Fatalf("probe encode: %v", err)
	}
	c.Message = sizebuf.New(make([]byte, probe.Len()))
	err := SendSignonHandshake(c, signonInfoFixture())
	if err == nil {
		t.Fatal("got nil err on overflow")
	}
}
