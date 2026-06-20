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

// --- input-validation guards --------------------------------------------

func TestEncodeServerInfo_NilBufErrors(t *testing.T) {
	if err := EncodeServerInfo(nil, ServerInfo{LevelName: "x"}); err == nil {
		t.Error("expected error on nil sizebuf")
	}
}

func TestEncodeServerInfo_EmptyLevelNameErrors(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeServerInfo(buf, ServerInfo{
		Protocol:   protocol.VersionNQ,
		MaxClients: 8,
		GameType:   GameTypeCoop,
		LevelName:  "",
	})
	if !errors.Is(err, ErrEmptyLevelName) {
		t.Errorf("got %v want ErrEmptyLevelName", err)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer modified on empty-name reject: len=%d", buf.Len())
	}
}

// --- happy path: full handshake round-trips through msg.Read* -----------

// Encodes a serverinfo with two model + two sound precache entries and
// reads every field back in wire order. Exercises both precache loops
// (including the `break` exit on the empty-slot sentinel) and the
// final svc_signonnum + 1 marker.
func TestEncodeServerInfo_HappyPath(t *testing.T) {
	buf := sizebuf.New(make([]byte, 128))
	info := ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    8,
		GameType:      GameTypeDeathmatch,
		LevelName:     "The Slipgate Complex",
		ModelPrecache: []string{"maps/e1m1.bsp", "*1", "progs/player.mdl", "", "ignored"},
		SoundPrecache: []string{"", "weapons/sshotgun.wav", "items/itembk2.wav", "", "ignored"},
	}
	if err := EncodeServerInfo(buf, info); err != nil {
		t.Fatal(err)
	}

	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcServerInfo {
		t.Errorf("cmd byte: got %d want %d (SvcServerInfo)", cmd, protocol.SvcServerInfo)
	}
	if proto := r.ReadLong(); proto != int32(protocol.VersionNQ) {
		t.Errorf("protocol: got %d want %d", proto, protocol.VersionNQ)
	}
	if mc := r.ReadU8(); mc != 8 {
		t.Errorf("max_clients: got %d want 8", mc)
	}
	if gt := r.ReadU8(); gt != int(GameTypeDeathmatch) {
		t.Errorf("gametype: got %d want %d", gt, GameTypeDeathmatch)
	}
	if name := r.ReadString(); name != "The Slipgate Complex" {
		t.Errorf("level name: got %q", name)
	}

	// Model precache walk: slot 0 is skipped, walk stops at the
	// empty slot (slot 3 in the input), so we expect "*1" then
	// "progs/player.mdl" then the NUL sentinel.
	for _, want := range []string{"*1", "progs/player.mdl"} {
		if got := r.ReadString(); got != want {
			t.Errorf("model: got %q want %q", got, want)
		}
	}
	if sent := r.ReadU8(); sent != 0 {
		t.Errorf("model sentinel: got %d want 0", sent)
	}

	for _, want := range []string{"weapons/sshotgun.wav", "items/itembk2.wav"} {
		if got := r.ReadString(); got != want {
			t.Errorf("sound: got %q want %q", got, want)
		}
	}
	if sent := r.ReadU8(); sent != 0 {
		t.Errorf("sound sentinel: got %d want 0", sent)
	}

	if sn := r.ReadU8(); sn != protocol.SvcSignonNum {
		t.Errorf("signon cmd: got %d want %d", sn, protocol.SvcSignonNum)
	}
	if stage := r.ReadU8(); stage != 1 {
		t.Errorf("signon stage: got %d want 1", stage)
	}

	if r.Bad() {
		t.Error("reader hit EOF mid-handshake (encoder shorted a field)")
	}
}

// Precaches that contain only the slot-0 reserved entry (or are
// length 1 / 0): both loops should exit immediately without writing
// any name strings. Exercises:
//
//   - the `for i := 1; i < len(...)` natural exit (no break)
//   - back-to-back sentinel bytes after empty-list loops
func TestEncodeServerInfo_EmptyPrecaches(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	info := ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    1,
		GameType:      GameTypeCoop,
		LevelName:     "x",
		ModelPrecache: []string{"world.bsp"}, // len=1: loop body never runs
		SoundPrecache: []string{},            // len=0: loop body never runs
	}
	if err := EncodeServerInfo(buf, info); err != nil {
		t.Fatal(err)
	}

	// Wire shape: cmd(1) + proto(4) + maxclients(1) + gametype(1) +
	// "x"\0 (2) + model_sentinel(1) + sound_sentinel(1) + signonnum(1) +
	// stage(1) = 13 bytes.
	if buf.Len() != 13 {
		t.Errorf("empty-precache wire size: got %d want 13", buf.Len())
	}

	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()     // SvcServerInfo
	_ = r.ReadLong()   // protocol
	_ = r.ReadU8()     // maxclients
	_ = r.ReadU8()     // gametype
	_ = r.ReadString() // level name
	if b := r.ReadU8(); b != 0 {
		t.Errorf("model sentinel: got %d want 0", b)
	}
	if b := r.ReadU8(); b != 0 {
		t.Errorf("sound sentinel: got %d want 0", b)
	}
	if b := r.ReadU8(); b != protocol.SvcSignonNum {
		t.Errorf("signon cmd: got %d want %d", b, protocol.SvcSignonNum)
	}
	if b := r.ReadU8(); b != 1 {
		t.Errorf("signon stage: got %d want 1", b)
	}
}

// Coop gametype byte round-trips (the happy path uses Deathmatch).
func TestEncodeServerInfo_GameTypeCoop(t *testing.T) {
	buf := sizebuf.New(make([]byte, 32))
	info := ServerInfo{
		Protocol:   protocol.VersionNQ,
		MaxClients: 1,
		GameType:   GameTypeCoop,
		LevelName:  "x",
	}
	if err := EncodeServerInfo(buf, info); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8()   // cmd
	_ = r.ReadLong() // proto
	_ = r.ReadU8()   // maxclients
	if gt := r.ReadU8(); gt != int(GameTypeCoop) {
		t.Errorf("gametype: got %d want %d (Coop)", gt, GameTypeCoop)
	}
}

// --- per-write overflow propagation -------------------------------------

// Walk through every msg.Write* call site by sizing the buffer just
// short of the next write. The fixture is the smallest non-trivial
// encode -- one model + one sound precache entry, level name "a" --
// so each write is the next one to fail at a known cap.
//
// Wire layout (19 bytes total):
//
//	[0]    SvcServerInfo                  -> cap 0 fails here
//	[1..5) Protocol (long, 4B)            -> cap 1 fails here
//	[5]    MaxClients                     -> cap 5 fails here
//	[6]    GameType                       -> cap 6 fails here
//	[7..9) "a"\0   (2B)                   -> cap 7 fails here
//	[9..12) "m1"\0 (3B)                   -> cap 9 fails here
//	[12]   model sentinel 0               -> cap 12 fails here
//	[13..16) "s1"\0 (3B)                  -> cap 13 fails here
//	[16]   sound sentinel 0               -> cap 16 fails here
//	[17]   SvcSignonNum                   -> cap 17 fails here
//	[18]   signon stage 1                 -> cap 18 fails here
//	(cap 19 succeeds.)
func TestEncodeServerInfo_PerWriteOverflowPropagates(t *testing.T) {
	info := ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    1,
		GameType:      GameTypeCoop,
		LevelName:     "a",
		ModelPrecache: []string{"world.bsp", "m1"},
		SoundPrecache: []string{"", "s1"},
	}
	for _, cap := range []int{0, 1, 5, 6, 7, 9, 12, 13, 16, 17, 18} {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, cap))
			err := EncodeServerInfo(buf, info)
			if err == nil {
				t.Errorf("cap=%d: expected write error, got nil", cap)
			}
			if errors.Is(err, ErrEmptyLevelName) {
				t.Errorf("cap=%d: leaked guard error, expected sizebuf overflow", cap)
			}
		})
	}
	// Sanity: cap 19 must succeed.
	buf := sizebuf.New(make([]byte, 19))
	if err := EncodeServerInfo(buf, info); err != nil {
		t.Errorf("cap=19: expected success, got %v", err)
	}
	if buf.Len() != 19 {
		t.Errorf("cap=19: wire size got %d want 19", buf.Len())
	}
}
