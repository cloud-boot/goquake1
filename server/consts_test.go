// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import "testing"

// Every constant defined in consts.go is a single numeric value
// that MUST match the tyrquake C header verbatim, because both the
// engine and the QuakeC code observe these enums on the same edict
// fields. Mutating any of these silently changes game behaviour
// (entities classify wrong, physics paths fire on the wrong entities,
// rendering goes off-spec). This file is a drift-detection bank.

func TestMoveType_TyrquakeValues(t *testing.T) {
	cases := []struct {
		name string
		got  MoveType
		want int32
	}{
		{"None", MoveTypeNone, 0},
		{"AngleNoClip", MoveTypeAngleNoClip, 1},
		{"AngleClip", MoveTypeAngleClip, 2},
		{"Walk", MoveTypeWalk, 3},
		{"Step", MoveTypeStep, 4},
		{"Fly", MoveTypeFly, 5},
		{"Toss", MoveTypeToss, 6},
		{"Push", MoveTypePush, 7},
		{"NoClip", MoveTypeNoClip, 8},
		{"FlyMissile", MoveTypeFlyMissile, 9},
		{"Bounce", MoveTypeBounce, 10},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("MoveType%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

func TestSolid_TyrquakeValues(t *testing.T) {
	cases := []struct {
		name string
		got  Solid
		want int32
	}{
		{"Not", SolidNot, 0},
		{"Trigger", SolidTrigger, 1},
		{"BBox", SolidBBox, 2},
		{"SlideBox", SolidSlideBox, 3},
		{"BSP", SolidBSP, 4},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("Solid%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

func TestDeadFlag_TyrquakeValues(t *testing.T) {
	cases := []struct {
		name string
		got  DeadFlag
		want int32
	}{
		{"No", DeadNo, 0},
		{"Dying", DeadDying, 1},
		{"Dead", DeadDead, 2},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("Dead%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

func TestEntityFlag_TyrquakeValues(t *testing.T) {
	// FL_* are bit flags; each MUST be a distinct power of 2 in
	// the order tyrquake defines them.
	cases := []struct {
		name string
		got  EntityFlag
		want int32
	}{
		{"Fly", FlagFly, 1},
		{"Swim", FlagSwim, 2},
		{"Conveyor", FlagConveyor, 4},
		{"Client", FlagClient, 8},
		{"InWater", FlagInWater, 16},
		{"Monster", FlagMonster, 32},
		{"GodMode", FlagGodMode, 64},
		{"NoTarget", FlagNoTarget, 128},
		{"Item", FlagItem, 256},
		{"OnGround", FlagOnGround, 512},
		{"PartialGround", FlagPartialGround, 1024},
		{"WaterJump", FlagWaterJump, 2048},
		{"JumpReleased", FlagJumpReleased, 4096},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("Flag%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

// Spot-check FL_CONVEYOR's gap (no FL_3): the upstream skips the
// value 3 deliberately (it WAS FL_OBSERVER in older builds but is
// reserved). The bit AFTER FL_SWIM (=2) is FL_CONVEYOR (=4), not
// (=3). Re-asserting because a naive iota-based port would have
// shifted the rest of the flags by 1.
func TestEntityFlag_GapAtThree(t *testing.T) {
	if FlagSwim<<1 != FlagConveyor {
		t.Errorf("FL_CONVEYOR should be the next power of 2 after FL_SWIM: %d vs %d", FlagConveyor, FlagSwim<<1)
	}
}

func TestEffectFlag_TyrquakeValues(t *testing.T) {
	cases := []struct {
		name string
		got  EffectFlag
		want int32
	}{
		{"BrightField", EffectBrightField, 1},
		{"MuzzleFlash", EffectMuzzleFlash, 2},
		{"BrightLight", EffectBrightLight, 4},
		{"DimLight", EffectDimLight, 8},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("Effect%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

// --- limits.go -----------------------------------------------------------

func TestAllocationLimits_TyrquakeValues(t *testing.T) {
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"MaxEdicts", MaxEdicts, 8192},
		{"MaxLightStyles", MaxLightStyles, 64},
		{"MaxModels", MaxModels, 2048},
		{"MaxSounds", MaxSounds, 1024},
		{"MaxDatagram", MaxDatagram, 1 << 18},
		{"MaxClients", MaxClients, 16},
		{"AreaDepth", AreaDepth, 4},
		{"AreaNodes", AreaNodes, 32},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s drift: got %d want %d (tyrquake)", c.name, c.got, c.want)
		}
	}
}

// The area-tree node count must hold a balanced quadtree of
// AreaDepth levels. tyrquake's SV_CreateAreaNode recurses to depth
// AREA_DEPTH(=4) building 1 + 2 + 4 + 8 + 16 = 31 nodes, so
// AREA_NODES(=32) is a safe upper bound. If either value drifts,
// the area allocator's static array under-/over-sizes.
func TestAreaTree_DepthFitsNodeBudget(t *testing.T) {
	// A balanced tree with this depth needs 2^(depth+1) - 1 = 31
	// nodes for depth=4; tyrquake rounds up to 32.
	required := (1 << (AreaDepth + 1)) - 1
	if AreaNodes < required {
		t.Errorf("AreaNodes (%d) too small for AreaDepth (%d) -- needs at least %d", AreaNodes, AreaDepth, required)
	}
}
