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

// --- ComposeBaselineFromEdict ---------------------------------------

func TestComposeBaselineFromEdict_NilArgs(t *testing.T) {
	got, hasModel := ComposeBaselineFromEdict(nil, nil, 0, 0, nil)
	if got != (EntityBaseline{}) || hasModel {
		t.Errorf("nil args: got %+v hasModel=%v want zero EntityBaseline + hasModel=false", got, hasModel)
	}

	// Half-nil: also zero (matches the "tolerant of structural absence"
	// stance used elsewhere in the package).
	got, hasModel = ComposeBaselineFromEdict(nil, &progs.Edict{}, 0, 0, nil)
	if got != (EntityBaseline{}) || hasModel {
		t.Errorf("nil progs: got %+v hasModel=%v want zero EntityBaseline + hasModel=false", got, hasModel)
	}
	got, hasModel = ComposeBaselineFromEdict(&progs.Progs{}, nil, 0, 0, nil)
	if got != (EntityBaseline{}) || hasModel {
		t.Errorf("nil edict: got %+v hasModel=%v want zero EntityBaseline + hasModel=false", got, hasModel)
	}
}

// TestComposeBaselineFromEdict_MissingFields exercises the "field not
// defined in QC" path: the EntVars reads return ErrFieldNotFound and
// the composer silently substitutes the QC zero default.
func TestComposeBaselineFromEdict_MissingFields_FallsBackToZero(t *testing.T) {
	p, e := emptyProgsAndEdict(t)
	bl, hasModel := ComposeBaselineFromEdict(p, e, 5, 4, nil) // entNum 5 > maxClients 4: non-player branch
	want := EntityBaseline{}
	if bl != want {
		t.Errorf("missing fields: got %+v want %+v", bl, want)
	}
	if hasModel {
		t.Errorf("hasModel: got true want false (no v.modelindex, no v.model)")
	}
}

// TestComposeBaselineFromEdict_PlayerSlot_UsesPlayerModelName forces
// modelindex=ModelIndex(PlayerModelName) on entNum in [1, maxClients]
// and ignores v.model. Verifies the upstream's hard-coded
// "progs/player.mdl" override.
func TestComposeBaselineFromEdict_PlayerSlot_UsesPlayerModelName(t *testing.T) {
	p, e := emptyProgsAndEdict(t)
	precache := []string{"maps/start.bsp", PlayerModelName, "progs/eyes.mdl"} // PlayerModelName at slot 1
	bl, hasModel := ComposeBaselineFromEdict(p, e, 1, 4, precache)            // entNum 1 == player slot 1
	if bl.ColorMap != 1 {
		t.Errorf("ColorMap: got %d want 1 (= entNum for player slot)", bl.ColorMap)
	}
	if bl.ModelIndex != 1 {
		t.Errorf("ModelIndex: got %d want 1 (= PlayerModelName precache slot)", bl.ModelIndex)
	}
	if !hasModel {
		t.Error("hasModel: got false want true (players always have model intent)")
	}
}

// Player-slot branch with PlayerModelName missing from the precache
// degrades to slot 0 (no model) -- the upstream Host_Errors, the Go
// port treats the missing precache as "no model" so the baseline still
// flows (subsequent client-side rendering will silently skip it).
func TestComposeBaselineFromEdict_PlayerSlot_MissingPlayerModel_FallsBackToZero(t *testing.T) {
	p, e := emptyProgsAndEdict(t)
	precache := []string{"maps/start.bsp", "progs/eyes.mdl"} // PlayerModelName NOT in precache
	bl, hasModel := ComposeBaselineFromEdict(p, e, 1, 4, precache)
	if bl.ModelIndex != 0 {
		t.Errorf("ModelIndex: got %d want 0 (PlayerModelName missing -> slot 0)", bl.ModelIndex)
	}
	if bl.ColorMap != 1 {
		t.Errorf("ColorMap: got %d want 1", bl.ColorMap)
	}
	if !hasModel {
		t.Error("hasModel: got false want true (players always flag model intent)")
	}
}

// Non-player slot branch with a v.model field that resolves to a
// precache slot.
func TestComposeBaselineFromEdict_NonPlayer_ResolvesModelFromVModel(t *testing.T) {
	p, e := progsWithModelField(t, "progs/dog.mdl")
	precache := []string{"maps/start.bsp", "progs/dog.mdl", "progs/zombie.mdl"}
	bl, hasModel := ComposeBaselineFromEdict(p, e, 10, 4, precache) // entNum 10 > maxClients 4: monster branch
	if bl.ColorMap != 0 {
		t.Errorf("ColorMap: got %d want 0 (non-player branch)", bl.ColorMap)
	}
	if bl.ModelIndex != 1 {
		t.Errorf("ModelIndex: got %d want 1 (progs/dog.mdl precache slot)", bl.ModelIndex)
	}
	if !hasModel {
		t.Error("hasModel: got false want true (v.model resolved to precache slot)")
	}
}

// Non-player slot with a v.model that is NOT in the precache -- silent
// degrade to ModelIndex=0, but hasModel still flags true so the
// bring-up emits a baseline (the QC setmodel wiring isn't done yet,
// so unprecached monster models are the common case).
func TestComposeBaselineFromEdict_NonPlayer_UnprecachedModel_FlagsHasModel(t *testing.T) {
	p, e := progsWithModelField(t, "progs/never_precached.mdl")
	precache := []string{"maps/start.bsp", "progs/dog.mdl"}
	bl, hasModel := ComposeBaselineFromEdict(p, e, 10, 4, precache)
	if bl.ModelIndex != 0 {
		t.Errorf("ModelIndex: got %d want 0 (unprecached model)", bl.ModelIndex)
	}
	if !hasModel {
		t.Error("hasModel: got false want true (non-empty v.model = intent, even unprecached)")
	}
}

// Non-player slot whose v.modelindex is non-zero (the QC setmodel path):
// the composer prefers it over the v.model string lookup.
func TestComposeBaselineFromEdict_NonPlayer_UsesVModelIndex(t *testing.T) {
	p, e := progsWithModelIndexField(t, 17)
	bl, hasModel := ComposeBaselineFromEdict(p, e, 10, 4, []string{"maps/start.bsp"})
	if bl.ModelIndex != 17 {
		t.Errorf("ModelIndex: got %d want 17 (v.modelindex direct)", bl.ModelIndex)
	}
	if !hasModel {
		t.Error("hasModel: got false want true (v.modelindex non-zero)")
	}
}

// Origin / angles / frame / skin all populated via the EntVars reads.
func TestComposeBaselineFromEdict_AllFieldsPopulated(t *testing.T) {
	p, e := progsWithFullEntity(t)
	bl, _ := ComposeBaselineFromEdict(p, e, 7, 4, []string{"maps/start.bsp"}) // non-player
	if bl.Origin != [3]float32{1, 2, 3} {
		t.Errorf("Origin: got %v want [1 2 3]", bl.Origin)
	}
	if bl.Angles != [3]float32{0, 90, 0} {
		t.Errorf("Angles: got %v want [0 90 0]", bl.Angles)
	}
	if bl.Frame != 5 {
		t.Errorf("Frame: got %d want 5", bl.Frame)
	}
	if bl.SkinNum != 2 {
		t.Errorf("SkinNum: got %d want 2", bl.SkinNum)
	}
}

// --- SendBaselines --------------------------------------------------

func TestSendBaselines_NilServer(t *testing.T) {
	var s *Server
	_, err := s.SendBaselines(NewClient(), nil, 1)
	if !errors.Is(err, ErrSendBaselinesNilServer) {
		t.Errorf("nil server: got %v want ErrSendBaselinesNilServer", err)
	}
}

func TestSendBaselines_NilClient(t *testing.T) {
	s := NewServer()
	stat, err := s.SendBaselines(nil, nil, 1)
	if err != nil {
		t.Errorf("nil client: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("nil client: got Emitted=%d want 0", stat.Emitted)
	}
}

func TestSendBaselines_InactiveClient(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = false
	stat, err := s.SendBaselines(c, nil, 1)
	if err != nil {
		t.Errorf("inactive client: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("inactive client: got Emitted=%d want 0", stat.Emitted)
	}
}

func TestSendBaselines_NilMessageBuf(t *testing.T) {
	s := NewServer()
	c := NewClient()
	c.Active = true
	c.Message = nil
	stat, err := s.SendBaselines(c, nil, 1)
	if err != nil {
		t.Errorf("nil message: got err %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("nil message: got Emitted=%d want 0", stat.Emitted)
	}
}

// Empty edict pool -> walk runs zero iterations -> Emitted=0 + nil err.
func TestSendBaselines_EmptyEdictPool(t *testing.T) {
	s := NewServer()
	s.Edicts = []*progs.Edict{}
	s.NumEdicts = 0
	c := NewClient()
	c.Active = true
	stat, err := s.SendBaselines(c, nil, 1)
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

// Mixed pool: worldspawn (slot 0) + 1 player + 1 monster + 1 free + 1
// no-model trigger. With the no-model skip disabled for bring-up the
// trigger ALSO emits; only the Free slot is skipped.
func TestSendBaselines_HappyPath_WalksAndSkipsCorrectly(t *testing.T) {
	s, p := serverWithMixedEdicts(t)
	c := NewClient()
	c.Active = true
	c.SendSignon = true

	stat, err := s.SendBaselines(c, p, 1) // maxClients=1: slot 1 is the player
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}

	// Walked 5 slots: world(emit) + player(emit) + monster(emit) +
	// free(skip-free) + no-model trigger(emit; no-model skip disabled
	// for bring-up) = 4 emitted, 1 skipped-free, 0 skipped-no-model.
	if stat.Emitted != 4 {
		t.Errorf("Emitted: got %d want 4", stat.Emitted)
	}
	if stat.SkippedFree != 1 {
		t.Errorf("SkippedFree: got %d want 1", stat.SkippedFree)
	}
	if stat.SkippedNoModel != 0 {
		t.Errorf("SkippedNoModel: got %d want 0 (skip disabled for bring-up)", stat.SkippedNoModel)
	}
	if len(stat.PerSlotSkipped) != 5 {
		t.Errorf("PerSlotSkipped len: got %d want 5", len(stat.PerSlotSkipped))
	}
	if stat.PerSlotSkipped[0] != BaselineSkipNone ||
		stat.PerSlotSkipped[1] != BaselineSkipNone ||
		stat.PerSlotSkipped[2] != BaselineSkipNone ||
		stat.PerSlotSkipped[3] != BaselineSkipFree ||
		stat.PerSlotSkipped[4] != BaselineSkipNone {
		t.Errorf("PerSlotSkipped: got %v", stat.PerSlotSkipped)
	}
	if len(stat.PerSlotEntNums) != 4 ||
		stat.PerSlotEntNums[0] != 0 ||
		stat.PerSlotEntNums[1] != 1 ||
		stat.PerSlotEntNums[2] != 2 ||
		stat.PerSlotEntNums[3] != 4 {
		t.Errorf("PerSlotEntNums: got %v want [0 1 2 4]", stat.PerSlotEntNums)
	}

	// Walk the queued bytes: should be 4 svc_spawnbaseline messages.
	r := msg.NewReader(c.Message.Bytes())
	for i, wantEnt := range []int{0, 1, 2, 4} {
		if op := r.ReadU8(); op != protocol.SvcSpawnBaseline {
			t.Errorf("emit[%d] opcode: got %d want SvcSpawnBaseline (%d)", i, op, protocol.SvcSpawnBaseline)
		}
		if ent := r.ReadShort(); ent != wantEnt {
			t.Errorf("emit[%d] entNum: got %d want %d", i, ent, wantEnt)
		}
		_ = r.ReadU8() // modelIndex
		_ = r.ReadU8() // frame
		_ = r.ReadU8() // colormap
		_ = r.ReadU8() // skin
		for axis := 0; axis < 3; axis++ {
			_ = r.ReadCoord()
			_ = r.ReadAngle()
		}
	}
}

// Nil edict slot (slot allocated but pointer is nil) classifies as
// BaselineSkipFree -- matches the "structurally absent = free" rule.
func TestSendBaselines_NilEdictSlot_SkipsAsFree(t *testing.T) {
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	s.Edicts = []*progs.Edict{nil, nil}
	s.NumEdicts = 2
	c := NewClient()
	c.Active = true

	stat, err := s.SendBaselines(c, nil, 1)
	if err != nil {
		t.Fatalf("err: got %v want nil", err)
	}
	if stat.Emitted != 0 {
		t.Errorf("Emitted: got %d want 0", stat.Emitted)
	}
	if stat.SkippedFree != 2 {
		t.Errorf("SkippedFree: got %d want 2", stat.SkippedFree)
	}
}

// EncodeBaseline overflow propagates verbatim. Force it by capping
// client.Message to zero bytes so even the first opcode write fails.
func TestSendBaselines_PropagatesEncoderError(t *testing.T) {
	s, p := serverWithMixedEdicts(t)
	c := NewClient()
	c.Active = true
	c.Message = sizebuf.New(make([]byte, 0)) // zero capacity -> overflow on first byte

	stat, err := s.SendBaselines(c, p, 1)
	if err == nil {
		t.Fatal("expected encoder error, got nil")
	}
	if stat.Emitted != 0 {
		t.Errorf("Emitted: got %d want 0 (failure on first emit)", stat.Emitted)
	}
}

// --- helpers --------------------------------------------------------

// emptyProgsAndEdict returns a Progs with zero field defs + a fresh
// Edict, so every EntVars read returns ErrFieldNotFound.
func emptyProgsAndEdict(t *testing.T) (*progs.Progs, *progs.Edict) {
	t.Helper()
	p := &progs.Progs{}
	e := &progs.Edict{}
	return p, e
}

// progsWithFullEntity returns a Progs whose field-def table declares
// every entvar SV_CreateBaseline reads (origin, angles, frame, skin,
// model) and an Edict whose field block holds: origin=(1,2,3),
// angles=(0,90,0), frame=5, skin=2, model="<unused-non-player-test>".
func progsWithFullEntity(t *testing.T) (*progs.Progs, *progs.Edict) {
	t.Helper()
	// Lay out a 12-slot field block: 3 vec3s (origin, angles) take 3
	// slots each + 3 floats (frame, skin, model) take 1 each = 3+3+1+1+1 = 9.
	defs := []progs.Def{
		{Type: uint16(progs.EvVector), Ofs: 0, SName: 0},  // origin@0..2
		{Type: uint16(progs.EvVector), Ofs: 3, SName: 7},  // angles@3..5
		{Type: uint16(progs.EvFloat), Ofs: 6, SName: 14},  // frame@6
		{Type: uint16(progs.EvFloat), Ofs: 7, SName: 20},  // skin@7
		{Type: uint16(progs.EvString), Ofs: 8, SName: 25}, // model@8
	}
	// String heap: \0 "origin\0" "angles\0" "frame\0" "skin\0" "model\0".
	strHeap := []byte{0}
	strHeap = append(strHeap, []byte("origin\x00")...) // start at 1, "origin" ends at 7
	strHeap = append(strHeap, []byte("angles\x00")...) // start at 8
	strHeap = append(strHeap, []byte("frame\x00")...)  // start at 15
	strHeap = append(strHeap, []byte("skin\x00")...)   // start at 21
	strHeap = append(strHeap, []byte("model\x00")...)  // start at 26
	// Adjust SName offsets to actual heap positions:
	defs[0].SName = 1
	defs[1].SName = 8
	defs[2].SName = 15
	defs[3].SName = 21
	defs[4].SName = 26

	p := &progs.Progs{
		FieldDefs: defs,
		Strings:   strHeap,
	}

	// Per-edict field block (9 slots, each 4 bytes).
	e := &progs.Edict{Fields: make([]byte, 9*4)}
	// origin = (1,2,3)
	must(t, e.FieldSetVector(0, [3]float32{1, 2, 3}))
	must(t, e.FieldSetVector(3, [3]float32{0, 90, 0}))
	must(t, e.FieldSetFloat(6, 5))
	must(t, e.FieldSetFloat(7, 2))
	// model field holds the string_t offset; for this test the value
	// is the heap-offset of "model" itself (28) which is not a model
	// name -- the composer will look it up, get ErrNotPrecached, and
	// degrade to slot 0. The test only inspects origin/angles/frame/
	// skin so model lookup is intentionally orthogonal.
	must(t, e.FieldSetInt(8, 28))
	return p, e
}

// progsWithModelIndexField returns a Progs whose v.modelindex is the
// supplied float value (used to drive the "QC setmodel already ran"
// branch).
func progsWithModelIndexField(t *testing.T, idx float32) (*progs.Progs, *progs.Edict) {
	t.Helper()
	heap := []byte{0}
	heap = append(heap, []byte("modelindex\x00")...) // "modelindex" at offset 1
	defs := []progs.Def{
		{Type: uint16(progs.EvFloat), Ofs: 0, SName: 1}, // modelindex@0
	}
	p := &progs.Progs{
		FieldDefs: defs,
		Strings:   heap,
	}
	e := &progs.Edict{Fields: make([]byte, 4)}
	must(t, e.FieldSetFloat(0, idx))
	return p, e
}

// progsWithModelField returns a Progs whose v.model resolves to the
// supplied modelName string (used to drive the non-player branch).
func progsWithModelField(t *testing.T, modelName string) (*progs.Progs, *progs.Edict) {
	t.Helper()
	// String heap: \0 "model\0" modelName\0
	heap := []byte{0}
	heap = append(heap, []byte("model\x00")...) // "model" at offset 1
	modelOff := int32(len(heap))
	heap = append(heap, []byte(modelName)...)
	heap = append(heap, 0)

	defs := []progs.Def{
		{Type: uint16(progs.EvString), Ofs: 0, SName: 1}, // model@0
	}
	p := &progs.Progs{
		FieldDefs: defs,
		Strings:   heap,
	}
	e := &progs.Edict{Fields: make([]byte, 4)}
	must(t, e.FieldSetInt(0, modelOff))
	return p, e
}

// serverWithMixedEdicts builds a Server + Progs where Edicts is:
//
//	slot 0: worldspawn (model="" -> non-player branch but world is
//	        slot 0 so it always emits via the bsp-loader having set
//	        modelindex earlier; the composer's modelindex=0 still
//	        triggers the noModel guard ONLY when slot > maxClients,
//	        so slot 0 is always emitted)
//	slot 1: player (entNum 1 <= maxClients 1, gets PlayerModelName)
//	slot 2: live monster (entNum 2 > maxClients, v.model resolves -> emit)
//	slot 3: free edict (skip)
//	slot 4: no-model trigger (entNum 4 > maxClients, modelindex 0 -> skip)
//
// maxClients = 1; precache = {"maps/start.bsp", PlayerModelName, "progs/dog.mdl"}.
func serverWithMixedEdicts(t *testing.T) (*Server, *progs.Progs) {
	t.Helper()
	s := NewServer()
	s.Protocol = protocol.VersionNQ
	s.ModelPrecache = []string{"maps/start.bsp", PlayerModelName, "progs/dog.mdl"}

	// String heap shared by all edicts: "\0" "model\0" "progs/dog.mdl\0".
	heap := []byte{0}
	heap = append(heap, []byte("model\x00")...) // "model" at offset 1
	dogOff := int32(len(heap))
	heap = append(heap, []byte("progs/dog.mdl\x00")...)

	p := &progs.Progs{
		FieldDefs: []progs.Def{{Type: uint16(progs.EvString), Ofs: 0, SName: 1}},
		Strings:   heap,
	}

	// Slot 0: worldspawn -- empty model field (slot 0 in precache lookup).
	world := &progs.Edict{Fields: make([]byte, 4)}

	// Slot 1: player -- model field unused (player branch overrides).
	player := &progs.Edict{Fields: make([]byte, 4)}

	// Slot 2: live monster -- model resolves to "progs/dog.mdl" -> slot 2.
	monster := &progs.Edict{Fields: make([]byte, 4)}
	must(t, monster.FieldSetInt(0, dogOff))

	// Slot 3: free edict.
	freeEd := &progs.Edict{Fields: make([]byte, 4), Free: true}

	// Slot 4: no-model trigger -- model field empty -> slot 0 in precache
	// -> SkippedNoModel because entNum 4 > maxClients 1.
	trigger := &progs.Edict{Fields: make([]byte, 4)}

	s.Edicts = []*progs.Edict{world, player, monster, freeEd, trigger}
	s.NumEdicts = 5

	return s, p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
}
