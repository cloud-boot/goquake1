// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// --- MapBSPPath ----------------------------------------------------------

func TestMapBSPPath_HappyPath(t *testing.T) {
	got, err := MapBSPPath("e1m1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "maps/e1m1.bsp" {
		t.Errorf("got %q want maps/e1m1.bsp", got)
	}
}

func TestMapBSPPath_EmptyErrors(t *testing.T) {
	_, err := MapBSPPath("")
	if !errors.Is(err, ErrEmptyMapName) {
		t.Errorf("got %v want ErrEmptyMapName", err)
	}
}

// --- ClampSkill ----------------------------------------------------------

func TestClampSkill_RoundsAndClamps(t *testing.T) {
	cases := []struct {
		in   float32
		want int
	}{
		// Exact integers in range -> unchanged.
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 3},
		// Rounding: .5 rounds away from zero (math.Round semantics).
		{0.4, 0},
		{0.6, 1},
		{1.5, 2},
		{2.5, 3},
		// Clamp low.
		{-1, 0},
		{-100, 0},
		{-0.6, 0}, // rounds to -1, clamps to 0
		// Clamp high.
		{4, 3},
		{100, 3},
		{3.5, 3}, // rounds to 4, clamps to 3
	}
	for _, c := range cases {
		if got := ClampSkill(c.in); got != c.want {
			t.Errorf("ClampSkill(%v): got %d want %d", c.in, got, c.want)
		}
	}
}

// --- LocalModelName ------------------------------------------------------

func TestLocalModelName_HappyPath(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "*0"},
		{1, "*1"},
		{99, "*99"},
		{MaxModels - 1, "*2047"}, // upper inclusive bound
	}
	for _, c := range cases {
		got, err := LocalModelName(c.in)
		if err != nil {
			t.Errorf("idx=%d: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("idx=%d: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestLocalModelName_OutOfRange(t *testing.T) {
	cases := []int{-1, MaxModels, MaxModels + 1, 99999}
	for _, in := range cases {
		_, err := LocalModelName(in)
		if !errors.Is(err, ErrLocalModelIndex) {
			t.Errorf("idx=%d: got %v want ErrLocalModelIndex", in, err)
		}
	}
}

// --- (*Server).Reset ----------------------------------------------------

func TestServerReset_HappyPath(t *testing.T) {
	s := NewServer()
	if err := s.Reset("start", protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	if s.Name != "start" {
		t.Errorf("Name: got %q want start", s.Name)
	}
	if s.ModelName != "maps/start.bsp" {
		t.Errorf("ModelName: got %q want maps/start.bsp", s.ModelName)
	}
	if s.Protocol != protocol.VersionNQ {
		t.Errorf("Protocol: got %d want %d", s.Protocol, protocol.VersionNQ)
	}
	if s.State != StateLoading {
		t.Errorf("State: got %d want StateLoading", s.State)
	}
	if s.Active || s.Paused || s.LoadGame {
		t.Errorf("flags should be false: %+v", s)
	}
	if s.Time != 0 || s.LastCheck != 0 {
		t.Errorf("clocks: %v %d", s.Time, s.LastCheck)
	}
	if s.MaxEdicts != MaxEdicts {
		t.Errorf("MaxEdicts: got %d want %d", s.MaxEdicts, MaxEdicts)
	}
	if len(s.Edicts) != MaxEdicts {
		t.Errorf("Edicts len: got %d want %d", len(s.Edicts), MaxEdicts)
	}
	if s.NumEdicts != 0 {
		t.Errorf("NumEdicts: got %d want 0", s.NumEdicts)
	}
	if s.WorldModel != nil {
		t.Errorf("WorldModel should be nil after Reset, got %v", s.WorldModel)
	}
}

// Reset clears prior precache + buffer state.
func TestServerReset_WipesPrecachesAndBuffers(t *testing.T) {
	s := NewServer()
	// Pre-populate state.
	s.ModelPrecache[0] = "stale.bsp"
	s.SoundPrecache[5] = "stale.wav"
	s.LightStyles[2] = "abcd"
	_ = s.Datagram.Write([]byte{1, 2, 3, 4})
	_ = s.Signon.Write([]byte{9, 9, 9})
	s.Active = true
	s.State = StateActive

	if err := s.Reset("e1m1", protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}

	if s.ModelPrecache[0] != "" || s.SoundPrecache[5] != "" || s.LightStyles[2] != "" {
		t.Errorf("precaches not wiped: model=%q sound=%q lightstyle=%q",
			s.ModelPrecache[0], s.SoundPrecache[5], s.LightStyles[2])
	}
	if s.Datagram.Len() != 0 || s.Signon.Len() != 0 {
		t.Errorf("buffers not cleared: Datagram=%d Signon=%d", s.Datagram.Len(), s.Signon.Len())
	}
	if s.Active || s.State != StateLoading {
		t.Errorf("flags not reset: Active=%v State=%d", s.Active, s.State)
	}
}

// Reset on an empty Server (no NewServer call) allocates the
// buffers + edict pool the constructor would have made.
func TestServerReset_AllocatesIfMissing(t *testing.T) {
	s := &Server{} // raw zero value -- no constructor called
	if err := s.Reset("test", protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	if s.Datagram == nil || s.ReliableDatagram == nil || s.Signon == nil {
		t.Errorf("Reset should allocate missing buffers: %+v", s)
	}
	if s.Edicts == nil {
		t.Error("Reset should allocate Edicts pool")
	}
	if s.MaxEdicts != MaxEdicts {
		t.Errorf("MaxEdicts default: got %d want %d", s.MaxEdicts, MaxEdicts)
	}
}

// Reset rejects an empty map name.
func TestServerReset_EmptyNameErrors(t *testing.T) {
	s := NewServer()
	err := s.Reset("", protocol.VersionNQ)
	if !errors.Is(err, ErrEmptyMapName) {
		t.Errorf("got %v want ErrEmptyMapName", err)
	}
}

// Reset re-uses the existing Edicts slice when its capacity is
// already big enough -- catches accidental re-allocation that would
// invalidate references the caller held.
func TestServerReset_PreservesEdictsBacking(t *testing.T) {
	s := NewServer()
	// First Reset allocates fresh.
	if err := s.Reset("a", protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	beforePtr := &s.Edicts[0]
	// Second Reset should re-use the same backing array.
	if err := s.Reset("b", protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	afterPtr := &s.Edicts[0]
	if beforePtr != afterPtr {
		t.Errorf("Edicts backing reallocated; before=%p after=%p", beforePtr, afterPtr)
	}
}

// Reset on a Server whose MaxEdicts has been bumped above the
// default uses the bumped cap.
func TestServerReset_RespectsBumpedMaxEdicts(t *testing.T) {
	s := &Server{MaxEdicts: 16384}
	if err := s.Reset("test", protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	if s.MaxEdicts != 16384 || len(s.Edicts) != 16384 {
		t.Errorf("expected bumped MaxEdicts honored: got %d / len(%d)", s.MaxEdicts, len(s.Edicts))
	}
}

// Reset's buffer-nil-then-create path: NewServer pre-allocates,
// but a manually-constructed Server with nil buffers should get
// freshly-allocated ones via Reset.
func TestServerReset_CreatesAllThreeBuffers(t *testing.T) {
	s := &Server{}
	if err := s.Reset("test", protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	// Each buffer must hold its full cap (MaxDatagram for the
	// unreliable, MaxMsgLen for the two reliable).
	if err := s.Datagram.Write(make([]byte, MaxDatagram)); err != nil {
		t.Errorf("Datagram cap drift: %v", err)
	}
	if err := s.ReliableDatagram.Write(make([]byte, MaxMsgLen)); err != nil {
		t.Errorf("ReliableDatagram cap drift: %v", err)
	}
	if err := s.Signon.Write(make([]byte, MaxMsgLen)); err != nil {
		t.Errorf("Signon cap drift: %v", err)
	}
}

// Just keep sizebuf imported (compile-time bridge for future helpers).
var _ = sizebuf.New
