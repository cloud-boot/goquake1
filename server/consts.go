// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

// MoveType is the value of an edict's entvars_t.movetype field. It
// selects which branch of SV_Physics runs for the entity per tick.
// tyrquake: MOVETYPE_* in NQ/server.h.
type MoveType int32

const (
	MoveTypeNone        MoveType = 0 // never moves
	MoveTypeAngleNoClip MoveType = 1 // rotation only, no world clip
	MoveTypeAngleClip   MoveType = 2 // rotation only, world clip
	MoveTypeWalk        MoveType = 3 // gravity
	MoveTypeStep        MoveType = 4 // gravity, special edge handling
	MoveTypeFly         MoveType = 5
	MoveTypeToss        MoveType = 6 // gravity
	MoveTypePush        MoveType = 7 // no clip to world, push and crush
	MoveTypeNoClip      MoveType = 8
	MoveTypeFlyMissile  MoveType = 9 // extra size to monsters
	MoveTypeBounce      MoveType = 10
)

// MoveMode is the trace-mode enum SV_TraceMove takes as its type
// parameter. Distinct from [MoveType] (the entvars.movetype value).
// The C upstream confusingly reuses the name movetype_t for both
// enums; the Go port splits them. tyrquake: the MOVE_NORMAL /
// MOVE_NOMONSTERS / MOVE_MISSILE enum in common/include/world.h.
type MoveMode int32

const (
	MoveModeNormal     MoveMode = 0 // standard swept trace
	MoveModeNoMonsters MoveMode = 1 // line of sight; skip non-BSP entities
	MoveModeMissile    MoveMode = 2 // monster bounds widened to a 30-unit cube
)

// Solid is the value of an edict's entvars_t.solid field. It
// selects how the entity participates in the SV_TraceMove broadphase
// and which collision hull (BSP vs box) SV_HullForEntity picks.
// tyrquake: SOLID_* in NQ/server.h.
type Solid int32

const (
	SolidNot      Solid = 0 // no interaction with other objects
	SolidTrigger  Solid = 1 // touch on edge, but not blocking
	SolidBBox     Solid = 2 // touch on edge, block
	SolidSlideBox Solid = 3 // touch on edge, but not an onground
	SolidBSP      Solid = 4 // bsp clip, touch on edge, block
)

// DeadFlag is the value of entvars_t.deadflag. Drives the
// not-yet-respawned + drop-corpse paths in SV_Physics.
// tyrquake: DEAD_* in NQ/server.h.
type DeadFlag int32

const (
	DeadNo    DeadFlag = 0
	DeadDying DeadFlag = 1
	DeadDead  DeadFlag = 2
)

// EntityFlag bits in entvars_t.flags. tyrquake: FL_* in NQ/server.h.
// Set/cleared by both engine (e.g. FL_INWATER, FL_ONGROUND) and
// QuakeC (e.g. FL_GODMODE, FL_NOTARGET).
type EntityFlag int32

const (
	FlagFly           EntityFlag = 1
	FlagSwim          EntityFlag = 2
	FlagConveyor      EntityFlag = 4
	FlagClient        EntityFlag = 8
	FlagInWater       EntityFlag = 16
	FlagMonster       EntityFlag = 32
	FlagGodMode       EntityFlag = 64
	FlagNoTarget      EntityFlag = 128
	FlagItem          EntityFlag = 256
	FlagOnGround      EntityFlag = 512
	FlagPartialGround EntityFlag = 1024 // not all corners are valid
	FlagWaterJump     EntityFlag = 2048 // player jumping out of water
	FlagJumpReleased  EntityFlag = 4096 // for jump debouncing
)

// EffectFlag bits in entvars_t.effects. Drives the corresponding
// renderer + sound paths client-side.
// tyrquake: EF_* in NQ/server.h.
type EffectFlag int32

const (
	EffectBrightField EffectFlag = 1
	EffectMuzzleFlash EffectFlag = 2
	EffectBrightLight EffectFlag = 4
	EffectDimLight    EffectFlag = 8
)
