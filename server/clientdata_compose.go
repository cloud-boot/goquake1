// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
)

// ComposeClientDataFromEdict reads the per-tic player snapshot off
// the player edict's entvars and returns it as a [ClientDataState]
// the [EncodeClientData] encoder can consume.
//
// tyrquake: the variable hoisting at the top of
// SV_WriteClientdataToMessage in NQ/sv_main.c lines 735-806 -- the
// reads of ent->v.view_ofs / ent->v.punchangle / ent->v.velocity /
// ent->v.items / ent->v.weapon / ent->v.weaponframe / ent->v.armor*
// / ent->v.currentammo / ent->v.ammo_shells .. ammo_cells /
// ent->v.health / (ent->v.flags & FL_ONGROUND) / (ent->v.waterlevel >= 2).
//
// Tolerant of missing fields (test progs that strip definitions):
// every read goes through [progs.EntVars] which returns
// [progs.ErrFieldNotFound] when the field isn't declared; the
// helper silently substitutes the field's QC zero default.
//
// ViewHeightOffset specifically defaults to [protocol.DefaultViewHeight]
// (=22) when the view_ofs[2] axis can't be read, so the
// SU_VIEWHEIGHT bit stays cleared on the wire (matching the C
// upstream's "only send if non-default" behaviour).
//
// Returns the zero [ClientDataState] (with ViewHeightOffset =
// DefaultViewHeight) when p == nil or edict == nil -- callers can
// treat that as "no clientdata to send this tic".
func ComposeClientDataFromEdict(p *progs.Progs, edict *progs.Edict) ClientDataState {
	state := ClientDataState{ViewHeightOffset: protocol.DefaultViewHeight}
	if p == nil || edict == nil {
		return state
	}
	// NewEntVars only errors on nil inputs; the guards above ensure
	// both are non-nil, so its error is dropped (bsptrace-style).
	v, _ := progs.NewEntVars(p, edict)

	// view_ofs is a vector field (eye-position offset); the C
	// upstream sends the Z component as the view-height char.
	if vo, err := v.ReadVec3("view_ofs"); err == nil {
		state.ViewHeightOffset = vo[2]
	}
	if ip, err := v.ReadFloat("idealpitch"); err == nil {
		state.IdealPitch = ip
	}
	if pa, err := v.ReadVec3("punchangle"); err == nil {
		state.PunchAngle = pa
	}
	if vel, err := v.ReadVec3("velocity"); err == nil {
		state.Velocity = vel
	}
	if items, err := v.ReadFloat("items"); err == nil {
		state.Items = int32(items)
	}
	if flags, err := v.ReadFloat("flags"); err == nil {
		if EntityFlag(int32(flags))&FlagOnGround != 0 {
			state.OnGround = true
		}
	}
	if wl, err := v.ReadFloat("waterlevel"); err == nil {
		if int32(wl) >= 2 {
			state.InWater = true
		}
	}
	if wf, err := v.ReadFloat("weaponframe"); err == nil {
		state.WeaponFrame = int(wf)
	}
	if av, err := v.ReadFloat("armorvalue"); err == nil {
		state.ArmorValue = int(av)
	}
	if h, err := v.ReadFloat("health"); err == nil {
		state.Health = int(h)
	}
	if ca, err := v.ReadFloat("currentammo"); err == nil {
		state.CurrentAmmo = int(ca)
	}
	if s, err := v.ReadFloat("ammo_shells"); err == nil {
		state.Ammo[0] = int(s)
	}
	if n, err := v.ReadFloat("ammo_nails"); err == nil {
		state.Ammo[1] = int(n)
	}
	if r, err := v.ReadFloat("ammo_rockets"); err == nil {
		state.Ammo[2] = int(r)
	}
	if c, err := v.ReadFloat("ammo_cells"); err == nil {
		state.Ammo[3] = int(c)
	}
	if w, err := v.ReadFloat("weapon"); err == nil {
		state.ActiveWeapon = int(w)
	}
	return state
}
