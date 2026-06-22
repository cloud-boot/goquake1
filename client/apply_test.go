// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"
	"strings"
	"testing"
)

// unknownDecoded is a Decoded variant defined ONLY in this test file,
// used to exercise Apply's forward-compatibility guard (the default
// arm that returns ErrApplyUnknown).
type unknownDecoded struct{}

func (unknownDecoded) isDecoded() {}

// --- Sentinels ----------------------------------------------------------

func TestApply_NilState(t *testing.T) {
	err := Apply(nil, DecodedNop{}, 1.0)
	if !errors.Is(err, ErrApplyNilState) {
		t.Errorf("got %v want ErrApplyNilState", err)
	}
}

func TestApply_NilMessage(t *testing.T) {
	s := NewState()
	err := Apply(s, nil, 1.0)
	if !errors.Is(err, ErrApplyNilMessage) {
		t.Errorf("got %v want ErrApplyNilMessage", err)
	}
}

func TestApply_UnknownType(t *testing.T) {
	s := NewState()
	err := Apply(s, unknownDecoded{}, 2.5)
	if !errors.Is(err, ErrApplyUnknown) {
		t.Errorf("got %v want ErrApplyUnknown", err)
	}
	// MsgTime is updated BEFORE the type switch, so even an unknown
	// type still records nowSec.
	if s.MsgTime != 2.5 {
		t.Errorf("MsgTime: got %v want 2.5", s.MsgTime)
	}
}

// --- MsgTime always-on side effect --------------------------------------

func TestApply_MsgTime_UpdatedOnEveryCall(t *testing.T) {
	cases := []struct {
		name string
		msg  Decoded
	}{
		{"Nop", DecodedNop{}},
		{"Print", DecodedPrint{Text: "hi"}},
		{"StuffText", DecodedStuffText{Text: "echo hi"}},
		{"Finale", DecodedFinale{Text: "the end"}},
		{"Cutscene", DecodedCutscene{Text: "..."}},
		{"SellScreen", DecodedSellScreen{}},
		{"KilledMonster", DecodedKilledMonster{}},
		{"FoundSecret", DecodedFoundSecret{}},
		{"Particle", DecodedParticle{}},
		{"Sound", DecodedSound{}},
		{"Baseline", DecodedBaseline{}},
		{"Update", DecodedUpdate{}},
		{"SetView", DecodedSetView{}},
		{"UpdateStat", DecodedUpdateStat{Stat: 0, Value: 1}},
		{"UpdateName", DecodedUpdateName{}},
		{"UpdateColors", DecodedUpdateColors{}},
		{"UpdateFrags", DecodedUpdateFrags{Slot: 0, Frags: 1}},
		{"ClientData", DecodedClientData{}},
		{"ServerInfo", DecodedServerInfo{}},
		{"SignonNum-1", DecodedSignonNum{Stage: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewState()
			s.MsgTime = -1
			if err := Apply(s, c.msg, 7.25); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if s.MsgTime != 7.25 {
				t.Errorf("MsgTime: got %v want 7.25", s.MsgTime)
			}
		})
	}
}

// --- DecodedNop ---------------------------------------------------------

func TestApply_Nop_NoMutation(t *testing.T) {
	s := NewState()
	s.Health = 99
	s.PlayerNum = 3
	if err := Apply(s, DecodedNop{}, 1.0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Health != 99 || s.PlayerNum != 3 {
		t.Errorf("Nop mutated state: Health=%d PlayerNum=%d", s.Health, s.PlayerNum)
	}
}

// --- DecodedServerInfo --------------------------------------------------

func TestApply_ServerInfo(t *testing.T) {
	s := NewState()
	s.PlayerNum = 9 // must be reset to 0
	// Pre-set Connection to verify Apply doesn't touch it.
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	msg := DecodedServerInfo{
		Protocol:      15,
		MaxClients:    8,
		GameType:      0,
		LevelName:     "Slipgate Complex",
		ModelPrecache: []string{"progs/player.mdl", "progs/eyes.mdl"},
		SoundPrecache: []string{"weapons/rocket1i.wav"},
	}
	if err := Apply(s, msg, 0.5); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.MapName != "Slipgate Complex" {
		t.Errorf("MapName: got %q", s.MapName)
	}
	if s.LevelName != "Slipgate Complex" {
		t.Errorf("LevelName: got %q", s.LevelName)
	}
	if len(s.ModelPrecache) != 2 || s.ModelPrecache[0] != "progs/player.mdl" {
		t.Errorf("ModelPrecache: got %v", s.ModelPrecache)
	}
	if len(s.SoundPrecache) != 1 || s.SoundPrecache[0] != "weapons/rocket1i.wav" {
		t.Errorf("SoundPrecache: got %v", s.SoundPrecache)
	}
	if s.PlayerNum != 0 {
		t.Errorf("PlayerNum: got %d want 0", s.PlayerNum)
	}
	// Verify the precaches are owned copies, not aliases.
	msg.ModelPrecache[0] = "MUTATED"
	if s.ModelPrecache[0] == "MUTATED" {
		t.Error("ModelPrecache aliased the input slice; should be a copy")
	}
	if s.Connection != StateConnecting {
		t.Errorf("Apply must not change Connection (got %d)", s.Connection)
	}
}

// --- DecodedSignonNum ---------------------------------------------------

func TestApply_SignonNum_Stage4_FromConnecting(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := Apply(s, DecodedSignonNum{Stage: 4}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Connection != StateConnected {
		t.Errorf("Connection: got %d want StateConnected", s.Connection)
	}
	if !s.Spawned {
		t.Error("Spawned: got false want true")
	}
}

func TestApply_SignonNum_Stage4_FromConnected_WrapsErr(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("MarkSpawned: %v", err)
	}
	err := Apply(s, DecodedSignonNum{Stage: 4}, 0)
	if err == nil {
		t.Fatal("Apply: got nil, want wrapped ErrApplyBadState")
	}
	if !errors.Is(err, ErrApplyBadState) {
		t.Errorf("errors.Is ErrApplyBadState: false (err=%v)", err)
	}
	if !errors.Is(err, ErrNotConnecting) {
		t.Errorf("errors.Is ErrNotConnecting: false (err=%v)", err)
	}
	if u := errors.Unwrap(err); u != ErrNotConnecting {
		t.Errorf("errors.Unwrap: got %v want ErrNotConnecting", u)
	}
	if !strings.Contains(err.Error(), ErrNotConnecting.Error()) {
		t.Errorf("Error() should mention underlying: got %q", err.Error())
	}
}

// TestApply_SignonNum_Stage1_FromDisconnected_DrivesConnecting asserts
// the wire-driven establish-connection rule applySignonNum implements:
// the first svc_signonnum byte (stage 1) from a Disconnected client
// transitions it into StateConnecting, matching the C upstream's
// CL_EstablishConnection (cls.state = ca_connected) but driven by the
// server's wire bytes instead of a caller-side pre-step. Spawned stays
// false until stage 4 lands.
func TestApply_SignonNum_Stage1_FromDisconnected_DrivesConnecting(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedSignonNum{Stage: 1}, 0); err != nil {
		t.Fatalf("stage 1: Apply: %v", err)
	}
	if s.Connection != StateConnecting {
		t.Errorf("stage 1: Connection got %d want StateConnecting", s.Connection)
	}
	if s.Spawned {
		t.Error("stage 1: Spawned flipped true")
	}
}

// TestApply_SignonNum_Stage1_FromConnecting_NoChange covers the
// idempotence guard: a stage-1 retransmit on an already-Connecting
// state must not error (SetConnecting rejects StateConnecting), and
// must leave Connection untouched.
func TestApply_SignonNum_Stage1_FromConnecting_NoChange(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := Apply(s, DecodedSignonNum{Stage: 1}, 0); err != nil {
		t.Fatalf("stage 1: Apply: %v", err)
	}
	if s.Connection != StateConnecting {
		t.Errorf("stage 1: Connection got %d want StateConnecting", s.Connection)
	}
	if s.Spawned {
		t.Error("stage 1: Spawned flipped true")
	}
}

// TestApply_SignonNum_Stage1_FromConnected_NoChange covers the
// post-spawn retransmit case: a stale stage-1 byte arriving after the
// client has already reached StateConnected must not regress the
// state. SetConnecting would reject StateConnected; the
// applySignonNum guard short-circuits before calling it.
func TestApply_SignonNum_Stage1_FromConnected_NoChange(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("MarkSpawned: %v", err)
	}
	if err := Apply(s, DecodedSignonNum{Stage: 1}, 0); err != nil {
		t.Fatalf("stage 1: Apply: %v", err)
	}
	if s.Connection != StateConnected {
		t.Errorf("stage 1: Connection regressed (got %d want StateConnected)", s.Connection)
	}
	if !s.Spawned {
		t.Error("stage 1: Spawned regressed to false")
	}
}

// TestApply_SignonNum_Stages23_NoTransition asserts stages 2 + 3 are
// pure no-ops on any starting state (the upstream uses them as
// triggers for outbound clc_stringcmd commands the Go port doesn't
// yet emit).
func TestApply_SignonNum_Stages23_NoTransition(t *testing.T) {
	for _, stage := range []int{2, 3} {
		s := NewState()
		if err := Apply(s, DecodedSignonNum{Stage: stage}, 0); err != nil {
			t.Fatalf("stage %d: Apply: %v", stage, err)
		}
		if s.Connection != StateDisconnected {
			t.Errorf("stage %d: Connection moved (got %d want StateDisconnected)", stage, s.Connection)
		}
		if s.Spawned {
			t.Errorf("stage %d: Spawned flipped true", stage)
		}
	}
}

// --- DecodedDisconnect --------------------------------------------------

func TestApply_Disconnect(t *testing.T) {
	s := NewState()
	if err := s.SetConnecting(); err != nil {
		t.Fatalf("SetConnecting: %v", err)
	}
	if err := s.MarkSpawned(); err != nil {
		t.Fatalf("MarkSpawned: %v", err)
	}
	s.MapName = "e1m1"
	s.Health = 42
	if err := Apply(s, DecodedDisconnect{}, 3.0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Connection != StateDisconnected {
		t.Errorf("Connection: got %d want StateDisconnected", s.Connection)
	}
	if s.Spawned {
		t.Error("Spawned: got true want false")
	}
	if s.MapName != "" {
		t.Errorf("MapName: got %q want \"\"", s.MapName)
	}
	if s.Health != 0 {
		t.Errorf("Health: got %d want 0", s.Health)
	}
}

// --- DecodedSetView -----------------------------------------------------

func TestApply_SetView(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedSetView{EntityNum: 7}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.PlayerNum != 7 {
		t.Errorf("PlayerNum: got %d want 7", s.PlayerNum)
	}
}

// --- DecodedClientData --------------------------------------------------

func TestApply_ClientData(t *testing.T) {
	s := NewState()
	msg := DecodedClientData{
		ViewHeightOffset: 22.5,
		IdealPitch:       -8,
		PunchAngle:       [3]float32{1, 2, 3},
		Velocity:         [3]float32{16, -32, 48},
		Items:            0x7eadbeef,
		OnGround:         true,
		InWater:          true,
		Health:           75,
		CurrentAmmo:      40,
		Ammo:             [4]int{10, 20, 30, 40},
	}
	if err := Apply(s, msg, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.ViewHeightOffset != 22.5 {
		t.Errorf("ViewHeightOffset: got %v", s.ViewHeightOffset)
	}
	if s.IdealPitch != -8 {
		t.Errorf("IdealPitch: got %v", s.IdealPitch)
	}
	if s.PunchAngle != ([3]float32{1, 2, 3}) {
		t.Errorf("PunchAngle: got %v", s.PunchAngle)
	}
	if s.Velocity != ([3]float32{16, -32, 48}) {
		t.Errorf("Velocity: got %v", s.Velocity)
	}
	if s.Items != 0x7eadbeef {
		t.Errorf("Items: got %x", s.Items)
	}
	if !s.OnGround {
		t.Error("OnGround: got false want true")
	}
	if !s.InWater {
		t.Error("InWater: got false want true")
	}
	if s.Health != 75 {
		t.Errorf("Health: got %d want 75", s.Health)
	}
	if s.CurrentAmmo != 40 {
		t.Errorf("CurrentAmmo: got %d want 40", s.CurrentAmmo)
	}
	if s.Ammo != ([4]int{10, 20, 30, 40}) {
		t.Errorf("Ammo: got %v", s.Ammo)
	}
}

// --- DecodedUpdateStat --------------------------------------------------

func TestApply_UpdateStat_InRange(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedUpdateStat{Stat: 5, Value: 1234}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Stats[5] != 1234 {
		t.Errorf("Stats[5]: got %d want 1234", s.Stats[5])
	}
}

func TestApply_UpdateStat_BoundaryZero(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedUpdateStat{Stat: 0, Value: 7}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Stats[0] != 7 {
		t.Errorf("Stats[0]: got %d want 7", s.Stats[0])
	}
}

func TestApply_UpdateStat_BoundaryHigh(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedUpdateStat{Stat: len(s.Stats) - 1, Value: 9}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Stats[len(s.Stats)-1] != 9 {
		t.Errorf("Stats[last]: got %d want 9", s.Stats[len(s.Stats)-1])
	}
}

func TestApply_UpdateStat_OutOfRange_SilentSkip(t *testing.T) {
	s := NewState()
	// Negative.
	if err := Apply(s, DecodedUpdateStat{Stat: -1, Value: 99}, 0); err != nil {
		t.Errorf("Apply (neg): %v", err)
	}
	// Past end.
	if err := Apply(s, DecodedUpdateStat{Stat: len(s.Stats), Value: 99}, 0); err != nil {
		t.Errorf("Apply (over): %v", err)
	}
	// Nothing should have been written.
	for i, v := range s.Stats {
		if v != 0 {
			t.Errorf("Stats[%d]: got %d want 0 (no write expected)", i, v)
		}
	}
}

// --- DecodedUpdateName / UpdateColors (intentional no-op + ack) ---------

func TestApply_UpdateName_AckOnly(t *testing.T) {
	s := NewState()
	// Pre-set state to verify nothing relevant changes.
	s.Health = 50
	if err := Apply(s, DecodedUpdateName{Slot: 2, Name: "player2"}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Health != 50 {
		t.Errorf("UpdateName must not mutate Health: got %d", s.Health)
	}
}

func TestApply_UpdateColors_AckOnly(t *testing.T) {
	s := NewState()
	s.Health = 50
	if err := Apply(s, DecodedUpdateColors{Slot: 2, Colors: 0x42}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Health != 50 {
		t.Errorf("UpdateColors must not mutate Health: got %d", s.Health)
	}
}

// --- DecodedUpdateFrags -------------------------------------------------

func TestApply_UpdateFrags_InRange(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedUpdateFrags{Slot: 3, Frags: 17}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Frags[3] != 17 {
		t.Errorf("Frags[3]: got %d want 17", s.Frags[3])
	}
}

func TestApply_UpdateFrags_BoundaryZero(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedUpdateFrags{Slot: 0, Frags: 1}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Frags[0] != 1 {
		t.Errorf("Frags[0]: got %d want 1", s.Frags[0])
	}
}

func TestApply_UpdateFrags_BoundaryHigh(t *testing.T) {
	s := NewState()
	last := len(s.Frags) - 1
	if err := Apply(s, DecodedUpdateFrags{Slot: last, Frags: 42}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Frags[last] != 42 {
		t.Errorf("Frags[last]: got %d want 42", s.Frags[last])
	}
}

func TestApply_UpdateFrags_OutOfRange_SilentSkip(t *testing.T) {
	s := NewState()
	if err := Apply(s, DecodedUpdateFrags{Slot: -1, Frags: 99}, 0); err != nil {
		t.Errorf("Apply (neg): %v", err)
	}
	if err := Apply(s, DecodedUpdateFrags{Slot: len(s.Frags), Frags: 99}, 0); err != nil {
		t.Errorf("Apply (over): %v", err)
	}
	for i, v := range s.Frags {
		if v != 0 {
			t.Errorf("Frags[%d]: got %d want 0 (no write expected)", i, v)
		}
	}
}

// --- DecodedBaseline ----------------------------------------------------

// TestApply_Baseline_CachesIntoState verifies the per-entity baseline
// is folded into State.Baselines keyed by EntityNum, with every field
// copied verbatim. Mirrors the upstream cl_entities[ent].baseline write.
func TestApply_Baseline_CachesIntoState(t *testing.T) {
	s := NewState()
	bl := DecodedBaseline{
		EntityNum: 42,
		ModelIdx:  7,
		Frame:     3,
		ColorMap:  2,
		SkinNum:   1,
		Origin:    [3]float32{8, 16, 24},
		Angles:    [3]float32{0, 90, 180},
		Alpha:     0,
	}
	if err := Apply(s, bl, 1.5); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, ok := s.Baselines[42]
	if !ok {
		t.Fatalf("Baselines[42] missing; map = %v", s.Baselines)
	}
	want := EntityBaseline{
		ModelIdx: 7,
		Frame:    3,
		ColorMap: 2,
		SkinNum:  1,
		Origin:   [3]float32{8, 16, 24},
		Angles:   [3]float32{0, 90, 180},
		Alpha:    0,
	}
	if got != want {
		t.Errorf("Baselines[42]: got %+v want %+v", got, want)
	}
}

// Apply must lazily allocate Baselines if a caller built a State
// without going through NewState (e.g. test stubs, partial-construction).
func TestApply_Baseline_LazilyAllocatesMap(t *testing.T) {
	s := &State{} // no NewState; Baselines is nil
	if err := Apply(s, DecodedBaseline{EntityNum: 1, ModelIdx: 9}, 0); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if s.Baselines == nil {
		t.Fatal("Baselines: got nil want allocated")
	}
	if s.Baselines[1].ModelIdx != 9 {
		t.Errorf("Baselines[1].ModelIdx: got %d want 9", s.Baselines[1].ModelIdx)
	}
}

// Sequential baselines for distinct entity numbers all survive and
// don't collide.
func TestApply_Baseline_MultipleEntities(t *testing.T) {
	s := NewState()
	for i := 0; i < 10; i++ {
		if err := Apply(s, DecodedBaseline{EntityNum: i, ModelIdx: i * 7}, 0); err != nil {
			t.Fatalf("Apply[%d]: %v", i, err)
		}
	}
	if len(s.Baselines) != 10 {
		t.Errorf("Baselines len: got %d want 10", len(s.Baselines))
	}
	for i := 0; i < 10; i++ {
		if s.Baselines[i].ModelIdx != i*7 {
			t.Errorf("Baselines[%d].ModelIdx: got %d want %d", i, s.Baselines[i].ModelIdx, i*7)
		}
	}
}

// --- Documented no-op arms (Print/StuffText/Finale/Cutscene/SellScreen/
// KilledMonster/FoundSecret/Particle/Sound/Update) -----------------------

func TestApply_DocumentedNoOps_DoNotMutate(t *testing.T) {
	cases := []struct {
		name string
		msg  Decoded
	}{
		{"Print", DecodedPrint{Text: "hi"}},
		{"StuffText", DecodedStuffText{Text: "exec config.cfg"}},
		{"Finale", DecodedFinale{Text: "Episode 1 complete"}},
		{"Cutscene", DecodedCutscene{Text: "..."}},
		{"SellScreen", DecodedSellScreen{}},
		{"KilledMonster", DecodedKilledMonster{}},
		{"FoundSecret", DecodedFoundSecret{}},
		{"Particle", DecodedParticle{Origin: [3]float32{1, 2, 3}, Count: 10}},
		{"Sound", DecodedSound{EntityIdx: 5, SoundNum: 10, Volume: 200}},
		{"Update", DecodedUpdate{EntityNum: 4, Bits: 0xf0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewState()
			// Pre-populate fields that ARE mutated by other arms so a
			// no-op accidentally writing to one of them would show up.
			s.Health = 88
			s.PlayerNum = 4
			s.MapName = "test"
			s.LevelName = "Test Map"
			s.Stats[0] = 13
			s.Frags[0] = 6
			s.Items = 0x42
			s.Connection = StateConnecting
			s.Spawned = false
			s.OnGround = true
			s.CurrentAmmo = 9
			s.Velocity = [3]float32{1, 2, 3}

			if err := Apply(s, c.msg, 11.0); err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if s.Health != 88 || s.PlayerNum != 4 || s.MapName != "test" ||
				s.LevelName != "Test Map" || s.Stats[0] != 13 || s.Frags[0] != 6 ||
				s.Items != 0x42 || s.Connection != StateConnecting || s.Spawned ||
				!s.OnGround || s.CurrentAmmo != 9 ||
				s.Velocity != ([3]float32{1, 2, 3}) {
				t.Errorf("state mutated by no-op arm: %+v", s)
			}
		})
	}
}

// --- applyBadStateErr internals -----------------------------------------

// Direct exercise of the wrapper's Is/Unwrap protocol to lock the
// contract even if no public Apply path produces a non-matching
// target.
func TestApplyBadStateErr_IsAndUnwrap(t *testing.T) {
	w := &applyBadStateErr{underlying: ErrAlreadyConnected}
	if !errors.Is(w, ErrApplyBadState) {
		t.Error("Is(ErrApplyBadState): false")
	}
	if !errors.Is(w, ErrAlreadyConnected) {
		t.Error("Is(ErrAlreadyConnected): false")
	}
	if errors.Unwrap(w) != ErrAlreadyConnected {
		t.Errorf("Unwrap: got %v want ErrAlreadyConnected", errors.Unwrap(w))
	}
	// A category-mismatch target must return false (covers the
	// non-matching branch of Is).
	otherSentinel := errors.New("unrelated")
	if errors.Is(w, otherSentinel) {
		t.Error("Is(unrelated): true; want false")
	}
}
