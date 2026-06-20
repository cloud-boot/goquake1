// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// ClientDataState is the per-tick player-state snapshot the server
// writes to each client's own message buffer in the svc_clientdata
// message. Caller composes it from the player edict's entvars +
// resolved precache indices (WeaponModel = SV_ModelIndex(player.weaponmodel)).
//
// tyrquake: the local variables of SV_WriteClientdataToMessage in
// NQ/sv_main.c lines 735-923. The C upstream reads everything off
// the edict; the Go port takes a plain struct so callers compose
// the snapshot however (entity-component, savegame replay, fixture-
// driven tests, ...).
type ClientDataState struct {
	// View offset along player's +Z axis (the eye height above origin).
	// Default 22; non-default sets SU_VIEWHEIGHT in the wire bitmask.
	ViewHeightOffset float32

	// Ideal pitch the auto-look-at-floor code computed.
	IdealPitch float32

	// Per-axis punchangle (recoil/impact angular kick).
	PunchAngle [3]float32

	// Per-axis player velocity (sent in 1/16 quantised units).
	Velocity [3]float32

	// Inventory + status bitfield (weapons owned, keys, sigils, ...).
	// In the C upstream this is items |= items2<<23 OR
	// serverflags<<28; caller is responsible for composing the
	// final 32-bit value.
	Items int32

	// Engine-side movement state. The C upstream sets
	// SU_ONGROUND / SU_INWATER bits in the SU bitmask when these
	// are true; nothing is OR'd into Items by the encoder. (The
	// upstream code at NQ/sv_main.c:799-803 is bit-set-only.)
	OnGround bool
	InWater  bool

	// Current weapon's animation frame.
	WeaponFrame int

	// Armor value (0..200).
	ArmorValue int

	// Precache-resolved current weapon model index (the result of
	// SV_ModelIndex on the C side). Zero suppresses the SU_WEAPON
	// bit; non-zero emits the model-index byte.
	WeaponModel int

	// Player HP (signed; can be negative on death).
	Health int

	// Current ammo for the active weapon.
	CurrentAmmo int

	// Per-type ammo: shells / nails / rockets / cells.
	Ammo [4]int

	// Active weapon byte. The C upstream emits either
	// player->v.weapon as-is (standard_quake) or the bit-index of
	// the lowest set bit (mission packs); the Go port writes the
	// caller-supplied byte verbatim so either policy is selectable
	// at the call site.
	ActiveWeapon int
}

// EncodeClientData writes one svc_clientdata message into buf.
// The wire bitmask is computed from non-default fields:
//
//   - ViewHeightOffset != DefaultViewHeight  -> SUViewHeight
//   - IdealPitch != 0                        -> SUIdealPitch
//   - PunchAngle[i] != 0                     -> SUPunch1 << i
//   - Velocity[i]   != 0                     -> SUVelocity1 << i
//   - WeaponFrame != 0                       -> SUWeaponFrame
//   - ArmorValue  != 0                       -> SUArmor
//   - WeaponModel != 0                       -> SUWeapon
//   - InWater                                -> SUInWater
//   - OnGround                               -> SUOnGround
//
// SUItems is always set: the items long is unconditionally written
// in the C upstream (the "[always sent]" comment on NQ/sv_main.c:873).
//
// Wire shape (variable length):
//
//	byte    svc_clientdata
//	short   bits
//	[char   view_offset_z]       iff SUViewHeight
//	[char   ideal_pitch]         iff SUIdealPitch
//	[char   punchangle[i]]       per axis, iff SUPunch1<<i
//	[char   velocity[i]/16]      per axis, iff SUVelocity1<<i
//	long    items
//	[byte   weaponframe]         iff SUWeaponFrame
//	[byte   armorvalue]          iff SUArmor
//	[byte   weaponmodel]         iff SUWeapon
//	short   health
//	byte    currentammo
//	byte    ammo[0..3]           4 bytes (shells/nails/rockets/cells)
//	byte    activeweapon
//
// FITZ-protocol extensions (SUFitzWeapon2 / SUFitzArmor2 / SUFitzAmmo2
// / shells2 / nails2 / rockets2 / cells2 / weaponframe2 / weaponalpha
// and the SUFitzExtend1/Extend2 escapes that gate them) are NOT
// emitted by this version -- this encoder is scoped to the vanilla
// NQ message. A FITZ variant can wrap or extend this one later.
//
// Returns:
//   - ErrNilBuf                       if buf is nil
//   - propagated msg.Write* errors    on overflow
func EncodeClientData(buf *sizebuf.Buffer, state ClientDataState) error {
	if buf == nil {
		return ErrNilBuf
	}

	bits := 0

	if state.ViewHeightOffset != protocol.DefaultViewHeight {
		bits |= protocol.SUViewHeight
	}
	if state.IdealPitch != 0 {
		bits |= protocol.SUIdealPitch
	}
	for i := 0; i < 3; i++ {
		if state.PunchAngle[i] != 0 {
			bits |= protocol.SUPunch1 << i
		}
		if state.Velocity[i] != 0 {
			bits |= protocol.SUVelocity1 << i
		}
	}
	// SUItems is always set: the items long is always written.
	bits |= protocol.SUItems
	if state.OnGround {
		bits |= protocol.SUOnGround
	}
	if state.InWater {
		bits |= protocol.SUInWater
	}
	if state.WeaponFrame != 0 {
		bits |= protocol.SUWeaponFrame
	}
	if state.ArmorValue != 0 {
		bits |= protocol.SUArmor
	}
	if state.WeaponModel != 0 {
		bits |= protocol.SUWeapon
	}

	if err := msg.WriteByte(buf, protocol.SvcClientData); err != nil {
		return err
	}
	if err := msg.WriteShort(buf, bits); err != nil {
		return err
	}

	if bits&protocol.SUViewHeight != 0 {
		if err := msg.WriteChar(buf, int(state.ViewHeightOffset)); err != nil {
			return err
		}
	}
	if bits&protocol.SUIdealPitch != 0 {
		if err := msg.WriteChar(buf, int(state.IdealPitch)); err != nil {
			return err
		}
	}
	for i := 0; i < 3; i++ {
		if bits&(protocol.SUPunch1<<i) != 0 {
			if err := msg.WriteChar(buf, int(state.PunchAngle[i])); err != nil {
				return err
			}
		}
		if bits&(protocol.SUVelocity1<<i) != 0 {
			if err := msg.WriteChar(buf, int(state.Velocity[i]/16)); err != nil {
				return err
			}
		}
	}

	if err := msg.WriteLong(buf, state.Items); err != nil {
		return err
	}

	if bits&protocol.SUWeaponFrame != 0 {
		if err := msg.WriteByte(buf, state.WeaponFrame); err != nil {
			return err
		}
	}
	if bits&protocol.SUArmor != 0 {
		if err := msg.WriteByte(buf, state.ArmorValue); err != nil {
			return err
		}
	}
	if bits&protocol.SUWeapon != 0 {
		if err := msg.WriteByte(buf, state.WeaponModel); err != nil {
			return err
		}
	}

	if err := msg.WriteShort(buf, state.Health); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, state.CurrentAmmo); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		if err := msg.WriteByte(buf, state.Ammo[i]); err != nil {
			return err
		}
	}
	return msg.WriteByte(buf, state.ActiveWeapon)
}
