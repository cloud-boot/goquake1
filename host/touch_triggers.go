// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"fmt"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/world"
)

// LastTriggerTouches is the cumulative count of QC `.touch` functions
// the most recent [Host.Frame] call dispatched via [Host.TouchTriggers].
// Reset at the top of every Frame. Exposed for bring-up instrumentation
// (the quake-tamago serial log prints the per-60-tic delta so a demo
// camera that grazes an item_shells / item_health / weapon_supershotgun
// shows a non-zero counter the moment pickup fires).
//
// Bumped only on dispatches that returned nil; errors fall into
// LastTouchErrors instead.
type touchCounters struct {
	dispatched int
	errors     int
	errMsgs    []string
}

// TouchTriggers walks the area tree for every SOLID_TRIGGER edict
// whose absbounds overlap mover's swept absbounds, then dispatches
// each trigger's QC `.touch` function with self=trigger, other=mover.
// tyrquake: SV_TouchLinks in common/sv_world.c (the per-link loop
// SV_LinkEdict fires after re-linking the moving entity into the
// area tree).
//
// Inputs:
//
//   - moverKey is the moving entity's [world.Key]; used to exclude
//     self-overlap from the trigger walk so a trigger doesn't fire
//     its own touch on itself (defensive: triggers are SOLID_TRIGGER
//     and movers are SOLID_BBOX/SLIDEBOX so the kinds normally don't
//     collide, but the test fixture proves the exclusion).
//   - moverSlot is the mover edict's slot in h.Server.Edicts. Used
//     to resolve mover's `origin / mins / maxs` for the absbound
//     overlap query AND to compute the QC entity-pointer hand-off
//     for the QC `other` global.
//
// Per-trigger skip rules (matches the C upstream's SV_TouchLinks
// guards):
//
//   - trigger.Free                              -> skip
//   - trigger.solid != SOLID_TRIGGER            -> skip (stale tree)
//   - trigger.touch == 0                        -> skip (no handler)
//   - trigger overlaps mover but bounds don't   -> already filtered
//     by AreaQuery; defensive re-check kept for the case the link
//     entry is stale (entity moved without a relink).
//
// A `.touch` dispatch error increments [Host.LastTouchErrors] and is
// recorded into LastTouchErrorMsgs (capped at 8 unique messages); the
// walk continues so one broken trigger doesn't take down the rest of
// the frame. Successful dispatches bump LastTriggerTouches.
//
// Pre-conditions silently handled (no error surfaced):
//
//   - h or h.World or h.Server nil      -> no-op
//   - moverSlot out of range or nil ent -> no-op
//   - no progs bound                    -> no-op (the per-trigger
//     entvars binding needs it, and without
//     a progs the QC dispatch has no
//     self / other globals anyway)
//   - mover edict missing origin/mins/maxs   -> no-op (cannot build
//     the area-query box)
//   - no `touch` field declared in progs    -> no-op (every trigger
//     skip per the rule above)
func (h *Host) TouchTriggers(moverSlot int, moverKey world.Key) {
	if h == nil || h.World == nil || h.Server == nil {
		return
	}
	if moverSlot < 0 || moverSlot >= len(h.Server.Edicts) {
		return
	}
	mover := h.Server.Edicts[moverSlot]
	if mover == nil || mover.Free {
		return
	}
	p := h.findProgs()
	if p == nil {
		return
	}
	touchDef := p.FindField("touch")
	if touchDef == nil {
		// No QC progs declares `.touch`? Then there's nothing to
		// dispatch; matches SV_TouchLinks's effective no-op when
		// the link entries' touch field is zero.
		return
	}

	mEV, _ := progs.NewEntVars(p, mover)
	mOrigin, err := mEV.ReadVec3("origin")
	if err != nil {
		return
	}
	mMins, err := mEV.ReadVec3("mins")
	if err != nil {
		return
	}
	mMaxs, err := mEV.ReadVec3("maxs")
	if err != nil {
		return
	}
	absmin := [3]float32{
		mOrigin[0] + mMins[0] - 1,
		mOrigin[1] + mMins[1] - 1,
		mOrigin[2] + mMins[2] - 1,
	}
	absmax := [3]float32{
		mOrigin[0] + mMaxs[0] + 1,
		mOrigin[1] + mMaxs[1] + 1,
		mOrigin[2] + mMaxs[2] + 1,
	}

	keys := h.World.AreaQuery(absmin, absmax, world.QueryTriggersOnly)
	for _, k := range keys {
		if k == moverKey {
			// Self-overlap guard: matches the C upstream's "if (touch
			// == ent) continue" inside SV_TouchLinks's per-link loop.
			continue
		}
		tslot := int(k)
		if tslot <= 0 || tslot >= len(h.Server.Edicts) {
			continue
		}
		trig := h.Server.Edicts[tslot]
		if trig == nil || trig.Free {
			continue
		}
		tEV, _ := progs.NewEntVars(p, trig)
		// Re-check solid: the link entry may be stale (entity moved
		// but caller forgot to relink) -- the upstream's SV_TouchLinks
		// trusts SOLID_TRIGGER classification at the time of link, so
		// the defensive check here is structurally a no-op for the
		// happy path but catches stale entries cleanly.
		solidF, err := tEV.ReadFloat("solid")
		if err != nil {
			continue
		}
		if server.Solid(int32(solidF)) != server.SolidTrigger {
			continue
		}
		funcID, err := tEV.ReadInt32("touch")
		if err != nil {
			continue
		}
		if funcID == 0 {
			continue
		}
		if err := h.dispatchTouch(trig, mover, funcID); err != nil {
			h.LastTouchErrors++
			msg := fmt.Sprintf("touch funcID=%d (trigger slot=%d, other slot=%d): %v",
				funcID, tslot, moverSlot, err)
			if len(h.LastTouchErrorMsgs) < 8 {
				seen := false
				for _, prev := range h.LastTouchErrorMsgs {
					if prev == msg {
						seen = true
						break
					}
				}
				if !seen {
					h.LastTouchErrorMsgs = append(h.LastTouchErrorMsgs, msg)
				}
			}
			continue
		}
		h.LastTriggerTouches++
	}
}

// dispatchTouch sets the QC self + other globals to the trigger +
// mover entity-pointers, then invokes the QC function indexed by
// funcID. Mirrors the self/other hand-off in [Host.thinkCaller],
// but with `other = mover` instead of `other = world`.
//
// The self->mover, other->trigger direction here matches the C
// upstream's SV_TouchLinks call:
//
//	pr_global_struct->self = EDICT_TO_PROG(touch);   // trigger
//	pr_global_struct->other = EDICT_TO_PROG(ent);    // mover
//	PR_ExecuteProgram(touch->v.touch);
//
// = the trigger's POV runs the .touch function ("self" = me, the
// trigger; "other" = whoever ran into me, the mover).
//
// Returns the VM.Run error verbatim; nil on success. A nil progs
// short-circuits the named-global writes but vm.Run still dispatches
// (matching thinkCaller's contract).
func (h *Host) dispatchTouch(trigger, mover *progs.Edict, funcID int32) error {
	p := h.findProgs()
	if p != nil {
		if def := p.FindGlobal("self"); def != nil {
			_ = h.VM.SetGlobalInt(int(def.Ofs), int32(h.entityPointer(trigger)))
		}
		if def := p.FindGlobal("other"); def != nil {
			_ = h.VM.SetGlobalInt(int(def.Ofs), int32(h.entityPointer(mover)))
		}
		if def := p.FindGlobal("time"); def != nil {
			_ = h.VM.SetGlobalFloat(int(def.Ofs), float32(h.Server.Time))
		}
	}
	return h.VM.Run(funcID)
}

// SetOrigin writes origin onto ent's entvars then relinks the edict's
// area-tree entry with the new bounds. tyrquake: PF_setorigin /
// SV_LinkEdict.
//
// This is the engine-side hook the [BuiltinSetOrigin] dispatcher
// invokes when QC calls setorigin(self, x). Two side-effects beyond
// the bare entvars write matter for item pickup:
//
//  1. After an item_*_touch hands its payload to the player, the QC
//     calls setorigin(self, '-8000 -8000 -8000') to park the trigger
//     well outside the playable bbox. Without relinking the area
//     tree, the trigger's stale entry stays at its old origin and
//     the player's next-tic TouchTriggers re-fires the same pickup
//     (= infinite ammo / loop). The relink moves the area-tree
//     entry to the new (far-away) bounds so the player's swept-box
//     query no longer returns it.
//
//  2. Likewise for setmodel("") followed by setorigin(...) (the
//     other half of the upstream item-remove pair): setmodel("")
//     unlinks via SolidKindSkip; setorigin then writes the new
//     origin so any subsequent setmodel() relinks at the right
//     place.
//
// Pre-conditions silently handled (matches the host's general no-op
// crash-safety contract for builtins):
//
//   - ent == nil OR Free                    -> no-op
//   - h.Server.Arena nil                    -> entvars write still
//     happens, area-tree relink
//     is skipped (no slot index)
//   - h.World == nil                        -> entvars write only
//   - origin / mins / maxs not declared     -> origin write skipped
//     per missing field, relink skipped on missing mins/maxs
//   - solid field absent                    -> kind defaults to
//     SolidKindSkip (= unlink)
func (h *Host) SetOrigin(ent *progs.Edict, origin [3]float32) {
	if h == nil || ent == nil || ent.Free {
		return
	}
	p := h.findProgs()
	if p == nil {
		return
	}
	ev, _ := progs.NewEntVars(p, ent)
	if err := ev.WriteVec3("origin", origin); err != nil {
		// The entvars write failed (typically: progs doesn't declare
		// origin). Skip the relink too -- without a written origin the
		// new absmin/absmax would lie about the entity's position.
		return
	}
	h.LinkEdict(ent)
}

// LinkEdict re-registers ent's area-tree entry using its current
// entvars (origin, mins, maxs, solid). tyrquake: SV_LinkEdict
// (the post-move re-link inside SV_Physics_Toss / SV_Push /
// SV_FlyMove / etc.).
//
// Resolves the area-tree [world.Key] from the edict's arena slot.
// If the arena isn't wired (test stubs) the relink degrades to a
// no-op -- without a stable Key the tree entry can't be addressed.
//
// kind dispatch (matches solidKindFromEntvars in quake-tamago):
//
//	SOLID_NOT      -> SolidKindSkip   (Unlink the entry; never
//	                                   re-link)
//	SOLID_TRIGGER  -> SolidKindTrigger
//	otherwise      -> SolidKindSolid
//
// Missing mins/maxs -> the relink is skipped (no bbox to register).
//
// Exposed as a method on Host so the quake-tamago BuiltinSetOrigin
// wiring + future BuiltinSetSize wiring both go through ONE path.
func (h *Host) LinkEdict(ent *progs.Edict) {
	if h == nil || h.World == nil || ent == nil || ent.Free {
		return
	}
	if h.Server == nil || h.Server.Arena == nil {
		return
	}
	p := h.findProgs()
	if p == nil {
		return
	}
	slot := h.Server.Arena.NumFor(ent)
	if slot < 0 {
		return
	}
	ev, _ := progs.NewEntVars(p, ent)
	origin, err := ev.ReadVec3("origin")
	if err != nil {
		return
	}
	mins, err := ev.ReadVec3("mins")
	if err != nil {
		return
	}
	maxs, err := ev.ReadVec3("maxs")
	if err != nil {
		return
	}
	absmin := [3]float32{
		origin[0] + mins[0],
		origin[1] + mins[1],
		origin[2] + mins[2],
	}
	absmax := [3]float32{
		origin[0] + maxs[0],
		origin[1] + maxs[1],
		origin[2] + maxs[2],
	}
	kind := hostSolidKindFromEntvars(ev)
	h.World.LinkBounds(world.Key(slot), absmin, absmax, kind)
}

// hostSolidKindFromEntvars is the package-local copy of the SOLID_*
// -> SolidKind dispatch the quake-tamago glue uses. Duplicated here
// (one switch, six lines) so the host package doesn't need to import
// quake-tamago (which would introduce a circular dep -- the
// quake-tamago binary imports host, not the other way round).
func hostSolidKindFromEntvars(ev *progs.EntVars) world.SolidKind {
	solid, err := ev.ReadFloat("solid")
	if err != nil {
		return world.SolidKindSkip
	}
	switch server.Solid(int32(solid)) {
	case server.SolidNot:
		return world.SolidKindSkip
	case server.SolidTrigger:
		return world.SolidKindTrigger
	default:
		return world.SolidKindSolid
	}
}

// ResetTouchCounters clears the per-Frame touch counters. Called by
// [Host.Frame] at the top of every tic so the per-60 instrumentation
// in quake-tamago shows per-tic activity rather than cumulative-since-
// boot totals (mirrors the runThink walker's reset contract).
func (h *Host) ResetTouchCounters() {
	h.LastTriggerTouches = 0
	h.LastTouchErrors = 0
	h.LastTouchErrorMsgs = h.LastTouchErrorMsgs[:0]
	_ = touchCounters{}
}
